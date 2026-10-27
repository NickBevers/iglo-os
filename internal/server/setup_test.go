package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
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
func (f *fakeSetup) ExportPlan(context.Context) ([]byte, error) {
	return setup.MarshalFile(setup.Plan{Apps: []string{"jellyfin"}, Admin: setup.Admin{Username: "nick", Password: "secret pass"}})
}

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

func TestSetupPlanFile(t *testing.T) {
	server, _ := newTestServerWithOptions(t, func(options *Options) { options.Setup = &fakeSetup{} })
	cookie := login(t, server)

	response := request(t, http.MethodGet, server.URL+"/api/setup/plan", "", cookie)
	body, _ := io.ReadAll(response.Body)
	if response.StatusCode != http.StatusOK || !strings.Contains(response.Header.Get("Content-Disposition"), "iglo-setup.yaml") ||
		!strings.Contains(string(body), "username: nick") || strings.Contains(string(body), "secret pass") {
		t.Fatalf("export: %d %v\n%s", response.StatusCode, response.Header, body)
	}

	response = request(t, http.MethodPost, server.URL+"/api/setup/plan", string(body), cookie)
	var plan setup.Plan
	if err := json.NewDecoder(response.Body).Decode(&plan); err != nil || response.StatusCode != http.StatusOK || plan.Admin.Username != "nick" {
		t.Fatalf("load: %d %+v %v", response.StatusCode, plan, err)
	}
	if response := request(t, http.MethodPost, server.URL+"/api/setup/plan", "version: 9", cookie); response.StatusCode != http.StatusBadRequest {
		t.Fatalf("bad file: %d", response.StatusCode)
	}
}
