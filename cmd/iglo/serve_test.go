package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRedirectKeepsOnlyAppNames(t *testing.T) {
	isAppHost := func(host string) bool { return host == "seerr.iglo.local" || host == "seerr.iglo.local:80" }
	handler := redirectHandler("iglo.local", isAppHost, []byte("ca"))

	for host, want := range map[string]string{
		"seerr.iglo.local:80": "https://seerr.iglo.local/requests?x=1",
		"iglo.local":          "https://iglo.local/requests?x=1",
		"evil.example.com":    "https://iglo.local/requests?x=1",
	} {
		r := httptest.NewRequest(http.MethodGet, "/requests?x=1", nil)
		r.Host = host
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if got := w.Header().Get("Location"); got != want {
			t.Errorf("%s: Location = %q, want %q", host, got, want)
		}
	}
}
