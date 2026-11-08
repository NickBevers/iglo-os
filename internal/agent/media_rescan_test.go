package agent

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

func TestMediaLibraryCommands(t *testing.T) {
	i, _ := newTestInstaller(t)
	var got []string
	i.output = func(_ context.Context, name string, args ...string) ([]byte, error) {
		got = append([]string{name}, args...)
		return []byte(`[]`), nil
	}
	in := MediaLibraryRequest{VMID: 130, Paths: []string{"anime/Show/Season 01/a.mkv", "movies/Film/a.mkv"}}
	for _, command := range []string{"rescan", "shrink-check"} {
		var err error
		if command == "rescan" {
			_, err = i.RescanLibrary(context.Background(), in)
		} else {
			_, err = i.ShrinkCheck(context.Background(), in)
		}
		if err != nil {
			t.Fatal(err)
		}
		want := append([]string{"pct", "exec", "130", "--", "/usr/local/bin/iglo-arr", command}, in.Paths...)
		if !reflect.DeepEqual(got, want) {
			t.Fatal(got)
		}
	}
	got = nil
	for _, p := range []string{"../anime/a.mkv", "home-videos/a.mkv", "anime/../x.mkv"} {
		bad := in
		bad.Paths = []string{p}
		if _, err := i.ShrinkCheck(context.Background(), bad); err == nil {
			t.Fatal("accepted", p)
		}
	}
	if got != nil {
		t.Fatal("invalid paths ran a command")
	}
	i.output = func(context.Context, string, ...string) ([]byte, error) { return nil, errors.New("API key secret") }
	if _, err := i.RescanLibrary(context.Background(), in); err == nil || strings.Contains(err.Error(), "secret") {
		t.Fatal("leaked diagnostic", err)
	}
}
func TestMediaLibraryRoutes(t *testing.T) {
	i, _ := newTestInstaller(t)
	i.output = func(context.Context, string, ...string) ([]byte, error) { return []byte(`[]`), nil }
	mux := http.NewServeMux()
	RegisterMediaLibrary(mux, i)
	for _, command := range []string{"rescan", "shrink-check"} {
		for _, tc := range []struct {
			body string
			code int
		}{{`{"vmid":130,"paths":["anime/Show/a.mkv"]}`, 200}, {`{"vmid":130,"paths":["home-videos/a.mkv"]}`, 400}, {`{"vmid":1,"paths":["movies/A/a.mkv"]}`, 400}} {
			w := httptest.NewRecorder()
			r := httptest.NewRequest("POST", "/v1/apps/media/"+command, strings.NewReader(tc.body))
			mux.ServeHTTP(w, r)
			if w.Code != tc.code {
				t.Fatal(command, w.Code, w.Body.String())
			}
		}
	}
}
func TestShrinkRelayRejectsBeforeTransport(t *testing.T) {
	mux := http.NewServeMux()
	RegisterInbox(mux)
	for _, tc := range []struct{ method, path, body string }{{"POST", "/v1/inbox/104/shrink", `{"paths":["anime/Show/a.mkv"],"profile":"standard"}`}, {"POST", "/v1/inbox/104/shrink", `{"paths":["home-videos/a.mkv"],"profile":"standard","confirm":"replace-originals"}`}, {"PUT", "/v1/inbox/104/shrink/settings", `{}`}, {"POST", "/v1/inbox/104/shrink/cancel/bad", `{}`}} {
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body)))
		if w.Code != 400 {
			t.Fatal(w.Code, w.Body.String())
		}
	}
}
