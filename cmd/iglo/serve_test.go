package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"iglo/internal/proxmox"
)

func TestAdmissionSenderSkipsUnchangedAnswers(t *testing.T) {
	now := time.Date(2026, 10, 23, 3, 0, 0, 0, time.UTC)
	guests := []proxmox.Resource{
		{Type: "lxc", Status: "running", Node: "pve", VMID: 120, Tags: "iglo;music"},
		{Type: "lxc", Status: "running", Node: "pve", VMID: 121, Tags: "iglo;inbox"},
		{Type: "lxc", Status: "running", Node: "pve", VMID: 122, Tags: "other"},
	}
	calls := 0
	send := admissionSender(
		func(context.Context) ([]proxmox.Resource, error) { return guests, nil },
		func(context.Context, string, int, int64) error { calls++; return nil },
		func() time.Time { return now },
	)
	step := func(until int64, want int) {
		t.Helper()
		calls = 0
		if err := send(context.Background(), until); err != nil {
			t.Fatal(err)
		}
		if calls != want {
			t.Fatalf("until=%d: %d calls, want %d", until, calls, want)
		}
	}

	step(-1, 2)
	step(-1, 0)
	step(0, 2)
	step(0, 0)
	// Leases expire, so they go out again before they run low.
	lease := now.Add(time.Minute).Unix()
	step(lease, 2)
	now = now.Add(15 * time.Second)
	step(now.Add(time.Minute).Unix(), 0)
	now = now.Add(15 * time.Second)
	step(now.Add(time.Minute).Unix(), 2)
	step(0, 2)
	now = now.Add(admissionRefresh)
	step(0, 2)

	// A worker that stops and runs again gets the answer again.
	guests[0].Status = "stopped"
	step(0, 0)
	guests[0].Status = "running"
	step(0, 1)
}

func TestRedirectKeepsOnlyAppNames(t *testing.T) {
	isAppHost := func(host string) bool { return host == "seerr.iglo.local" || host == "seerr.iglo.local:80" }
	handler := redirectHandler("iglo.local", isAppHost, []byte("ca"))

	for host, want := range map[string]string{
		"seerr.iglo.local:80": "https://seerr.iglo.local/requests?x=1",
		"iglo.local":          "https://iglo.local/requests?x=1",
		"evil.example.com":    "https://iglo.local/requests?x=1",
	} {
		r := httptest.NewRequest(http.MethodGet, "/requests?x=1", nil)
		r.Host = host
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if got := w.Header().Get("Location"); got != want {
			t.Errorf("%s: Location = %q, want %q", host, got, want)
		}
	}
}
