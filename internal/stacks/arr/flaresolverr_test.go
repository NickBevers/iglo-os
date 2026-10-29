package arr

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// fakeProwlarr keeps tags and indexer proxies in memory, like Prowlarr's API v1.
type fakeProwlarr struct {
	tags    []map[string]any
	proxies []map[string]any
	deleted []string
}

func (f *fakeProwlarr) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("X-Api-Key") != "prowlarr-key" {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/api/v1/system/status":
		w.Write([]byte(`{}`))
	case r.Method == http.MethodGet && r.URL.Path == "/api/v1/tag":
		json.NewEncoder(w).Encode(f.tags)
	case r.Method == http.MethodPost && r.URL.Path == "/api/v1/tag":
		tag := map[string]any{"id": len(f.tags) + 1, "label": "flaresolverr"}
		f.tags = append(f.tags, tag)
		json.NewEncoder(w).Encode(tag)
	case r.Method == http.MethodGet && r.URL.Path == "/api/v1/indexerProxy":
		json.NewEncoder(w).Encode(f.proxies)
	case r.Method == http.MethodGet && r.URL.Path == "/api/v1/indexerProxy/schema":
		w.Write([]byte(`[{"implementation":"FlareSolverr","tags":[],"fields":[{"name":"host","value":"http://localhost:8191/"},{"name":"requestTimeout","value":60}]}]`))
	case r.Method == http.MethodPost && r.URL.Path == "/api/v1/indexerProxy":
		var proxy map[string]any
		json.NewDecoder(r.Body).Decode(&proxy)
		proxy["id"] = float64(len(f.proxies) + 10)
		f.proxies = append(f.proxies, proxy)
	case r.Method == http.MethodPut && strings.HasPrefix(r.URL.Path, "/api/v1/indexerProxy/"):
		var proxy map[string]any
		json.NewDecoder(r.Body).Decode(&proxy)
		for index := range f.proxies {
			if fmt.Sprint(f.proxies[index]["id"]) == strings.TrimPrefix(r.URL.Path, "/api/v1/indexerProxy/") {
				f.proxies[index] = proxy
			}
		}
	case r.Method == http.MethodDelete && strings.HasPrefix(r.URL.Path, "/api/v1/indexerProxy/"):
		id := strings.TrimPrefix(r.URL.Path, "/api/v1/indexerProxy/")
		f.deleted = append(f.deleted, id)
		f.proxies = slices.DeleteFunc(f.proxies, func(proxy map[string]any) bool {
			return fmt.Sprint(proxy["id"]) == id
		})
	default:
		http.NotFound(w, r)
	}
}

func jsonString(value any) string {
	data, _ := json.Marshal(value)
	return string(data)
}

func TestSetFlareSolverr(t *testing.T) {
	fake := &fakeProwlarr{}
	server := httptest.NewServer(fake)
	defer server.Close()

	envPath := filepath.Join(t.TempDir(), ".env")
	os.WriteFile(envPath, []byte("TZ=Europe/Brussels\nPROWLARR_API_KEY=prowlarr-key\nFLARESOLVERR=n\nANIME_INDEXERS=y\nCOMPOSE_PROFILES=\nVPN_PROVIDER=pia\n"), 0o600)
	logf := func(string, ...any) {}

	if err := SetFlareSolverr(context.Background(), true, envPath, server.URL, logf); err != nil {
		t.Fatal(err)
	}
	if len(fake.proxies) != 1 || fake.proxies[0]["name"] != "FlareSolverr" || jsonString(fake.proxies[0]["tags"]) != "[1]" {
		t.Fatalf("proxies after on: %v", fake.proxies)
	}
	data, _ := os.ReadFile(envPath)
	if want := "TZ=Europe/Brussels\nPROWLARR_API_KEY=prowlarr-key\nANIME_INDEXERS=y\nVPN_PROVIDER=pia\nFLARESOLVERR=y\nCOMPOSE_PROFILES=flaresolverr\n"; string(data) != want {
		t.Fatalf(".env after on:\n%s", data)
	}

	// Turning it on again changes nothing.
	if err := SetFlareSolverr(context.Background(), true, envPath, server.URL, logf); err != nil || len(fake.proxies) != 1 || len(fake.tags) != 1 {
		t.Fatalf("on twice: %v, %v, %v", err, fake.proxies, fake.tags)
	}

	// Off deletes only the proxy of the stack, and keeps the tag.
	fake.proxies = append(fake.proxies, map[string]any{"id": float64(30), "implementation": "FlareSolverr", "fields": []any{map[string]any{"name": "host", "value": "http://192.168.1.5:8191"}}})
	if err := SetFlareSolverr(context.Background(), false, envPath, server.URL, logf); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(fake.deleted, []string{"10"}) || len(fake.proxies) != 1 || len(fake.tags) != 1 {
		t.Fatalf("off: deleted %v, proxies %v, tags %v", fake.deleted, fake.proxies, fake.tags)
	}
	data, _ = os.ReadFile(envPath)
	if !strings.HasSuffix(string(data), "FLARESOLVERR=n\nCOMPOSE_PROFILES=\n") {
		t.Fatalf(".env after off:\n%s", data)
	}
}

func TestSetFlareSolverrNeedsTheAPIKey(t *testing.T) {
	envPath := filepath.Join(t.TempDir(), ".env")
	os.WriteFile(envPath, []byte("FLARESOLVERR=n\n"), 0o600)
	if err := SetFlareSolverr(context.Background(), true, envPath, "http://127.0.0.1:1", func(string, ...any) {}); err == nil || !strings.Contains(err.Error(), "PROWLARR_API_KEY") {
		t.Fatalf("err = %v", err)
	}
}

func TestFlareSolverrEnv(t *testing.T) {
	// A stack from before FlareSolverr was optional has neither line.
	if got := flareSolverrEnv("TZ=UTC\n", false); got != "TZ=UTC\nFLARESOLVERR=n\nCOMPOSE_PROFILES=\n" {
		t.Fatalf("old stack: %q", got)
	}
	// Other profiles stay.
	if got := flareSolverrEnv("COMPOSE_PROFILES=extra, flaresolverr\nFLARESOLVERR=y\n", false); got != "FLARESOLVERR=n\nCOMPOSE_PROFILES=extra\n" {
		t.Fatalf("other profile: %q", got)
	}
	if got := flareSolverrEnv("COMPOSE_PROFILES=extra\n", true); got != "FLARESOLVERR=y\nCOMPOSE_PROFILES=extra,flaresolverr\n" {
		t.Fatalf("on with another profile: %q", got)
	}
}
