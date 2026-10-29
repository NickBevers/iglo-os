package workloads

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"iglo/internal/immich"
)

// newImmich is a new Immich without an admin. It counts the keys it makes.
type newImmich struct {
	mu          sync.Mutex
	initialized bool
	keys        int
}

func (n *newImmich) server(t *testing.T) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n.mu.Lock()
		defer n.mu.Unlock()
		switch r.Method + " " + r.URL.Path {
		case "GET /api/server/config":
			json.NewEncoder(w).Encode(map[string]bool{"isInitialized": n.initialized})
		case "POST /api/auth/admin-sign-up":
			n.initialized = true
			w.WriteHeader(http.StatusCreated)
		case "POST /api/auth/login":
			w.Write([]byte(`{"accessToken":"session"}`))
		case "GET /api/admin/config", "PUT /api/admin/config":
			w.Write([]byte(`{"job":{"videoConversion":{},"faceDetection":{},"smartSearch":{},"ocr":{}},"ffmpeg":{},"machineLearning":{"facialRecognition":{}}}`))
		case "POST /api/api-keys":
			n.keys++
			w.Write([]byte(`{"secret":"queue-secret"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	return server
}

var admin = adminFor("nick@example.org")

func adminFor(email string) immich.Admin {
	return immich.Admin{Email: email, Name: "Nick", Password: "a long password"}
}

func TestPhotosInstallConnectsItself(t *testing.T) {
	fake := &newImmich{}
	server := fake.server(t)
	host := &fakeHost{free: 8 << 30}
	s, err := New(Options{DataDir: t.TempDir(), HostMemory: host.HostMemory})
	if err != nil {
		t.Fatal(err)
	}
	// The install fills the URL first, then makes the admin and the key.
	if err := s.Discover(server.URL); err != nil {
		t.Fatal(err)
	}
	if err := s.ConnectInstalled(context.Background(), server.URL, admin); err != nil {
		t.Fatal(err)
	}
	view := s.Status()
	if !view.Configured || !view.Settings.Enabled || view.Settings.Node != "pve" || view.Settings.URL != server.URL || fake.keys != 1 {
		t.Fatalf("view = %+v, keys = %d", view, fake.keys)
	}
	// The key never reaches the page.
	if view.Settings.APIKey != "" {
		t.Fatal("the status has the key")
	}
	// The key is in the 0600 state file, and nowhere in the settings that pages read.
	data, err := os.ReadFile(s.path)
	if err != nil || !strings.Contains(string(data), "queue-secret") {
		t.Fatal("key not saved", err)
	}
	if info, _ := os.Stat(s.path); info.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %v", info.Mode())
	}
}

func TestPhotosInstallNeverReplacesAConnection(t *testing.T) {
	fake := &newImmich{}
	server := fake.server(t)
	host := &fakeHost{free: 8 << 30}
	s, err := New(Options{DataDir: t.TempDir(), HostMemory: host.HostMemory})
	if err != nil {
		t.Fatal(err)
	}
	settings := DefaultSettings()
	settings.URL, settings.APIKey, settings.Node = "http://nas:2283", "nicks-key", "pve"
	if err := s.SaveSettings(settings); err != nil {
		t.Fatal(err)
	}
	if err := s.ConnectInstalled(context.Background(), server.URL, admin); err != nil {
		t.Fatal(err)
	}
	// The admin is made, as asked, but no key, and the connection stays.
	if !fake.initialized || fake.keys != 0 {
		t.Fatalf("initialized = %v, keys = %d", fake.initialized, fake.keys)
	}
	data, _ := os.ReadFile(filepath.Join(filepath.Dir(s.path), "workloads.json"))
	if got := s.Status().Settings; got.URL != "http://nas:2283" || got.Enabled || !strings.Contains(string(data), "nicks-key") {
		t.Fatalf("settings = %+v", got)
	}
}

func TestPhotosInstallLeavesAnExistingAdmin(t *testing.T) {
	fake := &newImmich{initialized: true}
	server := fake.server(t)
	s, err := New(Options{DataDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.ConnectInstalled(context.Background(), server.URL, admin); !errors.Is(err, immich.ErrHasAdmin) || fake.keys != 0 || s.Status().Configured {
		t.Fatal(err)
	}
}

func TestPhotosInstallWithoutAgentKeepsControlOff(t *testing.T) {
	fake := &newImmich{}
	server := fake.server(t)
	host := &fakeHost{err: errors.New("no agent")}
	s, err := New(Options{DataDir: t.TempDir(), HostMemory: host.HostMemory})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.ConnectInstalled(context.Background(), server.URL, admin); err == nil {
		t.Fatal("said nothing about the node")
	}
	// The key is saved, so Nick only chooses the node and turns control on.
	if view := s.Status(); !view.Configured || view.Settings.Enabled || view.Settings.Node != "" {
		t.Fatalf("view = %+v", view)
	}
}

func TestPhotosInstallChecksTheAdmin(t *testing.T) {
	s, err := New(Options{DataDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	// Nothing is sent for a bad admin.
	if err := s.ConnectInstalled(context.Background(), "http://127.0.0.1:1", adminFor("nick")); err == nil {
		t.Fatal("took a bad email")
	}
}
