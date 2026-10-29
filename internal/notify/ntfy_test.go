package notify

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestNtfyPublishesToTopic(t *testing.T) {
	var path, body, auth, title string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data, _ := io.ReadAll(r.Body)
		path, body, auth, title = r.URL.Path, string(data), r.Header.Get("Authorization"), r.Header.Get("Title")
		w.Write([]byte(`{"id":"x"}`))
	}))
	defer server.Close()

	ntfy := Ntfy{Server: server.URL, Topic: "iglo-abc", Token: "tk_AgQdq7mVBoFD37zQVN29RhuMzNIz2"}
	if err := ntfy.Send(context.Background(), "✅ jellyfin: updated"); err != nil {
		t.Fatal(err)
	}
	if path != "/iglo-abc" || body != "✅ jellyfin: updated" || auth != "Bearer tk_AgQdq7mVBoFD37zQVN29RhuMzNIz2" || title != "Iglo-OS" {
		t.Fatalf("path %q, body %q, auth %q, title %q", path, body, auth, title)
	}
}

func TestNtfyNeverFollowsCredentialRedirects(t *testing.T) {
	redirected := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/topic" {
			http.Redirect(w, r, "/elsewhere", http.StatusTemporaryRedirect)
			return
		}
		redirected = true
	}))
	defer server.Close()
	err := (Ntfy{Server: server.URL, Topic: "topic", Token: "secret"}).Send(context.Background(), "hello")
	if err == nil || redirected {
		t.Fatalf("redirect followed: %t, %v", redirected, err)
	}
}
