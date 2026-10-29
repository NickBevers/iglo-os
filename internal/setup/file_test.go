package setup

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"iglo/internal/agent"
)

func TestPlanFileKeepsAllButTheSecrets(t *testing.T) {
	plan := validPlan()
	plan.ConfirmPrivileged = true
	data, err := MarshalFile(plan)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, secret := range []string{"correct horse battery", "nas-media", "cGVyZmVjdGx5", "confirmPrivileged"} {
		if strings.Contains(text, secret) {
			t.Fatalf("the file has %q:\n%s", secret, text)
		}
	}
	for _, want := range []string{"version: 1", "server: 192.168.1.20", "share: Videos", "username: media", "email: nick@example.com", "provider: protonvpn", "locations: Netherlands", "timezone: Europe/Brussels"} {
		if !strings.Contains(text, want) {
			t.Fatalf("the file misses %q:\n%s", want, text)
		}
	}
	// The plan in memory keeps its secrets.
	if plan.NAS.Shares["videos"].Password != "nas-media" || plan.Admin.Password == "" {
		t.Fatal("writing the file took the secrets out of the plan")
	}

	loaded, err := ParseFile(data)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.NAS.Shares["photos"].Username != "photos" || loaded.Admin.Email != "nick@example.com" || !loaded.AnimeIndexers || len(loaded.Apps) != 3 {
		t.Fatalf("loaded = %+v", loaded)
	}
	// With the secrets typed again, the plan is whole.
	loaded.Admin.Password, loaded.VPN.WireGuardKey = plan.Admin.Password, plan.VPN.WireGuardKey
	loaded.VPN.PIAUsername, loaded.VPN.PIAPassword, loaded.VPN.PIADedicatedToken = plan.VPN.PIAUsername, plan.VPN.PIAPassword, plan.VPN.PIADedicatedToken
	for role, share := range loaded.NAS.Shares {
		share.Password = "typed"
		loaded.NAS.Shares[role] = share
	}
	if err := loaded.Validate(nil); err != nil {
		t.Fatal(err)
	}
}

func TestParseFileIsStrict(t *testing.T) {
	for name, file := range map[string]string{
		"empty":         "",
		"no version":    "apps: [jellyfin]",
		"other version": "version: 2",
		"unknown key":   "version: 1\nappz: [jellyfin]",
		"unknown app":   "version: 1\napps: [made-up]",
		"unknown share": "version: 1\nnas: {shares: {movies: {share: Movies}}}",
		"too large":     "version: 1\n#" + strings.Repeat("x", MaxFileSize),
	} {
		if _, err := ParseFile([]byte(file)); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: err = %v", name, err)
		}
	}
	// A password written into a file by hand is dropped.
	plan, err := ParseFile([]byte("version: 1\nadmin: {username: nick, password: written-in}\nnas: {shares: {videos: {share: Videos, username: media, password: also}}}"))
	if err != nil || plan.Admin.Password != "" || plan.NAS.Shares["videos"].Password != "" {
		t.Fatalf("plan = %+v, %v", plan, err)
	}
}

func TestExportIsTheLastPlanOrWhatIsThere(t *testing.T) {
	host := &fakeHost{installed: map[string]bool{agent.JellyfinApp: true}, requests: map[string]agent.InstallRequest{}}
	host.bindings = []agent.Binding{
		{ID: "films", Role: "videos", Source: "//192.168.1.20/Films", Mounted: true},
		{ID: "backups", Role: "backups", Source: "//192.168.1.20/Backups", Mounted: true, ProxmoxStorage: "iglo-backups"},
	}
	dir := t.TempDir()
	service := New(Options{Apps: host, Agent: host, Backups: fakeBackups{host}, Workloads: fakeWorkloads{host, ""}, DataDir: dir, Poll: time.Millisecond})

	// Without a setup, the file describes the server as it is.
	data, err := service.ExportPlan(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	plan, err := ParseFile(data)
	if err != nil || plan.NAS.Server != "192.168.1.20" || plan.NAS.Shares["videos"].Share != "Films" || !plan.ProxmoxBackups || len(plan.Apps) != 1 || plan.Apps[0] != agent.JellyfinApp {
		t.Fatalf("plan = %+v, %v", plan, err)
	}

	// After a setup starts, the file is its plan.
	run := validPlan()
	run.Apps, run.ProxmoxBackups = []string{agent.MediaStackApp}, false
	if err := service.Start(context.Background(), run); err != nil {
		t.Fatal(err)
	}
	waitFor(t, service, StateDone)
	saved, err := os.ReadFile(filepath.Join(dir, planFile))
	if err != nil || strings.Contains(string(saved), "correct horse battery") {
		t.Fatalf("saved:\n%s\n%v", saved, err)
	}
	data, _ = service.ExportPlan(context.Background())
	if plan, _ := ParseFile(data); len(plan.Apps) != 1 || plan.Apps[0] != agent.MediaStackApp {
		t.Fatalf("plan = %+v", plan)
	}
	if info, _ := os.Stat(filepath.Join(dir, planFile)); info.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %v", info.Mode())
	}
}

func TestOldPlanFilesCallLocationsCountries(t *testing.T) {
	plan, err := ParseFile([]byte("version: 1\nvpn: { countries: Netherlands, wireguardKey: secret }\n"))
	if err != nil || plan.VPN.Locations != "Netherlands" || plan.VPN.Countries != "" || plan.VPN.WireGuardKey != "" {
		t.Fatalf("plan = %+v, %v", plan.VPN, err)
	}
	if settings := plan.VPN.settings(); settings.Provider != "protonvpn" || settings.Locations != "Netherlands" {
		t.Fatalf("settings = %+v", settings)
	}
}

func TestDocsExampleParses(t *testing.T) {
	doc, err := os.ReadFile("../../docs/setup.md")
	if err != nil {
		t.Fatal(err)
	}
	_, example, _ := strings.Cut(string(doc), "```yaml\n")
	example, _, _ = strings.Cut(example, "```")
	plan, err := ParseFile([]byte(example))
	if err != nil || len(plan.Apps) != 5 || plan.NAS.Shares["videos"].Username != "media" {
		t.Fatalf("plan = %+v, %v", plan, err)
	}
}
