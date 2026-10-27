package proxmox

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"

	"iglo/internal/config"
)

func newTestClient(t *testing.T, handler http.HandlerFunc) *Client {
	t.Helper()

	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	client, err := New(config.Proxmox{URL: server.URL, TokenID: "iglo@pve!manager", TokenSecret: "secret"})
	if err != nil {
		t.Fatal(err)
	}

	return client
}

func TestResourcesSendsTokenAndDecodesData(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api2/json/cluster/resources" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "PVEAPIToken=iglo@pve!manager=secret" {
			t.Errorf("unexpected auth header %q", got)
		}
		w.Write([]byte(`{"data":[{"id":"lxc/101","type":"lxc","node":"pve","vmid":101,"name":"jellyfin","status":"running","cpu":0.25}]}`))
	})

	resources, err := client.Resources(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	if len(resources) != 1 || resources[0].Name != "jellyfin" || resources[0].CPU != 0.25 {
		t.Fatalf("unexpected resources: %+v", resources)
	}
}

func TestRunGuestActionPostsToStatusEndpoint(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api2/json/nodes/pve/lxc/101/status/reboot" {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		w.Write([]byte(`{"data":"UPID:pve:0001"}`))
	})

	upid, err := client.RunGuestAction(context.Background(), "pve", LXC, 101, Reboot)
	if err != nil {
		t.Fatal(err)
	}
	if upid != "UPID:pve:0001" {
		t.Fatalf("unexpected upid %q", upid)
	}
}

func TestErrorStatusIsReturned(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "permission check failed", http.StatusForbidden)
	})

	if _, err := client.Resources(context.Background()); err == nil {
		t.Fatal("expected error")
	}
}

func TestResourcesAreSharedForAMoment(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		switch r.URL.Path {
		case "/api2/json/cluster/resources":
			w.Write([]byte(`{"data":[{"type":"node","node":"pve","status":"online"}]}`))
		case "/api2/json/nodes/pve/status":
			w.Write([]byte(`{"data":{"pveversion":"pve-manager/9.2.0"}}`))
		default:
			w.Write([]byte(`{"data":"UPID:pve:1"}`))
		}
	}))
	defer server.Close()
	client, err := New(config.Proxmox{URL: server.URL, TokenID: "iglo@pve!test", TokenSecret: "secret"})
	if err != nil {
		t.Fatal(err)
	}

	var wait sync.WaitGroup
	for range 10 {
		wait.Go(func() {
			if resources, err := client.Resources(t.Context()); err != nil || len(resources) != 1 {
				t.Errorf("resources = %v, %v", resources, err)
			}
		})
	}
	wait.Wait()
	resources, _ := client.Resources(t.Context())
	resources[0].Status = "changed by a caller"
	if again, _ := client.Resources(t.Context()); again[0].Status != "online" {
		t.Fatal("a caller changed the shared answer")
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("calls = %d, want 1", got)
	}

	// A guest action asks again.
	client.RunGuestAction(t.Context(), "pve", LXC, 190, Start)
	client.Resources(t.Context())
	if got := calls.Load(); got != 3 {
		t.Fatalf("calls = %d, want 3", got)
	}

	// The version of a node is kept longer.
	for range 3 {
		if status, err := client.NodeStatus(t.Context(), "pve"); err != nil || status.PVEVersion != "pve-manager/9.2.0" {
			t.Fatalf("status = %v, %v", status, err)
		}
	}
	if got := calls.Load(); got != 4 {
		t.Fatalf("calls = %d, want 4", got)
	}
}
