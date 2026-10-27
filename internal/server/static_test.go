package server

import (
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

func TestStaticFilesAreGzippedAndCached(t *testing.T) {
	script := strings.Repeat("console.log('iglo');\n", 200)
	handler := spa(fstest.MapFS{
		"index.html":       {Data: []byte("<h1>app</h1>")},
		"assets/app.js":    {Data: []byte(script)},
		"assets/font.woff": {Data: []byte(strings.Repeat("x", 4096))},
	})
	get := func(path, encoding, etag string) *httptest.ResponseRecorder {
		request := httptest.NewRequest(http.MethodGet, path, nil)
		if encoding != "" {
			request.Header.Set("Accept-Encoding", encoding)
		}
		if etag != "" {
			request.Header.Set("If-None-Match", etag)
		}
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		return response
	}

	gzipped := get("/assets/app.js", "gzip, deflate, br", "")
	if gzipped.Header().Get("Content-Encoding") != "gzip" || gzipped.Body.Len() >= len(script)/2 {
		t.Fatalf("headers = %v, size = %d", gzipped.Header(), gzipped.Body.Len())
	}
	reader, err := gzip.NewReader(gzipped.Body)
	if err != nil {
		t.Fatal(err)
	}
	if data, _ := io.ReadAll(reader); string(data) != script {
		t.Fatal("the gzipped file differs")
	}
	if !strings.Contains(gzipped.Header().Get("Content-Type"), "javascript") || gzipped.Header().Get("Cache-Control") == "" {
		t.Fatalf("headers = %v", gzipped.Header())
	}

	plain := get("/assets/app.js", "", "")
	if plain.Header().Get("Content-Encoding") != "" || plain.Body.String() != script || plain.Header().Get("Vary") != "Accept-Encoding" {
		t.Fatalf("headers = %v", plain.Header())
	}
	if refused := get("/assets/app.js", "gzip;q=0", ""); refused.Header().Get("Content-Encoding") != "" {
		t.Fatal("gzipped for a browser that refuses it")
	}

	// A browser that has the file gets 304, for each encoding.
	if again := get("/assets/app.js", "gzip", gzipped.Header().Get("ETag")); again.Code != http.StatusNotModified {
		t.Fatalf("code = %d", again.Code)
	}
	if again := get("/assets/app.js", "", plain.Header().Get("ETag")); again.Code != http.StatusNotModified {
		t.Fatalf("code = %d", again.Code)
	}

	// Fonts stay as they are, and unknown pages get the app.
	if font := get("/assets/font.woff", "gzip", ""); font.Header().Get("Content-Encoding") != "" {
		t.Fatal("gzipped a font")
	}
	if page := get("/some/page", "gzip", ""); page.Body.String() != "<h1>app</h1>" || page.Header().Get("Cache-Control") != "no-cache" {
		t.Fatalf("page = %q, %v", page.Body.String(), page.Header())
	}
}
