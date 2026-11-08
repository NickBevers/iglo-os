package server

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	"iglo/internal/apps"
	"iglo/internal/auth"
	"iglo/internal/setup"
	"iglo/internal/shrink"
	"iglo/internal/stacks/arr"
)

type shrinkRouteFake struct {
	*fakeApps
	calls int
	err   error
	input shrink.Input
}

func (f *shrinkRouteFake) Library(context.Context) (shrink.Library, error) {
	f.calls++
	return shrink.Library{Kinds: []shrink.Folder{}}, f.err
}
func (f *shrinkRouteFake) ShrinkJobs(context.Context) (shrink.View, error) {
	f.calls++
	return shrink.View{Jobs: []shrink.Job{}}, f.err
}
func (f *shrinkRouteFake) SubmitShrink(_ context.Context, in shrink.Input) (apps.ShrinkSubmission, error) {
	f.calls++
	f.input = in
	return apps.ShrinkSubmission{Jobs: []shrink.Job{{Path: "anime/Safe"}}, Refused: []arr.ShrinkCheckResult{{Path: "movies/Unsafe", Title: "Unsafe", State: "refused", Reason: "size format"}}}, f.err
}
func (f *shrinkRouteFake) ShrinkCommand(_ context.Context, command string, _ shrink.Settings) error {
	f.calls++
	if command == "cancel/bad" {
		return shrink.ErrInvalid
	}
	return f.err
}
func shrinkHandler(t *testing.T) (http.Handler, *http.Cookie, *shrinkRouteFake) {
	t.Helper()
	a := auth.NewService(auth.Admin{Username: "admin", PasswordHash: authHash(t), TOTPSecret: testSecret})
	token, err := a.Login("admin", "secret", currentCode(t), "127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	f := &shrinkRouteFake{fakeApps: &fakeApps{}}
	return New(Options{Auth: a, Apps: f, Logger: slog.New(slog.DiscardHandler), Web: fstest.MapFS{"index.html": {Data: []byte("app")}}}), &http.Cookie{Name: sessionCookie, Value: token}, f
}
func authHash(t *testing.T) string {
	t.Helper()
	h, err := auth.HashPassword("secret")
	if err != nil {
		t.Fatal(err)
	}
	return h
}
func TestShrinkRoutesAuthAndValidation(t *testing.T) {
	handler, cookie, f := shrinkHandler(t)
	valid := `{"paths":["anime/Safe"],"profile":"standard","confirm":"replace-originals"}`
	for _, route := range []struct {
		method, path, body string
		code               int
	}{{"GET", "/api/inbox/library", "", 200}, {"POST", "/api/inbox/library/scan", "", 202}, {"GET", "/api/inbox/shrink", "", 200}, {"POST", "/api/inbox/shrink", valid, 202}, {"PUT", "/api/inbox/shrink/settings", `{"paused":true}`, 204}, {"POST", "/api/inbox/shrink/cancel/" + strings.Repeat("a", 32), "", 204}} {
		w := mediaSettingsRequest(handler, route.method, route.path, route.body, nil, true)
		if w.Code != 401 {
			t.Fatal(route, w.Code)
		}
		if route.method != "GET" {
			w = mediaSettingsRequest(handler, route.method, route.path, route.body, cookie, false)
			if w.Code != 403 {
				t.Fatal(route, w.Code)
			}
		}
		if f.calls != 0 {
			t.Fatal("unauthorized call reached service")
		}
	}
	for _, body := range []string{"", `{}`, `null`, `{"paths":["../escape"],"profile":"standard","confirm":"replace-originals"}`, `{"paths":["anime/Safe"],"profile":"standard"}`, strings.Replace(valid, "standard", "unknown", 1), valid + "{}", valid + strings.Repeat(" ", 512<<10)} {
		w := mediaSettingsRequest(handler, "POST", "/api/inbox/shrink", body, cookie, true)
		if w.Code != 400 || f.calls != 0 {
			t.Fatal(w.Code, w.Body, f.calls)
		}
	}
	for _, body := range []string{`{}`, `null`, `{"paused":null}`, `{"paused":true}{}`, `{"paused":true,"extra":1}`} {
		w := mediaSettingsRequest(handler, "PUT", "/api/inbox/shrink/settings", body, cookie, true)
		if w.Code != 400 || f.calls != 0 {
			t.Fatal(w.Code, w.Body)
		}
	}
	for _, route := range []struct {
		method, path, body string
		code               int
	}{{"GET", "/api/inbox/library", "", 200}, {"POST", "/api/inbox/library/scan", "", 202}, {"GET", "/api/inbox/shrink", "", 200}, {"POST", "/api/inbox/shrink", valid, 202}, {"PUT", "/api/inbox/shrink/settings", `{"paused":true}`, 204}, {"POST", "/api/inbox/shrink/cancel/" + strings.Repeat("a", 32), "", 204}} {
		w := mediaSettingsRequest(handler, route.method, route.path, route.body, cookie, true)
		if w.Code != route.code {
			t.Fatal(route, w.Code, w.Body)
		}
		if route.method == "POST" && route.path == "/api/inbox/shrink" {
			var out apps.ShrinkSubmission
			if json.Unmarshal(w.Body.Bytes(), &out) != nil || len(out.Refused) != 1 || out.Refused[0].Reason != "size format" || len(out.Jobs) != 1 {
				t.Fatal(w.Body)
			}
		}
	}
}
func TestShrinkRouteErrors(t *testing.T) {
	handler, cookie, f := shrinkHandler(t)
	for _, test := range []struct {
		err  error
		code int
	}{{shrink.ErrInvalid, 400}, {apps.ErrNotInstalled, 409}, {apps.ErrNotRunning, 409}, {errors.New("shrink check failed"), 502}} {
		f.err = test.err
		w := mediaSettingsRequest(handler, "POST", "/api/inbox/shrink", `{"paths":["anime/Safe"],"profile":"high","confirm":"replace-originals"}`, cookie, true)
		if w.Code != test.code {
			t.Fatal(w.Code, w.Body)
		}
	}
	f.err = nil
	w := mediaSettingsRequest(handler, "POST", "/api/inbox/shrink/cancel/bad", "", cookie, true)
	if w.Code != 400 {
		t.Fatal(w.Code)
	}
}

type shrinkSetupFake struct{ *fakeSetup }

func (*shrinkSetupFake) View(context.Context) setup.View {
	return setup.View{Steps: []setup.Step{{ID: "app:inbox", App: "inbox", Title: "Install Inbox"}, {ID: "share:inbox", Title: "Connect the Inbox share"}}}
}
func TestShrinkRenameKeepsInboxShare(t *testing.T) {
	s := &server{Options: Options{Setup: &shrinkSetupFake{}}}
	w := httptest.NewRecorder()
	s.setupRoute(w, httptest.NewRequest("GET", "/api/setup", nil))
	var view setup.View
	if json.Unmarshal(w.Body.Bytes(), &view) != nil || view.Steps[0].Title != "Install Video tools" || view.Steps[1].Title != "Connect the Inbox share" {
		t.Fatal(w.Body)
	}
}
