package server

import (
	"encoding/json"
	"net/http"
	"testing"

	"iglo/internal/workloads"
)

func TestWorkloadEndpointsAuthenticateAndPersist(t *testing.T) {
	service, err := workloads.New(workloads.Options{DataDir: t.TempDir(), Proxmox: &fakeProxmox{}})
	if err != nil {
		t.Fatal(err)
	}
	server, _ := newTestServerWithOptions(t, func(options *Options) { options.Workloads = service })
	for _, path := range []string{"/api/workloads", "/api/workloads/settings", "/api/workloads/protection"} {
		method := http.MethodPut
		if path == "/api/workloads" {
			method = http.MethodGet
		}
		response := request(t, method, server.URL+path, `{}`, nil)
		if response.StatusCode != http.StatusUnauthorized {
			t.Fatalf("%s: %s", path, response.Status)
		}
	}
	cookie := login(t, server)
	settings := workloads.DefaultSettings()
	settings.URL = "http://photos:2283"
	settings.APIKey = "private-key"
	body, _ := json.Marshal(settings)
	response := request(t, http.MethodPut, server.URL+"/api/workloads/settings", string(body), cookie)
	if response.StatusCode != http.StatusNoContent {
		t.Fatal(response.Status)
	}
	response = request(t, http.MethodGet, server.URL+"/api/workloads", "", cookie)
	var view workloads.View
	if err := json.NewDecoder(response.Body).Decode(&view); err != nil {
		t.Fatal(err)
	}
	if !view.Configured || view.Settings.APIKey != "" {
		t.Fatal(view)
	}
	response = request(t, http.MethodPut, server.URL+"/api/workloads/protection", `{"minutes":120}`, cookie)
	if response.StatusCode != http.StatusNoContent || service.Status().GamingUntil.IsZero() {
		t.Fatal(response.Status)
	}
	response = request(t, http.MethodPut, server.URL+"/api/workloads/protection", `{"minutes":-1}`, cookie)
	if response.StatusCode != http.StatusBadRequest {
		t.Fatal(response.Status)
	}
	response = request(t, http.MethodPut, server.URL+"/api/workloads/settings", `{"enabled":true}`, cookie)
	if response.StatusCode != http.StatusBadRequest {
		t.Fatal(response.Status)
	}
	// Existing CSRF middleware also protects the new controls.
	req, _ := http.NewRequest(http.MethodPut, server.URL+"/api/workloads/protection", nil)
	req.AddCookie(cookie)
	response, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusForbidden {
		t.Fatal(response.Status)
	}
}
