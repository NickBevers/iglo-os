package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"testing/fstest"

	"iglo/internal/agent"
	"iglo/internal/apps"
	"iglo/internal/auth"
	"iglo/internal/stacks/arr"
)

func (f *fakeApps) MediaSettings(_ context.Context, id string) (arr.MediaSettings, error) {
	f.app = id
	f.mediaSettingsReads++
	return f.mediaSettings, f.mediaSettingsErr
}

func (f *fakeApps) ChangeMediaSettings(ctx context.Context, background context.Context, id string, settings arr.MediaSettings) error {
	f.app, f.mediaSettings = id, settings
	f.mediaSettingsChanges++
	f.mediaSettingsContext, f.mediaSettingsBackground = ctx, background
	return f.mediaSettingsErr
}

// mediaSettingsHandler uses the real middleware and routes without a socket.
func mediaSettingsHandler(t *testing.T, fake *fakeApps) (http.Handler, *http.Cookie) {
	t.Helper()
	hash, err := auth.HashPassword("secret")
	if err != nil {
		t.Fatal(err)
	}
	a := auth.NewService(auth.Admin{Username: "admin", PasswordHash: hash, TOTPSecret: testSecret})
	token, err := a.Login("admin", "secret", currentCode(t), "127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	handler := New(Options{
		Auth: a, Apps: fake, Background: t.Context(), Logger: slog.New(slog.DiscardHandler),
		Web: fstest.MapFS{"index.html": {Data: []byte("app")}},
	})
	return handler, &http.Cookie{Name: sessionCookie, Value: token}
}

func mediaSettingsRequest(handler http.Handler, method, path, body string, cookie *http.Cookie, header bool) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	if cookie != nil {
		r.AddCookie(cookie)
	}
	if header {
		r.Header.Set(csrfHeader, "1")
	}
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	return w
}

func TestMediaSettingsRoutes(t *testing.T) {
	settings := arr.DefaultMediaSettings()
	settings.Series.Subtitles = []string{"nl", "en"}
	fake := &fakeApps{mediaSettings: settings}
	handler, cookie := mediaSettingsHandler(t, fake)
	data, err := json.Marshal(struct {
		Settings arr.MediaSettings `json:"settings"`
	}{settings})
	if err != nil {
		t.Fatal(err)
	}
	body := string(data)
	w := mediaSettingsRequest(handler, http.MethodGet, "/api/apps/media/media-settings", "", cookie, false)
	if w.Code != http.StatusOK || strings.TrimSpace(w.Body.String()) != body || fake.app != "media" || fake.mediaSettingsReads != 1 {
		t.Fatalf("GET: %d %s, app = %q, reads = %d", w.Code, w.Body, fake.app, fake.mediaSettingsReads)
	}
	w = mediaSettingsRequest(handler, http.MethodPut, "/api/apps/media/media-settings", body, cookie, true)
	if w.Code != http.StatusAccepted || w.Body.Len() != 0 || fake.app != "media" || !reflect.DeepEqual(fake.mediaSettings, settings) || fake.mediaSettingsChanges != 1 {
		t.Fatalf("PUT: %d %s, settings = %+v", w.Code, w.Body, fake.mediaSettings)
	}
	if fake.mediaSettingsContext == nil || fake.mediaSettingsBackground != t.Context() {
		t.Fatal("change did not receive request and background contexts")
	}
}

func TestMediaSettingsRouteErrors(t *testing.T) {
	fake := &fakeApps{}
	handler, cookie := mediaSettingsHandler(t, fake)
	data, _ := json.Marshal(map[string]any{"settings": arr.DefaultMediaSettings()})
	for _, test := range []struct {
		name   string
		err    error
		status int
	}{
		{"unknown app", apps.ErrUnknownApp, http.StatusNotFound},
		{"busy", apps.ErrBusy, http.StatusConflict},
		{"unreadable config", fmt.Errorf("%w: its settings file is missing or can't be opened. Update the media stack on the Apps page.", apps.ErrMediaSettingsUnreadable), http.StatusConflict},
		{"unreadable profiles", fmt.Errorf("%w: Bazarr profile has invalid items. Fix it in Bazarr under Settings, Languages.", apps.ErrMediaSettingsUnreadable), http.StatusConflict},
		{"refused API key", fmt.Errorf("%w: Bazarr refused the API key. Update the media stack on the Apps page.", apps.ErrMediaSettingsUnreadable), http.StatusConflict},
		{"Bazarr unavailable", apps.ErrMediaSettingsUnavailable, http.StatusServiceUnavailable},
		{"needs update", apps.ErrMediaSettingsNeedsUpdate, http.StatusConflict},
		{"needs update with agent text", fmt.Errorf("host agent: %w", apps.ErrMediaSettingsNeedsUpdate), http.StatusConflict},
		{"not installed", apps.ErrNotInstalled, http.StatusConflict},
		{"not running", apps.ErrNotRunning, http.StatusConflict},
		{"invalid answers", fmt.Errorf("%w: invalid language", agent.ErrInvalidAnswers), http.StatusBadRequest},
		{"offline", errors.New("host agent is offline"), http.StatusBadGateway},
	} {
		for _, method := range []string{http.MethodGet, http.MethodPut} {
			t.Run(test.name+"/"+method, func(t *testing.T) {
				fake.mediaSettingsErr = test.err
				w := mediaSettingsRequest(handler, method, "/api/apps/media/media-settings", string(data), cookie, true)
				want, _ := json.Marshal(map[string]string{"error": test.err.Error()})
				if w.Code != test.status || strings.TrimSpace(w.Body.String()) != string(want) {
					t.Fatalf("got %d %s, want %d %s", w.Code, w.Body, test.status, want)
				}
			})
		}
	}
}

