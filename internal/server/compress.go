package server

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"net/http"
	"sync"
)

// minGzipJSON is the smallest JSON answer worth compressing. A smaller one
// gains less than the gzip header and the work on the manager's one core.
const minGzipJSON = 1024

// gzipJSON marks the answer to a client that accepts gzip, so writeJSON can
// compress it. An answer like /api/apps carries up to 64 KiB of install log,
// about once a second during an install. Other writes pass through as they are.
type gzipJSON struct{ http.ResponseWriter }

// Unwrap lets http.ResponseController reach the real writer.
func (w gzipJSON) Unwrap() http.ResponseWriter { return w.ResponseWriter }

// gzipWriters are reused: each gzip.Writer holds hundreds of KiB, and the
// manager runs with a small memory limit.
var gzipWriters = sync.Pool{New: func() any {
	writer, _ := gzip.NewWriterLevel(nil, gzip.BestSpeed)
	return writer
}}

// compressJSON lets writeJSON gzip the answers of the API. The event stream
// and websockets are left alone, because they flush or take the connection.
func compressJSON(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if acceptsGzip(r) && r.Header.Get("Upgrade") == "" && r.URL.Path != "/api/events" {
			w = gzipJSON{w}
		}
		next.ServeHTTP(w, r)
	})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	if _, ok := w.(gzipJSON); !ok {
		w.WriteHeader(status)
		json.NewEncoder(w).Encode(value)
		return
	}

	var buffer bytes.Buffer
	json.NewEncoder(&buffer).Encode(value)
	w.Header().Add("Vary", "Accept-Encoding")
	if buffer.Len() < minGzipJSON {
		w.WriteHeader(status)
		w.Write(buffer.Bytes())
		return
	}

	w.Header().Set("Content-Encoding", "gzip")
	w.WriteHeader(status)
	writer := gzipWriters.Get().(*gzip.Writer)
	writer.Reset(w)
	writer.Write(buffer.Bytes())
	writer.Close()
	gzipWriters.Put(writer)
}
