package jellyfin

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

type refreshTransport func(*http.Request) (*http.Response, error)

func (f refreshTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestRefreshLibraryUsesSavedClient(t *testing.T) {
	original := http.DefaultTransport
	defer func() { http.DefaultTransport = original }()
	calls := 0
	code := 204
	http.DefaultTransport = refreshTransport(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.Method != "POST" || r.URL.String() != "http://jellyfin/Library/Refresh" || r.Header.Get("Authorization") != `MediaBrowser Token="key"` {
			t.Fatal(r.Method, r.URL, r.Header)
		}
		w := httptest.NewRecorder()
		w.WriteHeader(code)
		return w.Result(), nil
	})
	s, err := NewService(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err = s.RefreshLibrary(t.Context()); err != nil || calls != 0 {
		t.Fatal(err, calls)
	}
	s.settings = Settings{URL: "http://jellyfin", APIKey: "key"}
	s.libraryAt = time.Now()
	if err = s.RefreshLibrary(t.Context()); err != nil || calls != 1 || !s.libraryAt.IsZero() {
		t.Fatal(err, calls, s.libraryAt)
	}
	s.libraryAt = time.Now()
	code = 503
	if err = s.RefreshLibrary(t.Context()); err == nil || s.libraryAt.IsZero() {
		t.Fatal("failed refresh cleared cache", err)
	}
}
