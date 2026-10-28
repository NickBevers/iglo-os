package server

import (
	"compress/gzip"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCompressJSON(t *testing.T) {
	large := map[string]string{"log": strings.Repeat("==> Creating container 130\n", 400)}
	handler := compressJSON(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/small":
			writeJSON(w, http.StatusOK, map[string]string{"ok": "yes"})
		case "/api/events":
			if _, wrapped := w.(gzipJSON); wrapped {
				t.Error("the event stream was wrapped")
			}
		default:
			writeJSON(w, http.StatusCreated, large)
			// Flushing still reaches the real writer.
			if err := http.NewResponseController(w).Flush(); err != nil {
				t.Errorf("flush: %v", err)
			}
		}
	}))
	get := func(path, encoding string) *httptest.ResponseRecorder {
		request := httptest.NewRequest(http.MethodGet, path, nil)
		if encoding != "" {
			request.Header.Set("Accept-Encoding", encoding)
		}
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)
		return recorder
	}

	zipped := get("/api/apps", "gzip, br")
	if zipped.Code != http.StatusCreated || zipped.Header().Get("Content-Encoding") != "gzip" || zipped.Header().Get("Vary") != "Accept-Encoding" {
		t.Fatalf("code = %d, headers = %v", zipped.Code, zipped.Header())
	}
	reader, err := gzip.NewReader(zipped.Body)
	if err != nil {
		t.Fatal(err)
	}
	data, _ := io.ReadAll(reader)
	var got map[string]string
	if json.Unmarshal(data, &got) != nil || got["log"] != large["log"] {
		t.Fatal("the gzipped answer differs")
	}

	if plain := get("/api/apps", ""); plain.Header().Get("Content-Encoding") != "" || !strings.Contains(plain.Body.String(), "Creating container") {
		t.Fatalf("plain headers = %v", plain.Header())
	}
	if refused := get("/api/apps", "gzip;q=0"); refused.Header().Get("Content-Encoding") != "" {
		t.Fatal("gzipped for a client that refuses it")
	}
	if small := get("/api/small", "gzip"); small.Header().Get("Content-Encoding") != "" || small.Body.String() != "{\"ok\":\"yes\"}\n" {
		t.Fatalf("small = %v %q", small.Header(), small.Body.String())
	}
	get("/api/events", "gzip")
}
