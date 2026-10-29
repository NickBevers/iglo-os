package server

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"iglo/internal/agent"
	"iglo/internal/apps"
	"iglo/internal/immich"
)

// fakeApps records the installs that the Apps page starts.
type fakeApps struct {
	Apps
	app     string
	request agent.InstallRequest
	then    apps.AfterInstall
}

func (f *fakeApps) Install(_ context.Context, _ context.Context, id string, request agent.InstallRequest) error {
	f.app, f.request = id, request
	return nil
}

func (f *fakeApps) InstallThen(_ context.Context, _ context.Context, id string, request agent.InstallRequest, then apps.AfterInstall) error {
	f.app, f.request, f.then = id, request, then
	return nil
}

func TestPhotosInstallKeepsTheImmichAdminInTheManager(t *testing.T) {
	fake := &fakeApps{}
	var connected immich.Admin
	var address string
	server, _ := newTestServerWithOptions(t, func(options *Options) {
		options.Apps = fake
		options.ConnectPhotos = func(_ context.Context, status agent.AppInstallStatus, admin immich.Admin) error {
			connected, address = admin, status.IP
			return nil
		}
	})
	cookie := login(t, server)
	scoped := `"scoped":{"storage":"local-lvm","libraryBinding":"photos-1","backupBinding":"backups-1","cores":4,"memoryMiB":8192,"diskGB":64}`

	// A bad admin starts nothing.
	response := request(t, http.MethodPost, server.URL+"/api/apps/photos/install", `{`+scoped+`,"immich":{"email":"nick","name":"Nick","password":"a long password"}}`, cookie)
	if response.StatusCode != http.StatusBadRequest || fake.app != "" {
		t.Fatalf("bad admin: %s, started %q", response.Status, fake.app)
	}
	// Only Photos has an Immich.
	response = request(t, http.MethodPost, server.URL+"/api/apps/music/install", `{`+scoped+`,"immich":{"email":"nick@example.org","name":"Nick","password":"a long password"}}`, cookie)
	if response.StatusCode != http.StatusBadRequest || fake.app != "" {
		t.Fatalf("music: %s, started %q", response.Status, fake.app)
	}

	response = request(t, http.MethodPost, server.URL+"/api/apps/photos/install", `{`+scoped+`,"immich":{"email":"nick@example.org","name":"Nick","password":"a long password","faceModels":true}}`, cookie)
	if response.StatusCode != http.StatusAccepted || fake.app != agent.PhotosApp || fake.then == nil || fake.request.Scoped.LibraryBinding != "photos-1" {
		t.Fatalf("photos: %s, %+v", response.Status, fake)
	}
	// What goes to the host agent has no admin.
	sent, _ := json.Marshal(fake.request)
	if strings.Contains(string(sent), "a long password") || strings.Contains(string(sent), "nick@example.org") {
		t.Fatalf("the agent gets the admin: %s", sent)
	}
	// After the install, the admin goes to Background work with the new address.
	if err := fake.then(context.Background(), agent.AppInstallStatus{IP: "192.168.1.170"}); err != nil {
		t.Fatal(err)
	}
	if connected.Email != "nick@example.org" || connected.Password != "a long password" || !connected.FaceModels || address != "192.168.1.170" {
		t.Fatalf("connected %+v at %s", connected, address)
	}

	// Without an admin, the install is as before.
	fake.then = nil
	response = request(t, http.MethodPost, server.URL+"/api/apps/photos/install", `{`+scoped+`}`, cookie)
	if response.StatusCode != http.StatusAccepted || fake.then != nil {
		t.Fatalf("without admin: %s", response.Status)
	}
}
