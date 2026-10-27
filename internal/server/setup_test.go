package server

import (
	"context"
	"fmt"
	"net/http"
	"testing"

	"iglo/internal/setup"
)

type fakeSetup struct {
	started []setup.Plan
	actions []string
	err     error
}

func (f *fakeSetup) View(context.Context) setup.View { return setup.View{State: setup.StateIdle} }
func (f *fakeSetup) Start(_ context.Context, plan setup.Plan) error {
	f.started = append(f.started, plan)
	return f.err
}
func (f *fakeSetup) Retry() error { f.actions = append(f.actions, "retry"); return nil }
func (f *fakeSetup) Skip() error  { f.actions = append(f.actions, "skip"); return nil }
func (f *fakeSetup) Cancel()      { f.actions = append(f.actions, "cancel") }

func TestSetupRoutes(t *testing.T) {
	fake := &fakeSetup{}
	server, _ := newTestServerWithOptions(t, func(options *Options) { options.Setup = fake })
	cookie := login(t, server)

	if response := request(t, http.MethodGet, server.URL+"/api/setup", "", cookie); response.StatusCode != http.StatusOK {
		t.Fatalf("view: %d", response.StatusCode)
	}
	if response := request(t, http.MethodPost, server.URL+"/api/setup", `{"apps":["jellyfin"],"admin":{"username":"nick"}}`, cookie); response.StatusCode != http.StatusAccepted || fake.started[0].Admin.Username != "nick" {
		t.Fatalf("start: %d, %+v", response.StatusCode, fake.started)
	}
	fake.err = fmt.Errorf("%w: choose at least one app", setup.ErrInvalid)
	if response := request(t, http.MethodPost, server.URL+"/api/setup", `{}`, cookie); response.StatusCode != http.StatusBadRequest {
		t.Fatalf("invalid: %d", response.StatusCode)
	}
	request(t, http.MethodPost, server.URL+"/api/setup/retry", "", cookie)
	request(t, http.MethodPost, server.URL+"/api/setup/skip", "", cookie)
	request(t, http.MethodDelete, server.URL+"/api/setup", "", cookie)
	if response := request(t, http.MethodPost, server.URL+"/api/setup/nope", "", cookie); response.StatusCode != http.StatusNotFound {
		t.Fatalf("unknown action: %d", response.StatusCode)
	}
	if fmt.Sprint(fake.actions) != "[retry skip cancel]" {
		t.Fatalf("actions = %v", fake.actions)
	}
	if response := request(t, http.MethodGet, server.URL+"/api/setup", "", nil); response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("without a session: %d", response.StatusCode)
	}
}
