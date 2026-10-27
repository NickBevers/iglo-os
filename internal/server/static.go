package server

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"net/http"
	"path"
	"strings"
	"sync"
	"time"
)

// compressible are the files of the web app that shrink with gzip. Fonts and
// images are compressed already.
var compressible = map[string]bool{".js": true, ".css": true, ".html": true, ".svg": true, ".json": true, ".txt": true}

// staticFile is one file of the web app, read and compressed once, on its
// first request. The manager has one core, so it never compresses twice.
type staticFile struct {
	once    sync.Once
	data    []byte
	gzipped []byte
	etag    string
	err     error
}

// staticFiles serves the files of the web app with an ETag, and gzipped to a
// browser that accepts it.
type staticFiles struct {
	files fs.FS
	cache sync.Map // path -> *staticFile
}

func (s *staticFiles) load(name string) (*staticFile, error) {
	value, _ := s.cache.LoadOrStore(name, &staticFile{})
	file := value.(*staticFile)
	file.once.Do(func() {
		file.data, file.err = fs.ReadFile(s.files, name)
		if file.err != nil {
			return
		}
		sum := sha256.Sum256(file.data)
		file.etag = hex.EncodeToString(sum[:8])
		if !compressible[path.Ext(name)] || len(file.data) < 1024 {
			return
		}
		var buffer bytes.Buffer
		writer, _ := gzip.NewWriterLevel(&buffer, gzip.BestCompression)
		writer.Write(file.data)
		writer.Close()
		// Keep the copy only when it saves something worth the header.
		if buffer.Len() < len(file.data)*9/10 {
			file.gzipped = buffer.Bytes()
		}
	})
	return file, file.err
}

func (s *staticFiles) serve(w http.ResponseWriter, r *http.Request, name string) {
	file, err := s.load(name)
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	content, etag := file.data, file.etag
	if file.gzipped != nil {
		w.Header().Add("Vary", "Accept-Encoding")
		if acceptsGzip(r) {
			w.Header().Set("Content-Encoding", "gzip")
			content, etag = file.gzipped, etag+"-gzip"
		}
	}
	w.Header().Set("ETag", `"`+etag+`"`)
	// ServeContent answers If-None-Match with 304 and sets the type from the name.
	http.ServeContent(w, r, name, time.Time{}, bytes.NewReader(content))
}

// acceptsGzip reports whether the request accepts gzip, and doesn't refuse it with q=0.
func acceptsGzip(r *http.Request) bool {
	for _, part := range strings.Split(r.Header.Get("Accept-Encoding"), ",") {
		name, params, _ := strings.Cut(strings.TrimSpace(part), ";")
		if strings.EqualFold(strings.TrimSpace(name), "gzip") {
			return strings.ReplaceAll(params, " ", "") != "q=0"
		}
	}
	return false
}
