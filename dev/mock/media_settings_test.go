package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"testing"
	"time"

	"iglo/internal/agent"
	"iglo/internal/stacks/arr"
)

func TestMockMediaSizeSettings(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := os.MkdirAll(statePath(""), 0o700); err != nil {
		t.Fatal(err)
	}
	installer, err := newAppInstaller()
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	addAppRoutes(mux, installer)
	get := func() arr.MediaSettings {
		t.Helper()
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/v1/apps/media/settings?vmid=130", nil))
		if w.Code != http.StatusOK {
			t.Fatalf("get: %d %s", w.Code, w.Body)
		}
		var settings arr.MediaSettings
		if err := json.Unmarshal(w.Body.Bytes(), &settings); err != nil {
			t.Fatal(err)
		}
		return settings
	}
	if settings := get(); settings.Sizes != (arr.SizeLimits{}) {
		t.Fatal("mock defaults have a limit")
	}
	settings := arr.DefaultMediaSettings()
	settings.Sizes = arr.SizeLimits{Series: 41, Films: 66, Films4K: 208}
	data, _ := json.Marshal(agent.MediaSettingsRequest{VMID: 130, Settings: settings})
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest(http.MethodPut, "/v1/apps/media/settings", bytes.NewReader(data)))
	if w.Code != http.StatusAccepted {
		t.Fatalf("put: %d %s", w.Code, w.Body)
	}
	// Drive the mock's real routes and installer without opening a socket.
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	deadline := time.NewTimer(15 * time.Second)
	defer deadline.Stop()
	waiting := true
	for waiting {
		select {
		case <-deadline.C:
			t.Fatal("mock install did not finish")
		case <-ticker.C:
			status := installer.Status()
			if status.State == agent.UpgradeRunning {
				continue
			}
			if status.State != agent.UpgradeSucceeded {
				t.Fatalf("mock install: %+v", status)
			}
			waiting = false
		}
	}
	if got := get(); !reflect.DeepEqual(got, settings) {
		t.Fatalf("mock lost settings: %+v", got)
	}
	for _, sizes := range []arr.SizeLimits{{Series: 9}, {Films: 401}, {Films4K: 9}, {Films4K: 1001}} {
		settings.Sizes = sizes
		data, _ := json.Marshal(map[string]any{"vmid": 130, "settings": settings})
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest(http.MethodPut, "/v1/apps/media/settings", bytes.NewReader(data)))
		if w.Code != http.StatusBadRequest {
			t.Fatalf("invalid limit: %d %s", w.Code, w.Body)
		}
	}
}