func TestMediaSettingsStrictBody(t *testing.T) {
	fake := &fakeApps{}
	handler, cookie := mediaSettingsHandler(t, fake)
	data, _ := json.Marshal(map[string]any{"settings": arr.DefaultMediaSettings()})
	valid := string(data)
	invalidBody := `{"error":"invalid request body"}`
	for _, test := range []struct {
		name string
		body string
		want string
	}{
		{"empty", "", invalidBody},
		{"malformed", "{", invalidBody},
		{"missing settings", "{}", invalidBody},
		{"null body", "null", invalidBody},
		{"null settings", `{"settings":null}`, invalidBody},
		{"array", "[]", invalidBody},
		{"unknown envelope field", strings.TrimSuffix(valid, "}") + `,"extra":true}`, invalidBody},
		{"unknown settings field", strings.Replace(valid, `"settings":{`, `"settings":{"extra":true,`, 1), invalidBody},
		{"unknown kind field", strings.Replace(valid, `"anime":{`, `"anime":{"extra":true,`, 1), invalidBody},
		{"wrong field type", strings.Replace(valid, `"audio":"original"`, `"audio":true`, 1), invalidBody},
		{"trailing object", valid + ` {}`, invalidBody},
		{"trailing null", valid + ` null`, invalidBody},
		{"trailing junk", valid + ` garbage`, invalidBody},
		{"oversized", valid + strings.Repeat(" ", 64*1024), invalidBody},
		{"over limit by one", valid + strings.Repeat(" ", 64*1024-len(valid)+1), invalidBody},
		{"incomplete settings", `{"settings":{}}`, `{"error":"Anime: audio must be \"original\""}`},
		{"invalid audio", strings.Replace(valid, `"audio":"dutch"`, `"audio":"original"`, 1), `{"error":"Dutch: audio must be \"dutch\""}`},
		{"invalid subtitles", strings.Replace(valid, `"subtitles":["en"]`, `"subtitles":["invalid"]`, 1), `{"error":"Series: unsupported subtitle language \"invalid\""}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			w := mediaSettingsRequest(handler, http.MethodPut, "/api/apps/media/media-settings", test.body, cookie, true)
			if w.Code != http.StatusBadRequest || strings.TrimSpace(w.Body.String()) != test.want {
				t.Fatalf("got %d %s, want 400 %s", w.Code, w.Body, test.want)
			}
			if fake.mediaSettingsChanges != 0 {
				t.Fatal("invalid settings reached the service")
			}
		})
	}
	for _, body := range []string{valid + " \n\t", valid + strings.Repeat(" ", 64*1024-len(valid))} {
		w := mediaSettingsRequest(handler, http.MethodPut, "/api/apps/media/media-settings", body, cookie, true)
		if w.Code != http.StatusAccepted {
			t.Fatalf("valid whitespace or 64 KiB body: %d %s", w.Code, w.Body)
		}
	}
}

func TestMediaSettingsRouteAuth(t *testing.T) {
	fake := &fakeApps{}
	handler, cookie := mediaSettingsHandler(t, fake)
	data, _ := json.Marshal(map[string]any{"settings": arr.DefaultMediaSettings()})
	for _, method := range []string{http.MethodGet, http.MethodPut} {
		w := mediaSettingsRequest(handler, method, "/api/apps/media/media-settings", string(data), nil, true)
		if w.Code != http.StatusUnauthorized || strings.TrimSpace(w.Body.String()) != `{"error":"not logged in"}` {
			t.Fatalf("unauthenticated %s: %d %s", method, w.Code, w.Body)
		}
	}
	w := mediaSettingsRequest(handler, http.MethodPut, "/api/apps/media/media-settings", string(data), cookie, false)
	if w.Code != http.StatusForbidden || strings.TrimSpace(w.Body.String()) != `{"error":"missing X-Iglo-Request header"}` {
		t.Fatalf("missing header: %d %s", w.Code, w.Body)
	}
	if fake.mediaSettingsReads != 0 || fake.mediaSettingsChanges != 0 {
		t.Fatal("unauthorized request reached the service")
	}
}
