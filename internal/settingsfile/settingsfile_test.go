package settingsfile

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"

	"iglo/internal/agent"
	"iglo/internal/backups"
	"iglo/internal/health"
	"iglo/internal/jellyfin"
	"iglo/internal/tailscale"
	"iglo/internal/updates"
	"iglo/internal/workloads"
)

type fakeUpdates struct {
	settings updates.Settings
	saved    []updates.SettingsInput
}

func (f *fakeUpdates) Settings() updates.Settings { return f.settings }
func (f *fakeUpdates) SaveSettings(input updates.SettingsInput) error {
	f.saved = append(f.saved, input)
	return nil
}

type fakeHealth struct {
	checks []health.Check
	set    [][]health.CheckInput
}

func (f *fakeHealth) Checks() []health.Check { return f.checks }
func (f *fakeHealth) SetChecks(inputs []health.CheckInput) error {
	f.set = append(f.set, inputs)
	return nil
}

type fakeBackups struct {
	view  backups.View
	err   error
	saved []agent.BackupJob
}

func (f *fakeBackups) Status(context.Context) (backups.View, error) { return f.view, f.err }
func (f *fakeBackups) SaveJob(_ context.Context, job agent.BackupJob) error {
	f.saved = append(f.saved, job)
	return nil
}

type fakeTailscale struct {
	view  tailscale.View
	saved []tailscale.Settings
	serve []bool
}

func (f *fakeTailscale) Status(context.Context) tailscale.View { return f.view }
func (f *fakeTailscale) SaveSettings(_ context.Context, settings tailscale.Settings) error {
	f.saved = append(f.saved, settings)
	return nil
}
func (f *fakeTailscale) SetServe(_ context.Context, enabled bool) error {
	f.serve = append(f.serve, enabled)
	return nil
}

type fakeJellyfin struct {
	settings jellyfin.Settings
	saved    []jellyfin.Settings
}

func (f *fakeJellyfin) Settings() jellyfin.Settings { return f.settings }
func (f *fakeJellyfin) SaveSettings(_ context.Context, input jellyfin.Settings) error {
	f.saved = append(f.saved, input)
	return nil
}

type fakeWorkloads struct {
	settings workloads.Settings
	saved    []workloads.Settings
}

func (f *fakeWorkloads) Status() workloads.View {
	view := workloads.View{Settings: f.settings, Configured: f.settings.URL != "" && f.settings.APIKey != ""}
	view.Settings.APIKey = ""
	return view
}
func (f *fakeWorkloads) SaveSettings(input workloads.Settings) error {
	f.saved = append(f.saved, input)
	return nil
}

type fakes struct {
	updates   *fakeUpdates
	health    *fakeHealth
	backups   *fakeBackups
	tailscale *fakeTailscale
	jellyfin  *fakeJellyfin
	workloads *fakeWorkloads
}

func newService() (*Service, fakes) {
	f := fakes{
		updates: &fakeUpdates{settings: updates.Settings{
			Schedule: updates.Schedule{Enabled: true, Weekday: 0, Hour: 4},
			Excluded: []int{105},
			Ntfy:     updates.NtfySettings{Server: "https://ntfy.sh", Topic: "iglo-old", Token: "tk_secrettokenabcdefghijklmnopq"},
		}},
		health: &fakeHealth{checks: []health.Check{
			{ID: "a", Name: "Jellyfin", Kind: health.HTTPCheck, Target: "http://192.168.1.20:8096"},
		}},
		backups: &fakeBackups{view: backups.View{Job: backups.JobView{
			BackupJob: agent.BackupJob{
				Enabled: true, Days: []string{}, Hour: 3, Storage: "local", Exclude: []int{},
				KeepDaily: 7, KeepWeekly: 4, KeepMonthly: 3,
			},
			Exists: true,
		}}},
		tailscale: &fakeTailscale{view: tailscale.View{
			State: "Running", Serving: true,
			Settings: tailscale.Settings{ShareSubnet: true, Subnet: "192.168.1.0/24"},
		}},
		jellyfin: &fakeJellyfin{settings: jellyfin.Settings{URL: "http://192.168.1.20:8096", APIKey: "key"}},
		workloads: &fakeWorkloads{settings: workloads.Settings{
			Enabled: true, URL: "http://192.168.1.30:2283", APIKey: "immich-secret", Node: "pve",
			ReserveGiB: 4, MaxQueues: 1, Queues: []string{"videoConversion", "smartSearch"},
			Window: true, Start: "02:00", End: "07:00", Timezone: "Europe/Brussels",
		}},
	}

	return &Service{
		Updates: f.updates, Health: f.health, Backups: f.backups,
		Tailscale: f.tailscale, Jellyfin: f.jellyfin,
		Workloads: f.workloads,
	}, f
}

func statuses(result Result) map[string]Status {
	found := map[string]Status{}
	for _, change := range result.Changes {
		found[change.Section] = change.Status
	}

	return found
}

func TestExportLeavesSecretsOutAndImportsUnchanged(t *testing.T) {
	service, _ := newService()

	data, err := service.Export(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, secret := range []string{"tk_", "apiKey", "key\n", "immich-secret"} {
		if strings.Contains(text, secret) {
			t.Fatalf("export has a secret %q:\n%s", secret, text)
		}
	}
	for _, want := range []string{"day: sun", `time: "04:00"`, "topic: iglo-old", "serve: true", "url: http://192.168.1.20:8096", "url: http://192.168.1.30:2283", "timezone: Europe/Brussels"} {
		if !strings.Contains(text, want) {
			t.Fatalf("export misses %q:\n%s", want, text)
		}
	}

	result, err := service.Import(context.Background(), data, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Changes) != 7 {
		t.Fatalf("changes = %+v", result.Changes)
	}
	for section, status := range statuses(result) {
		if status != StatusUnchanged {
			t.Fatalf("%s = %s, want unchanged", section, status)
		}
	}
}

func TestExportNotes(t *testing.T) {
	service, f := newService()
	f.backups.view.Job.Custom = "*/2:00"

	data, err := service.Export(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	if strings.Contains(text, "\nbackups:") || !strings.Contains(text, `# backups: left out, because the schedule "*/2:00"`) {
		t.Fatalf("custom schedule not left out:\n%s", text)
	}
	if _, err := Parse(data); err != nil {
		t.Fatalf("the export can't be imported: %v", err)
	}
}

func TestPreviewChangesNothing(t *testing.T) {
	service, f := newService()

	result, err := service.Import(context.Background(), []byte(`
version: 1
health:
  checks:
    - {name: Radarr, type: http, target: "http://192.168.1.30:7878"}
updates: {enabled: false, day: mon, time: "5:30", exclude: []}
`), false)
	if err != nil {
		t.Fatal(err)
	}

	got := statuses(result)
	if got["health"] != StatusChanged || got["updates"] != StatusChanged || len(got) != 2 {
		t.Fatalf("statuses = %v", got)
	}
	if len(f.health.set) != 0 || len(f.updates.saved) != 0 {
		t.Fatal("a preview saved settings")
	}
}

func TestImportApplies(t *testing.T) {
	service, f := newService()

	result, err := service.Import(context.Background(), []byte(`
version: 1
notifications: {ntfy: {topic: iglo-k3v9}}
updates: {enabled: false, day: mon, time: "5:30", exclude: [110, 105, 110]}
backups:
  enabled: true
  days: [thu, mon]
  time: "02:30"
  storage: nas
  exclude: [105]
  keep: {daily: 3, weekly: 0, monthly: 1}
health:
  checks:
    - {name: " Jellyfin ", type: http, target: "http://192.168.1.20:8096"}
    - {name: SSH, type: tcp, target: "192.168.1.2:22"}
tailscale: {serve: false, shareSubnet: false}
jellyfin: {url: "http://192.168.1.21:8096"}
`), true)
	if err != nil {
		t.Fatal(err)
	}

	for section, status := range statuses(result) {
		if status != StatusApplied {
			t.Fatalf("%s = %s", section, status)
		}
	}

	if len(f.updates.saved) != 1 {
		t.Fatalf("update settings saved %d times", len(f.updates.saved))
	}
	saved := f.updates.saved[0]
	if saved.Schedule != (updates.Schedule{Weekday: 1, Hour: 5, Minute: 30}) || len(saved.Excluded) != 2 {
		t.Fatalf("updates = %+v", saved)
	}
	if saved.Ntfy != (updates.NtfySettings{Server: "https://ntfy.sh", Topic: "iglo-k3v9"}) {
		t.Fatalf("ntfy = %+v", saved.Ntfy)
	}

	job := f.backups.saved[0]
	if job.Storage != "nas" || job.Hour != 2 || job.Minute != 30 || job.KeepDaily != 3 || len(job.Days) != 2 {
		t.Fatalf("job = %+v", job)
	}
	if checks := f.health.set[0]; len(checks) != 2 || checks[0].Name != "Jellyfin" {
		t.Fatalf("checks = %+v", checks)
	}
	if len(f.tailscale.saved) != 1 || f.tailscale.saved[0].ShareSubnet || len(f.tailscale.serve) != 1 || f.tailscale.serve[0] {
		t.Fatalf("tailscale saved %+v, serve %v", f.tailscale.saved, f.tailscale.serve)
	}
	if len(f.jellyfin.saved) != 1 || f.jellyfin.saved[0] != (jellyfin.Settings{URL: "http://192.168.1.21:8096"}) {
		t.Fatalf("jellyfin saved %+v", f.jellyfin.saved)
	}
}

func TestImportSkipsWhatNeedsSecretsOrTailscale(t *testing.T) {
	service, f := newService()
	f.jellyfin.settings = jellyfin.Settings{}
	f.tailscale.view = tailscale.View{State: "NeedsLogin"}

	result, err := service.Import(context.Background(), []byte(`
version: 1
tailscale: {serve: true, shareSubnet: false}
jellyfin: {url: "http://192.168.1.20:8096"}
`), true)
	if err != nil {
		t.Fatal(err)
	}

	got := statuses(result)
	for _, section := range []string{"tailscale", "jellyfin"} {
		if got[section] != StatusSkipped {
			t.Fatalf("%s = %s", section, got[section])
		}
	}
	if len(f.updates.saved)+len(f.tailscale.serve)+len(f.jellyfin.saved) != 0 {
		t.Fatal("a skipped section was saved")
	}
}

func TestParseRejectsBadFiles(t *testing.T) {
	for name, file := range map[string]string{
		"empty":            "",
		"no version":       "health: {checks: []}",
		"wrong version":    "version: 2",
		"unknown key":      "version: 1\nhealth: {chekcs: []}",
		"bad day":          "version: 1\nupdates: {enabled: true, day: funday, time: \"04:00\"}",
		"bad time":         "version: 1\nbackups: {enabled: true, time: \"25:00\", storage: local}",
		"bad storage":      "version: 1\nbackups: {enabled: true, time: \"03:00\", storage: \"a b\"}",
		"bad check":        "version: 1\nhealth: {checks: [{name: x, type: ping, target: y}]}",
		"bad subnet":       "version: 1\ntailscale: {shareSubnet: true, subnet: 192.168.1.1/24}",
		"bad url":          "version: 1\njellyfin: {url: ftp://nas}",
		"removed section":  "version: 1\nselfUpdate: {repo: someone/fork}",
		"removed theme":    "version: 1\njellyfin: {url: \"http://nas:8096\", theme: true}",
		"bad topic":        "version: 1\nnotifications: {ntfy: {topic: a/b}}",
		"bad queue":        "version: 1\nbackgroundWork: {reserveGiB: 4, maxQueues: 1, queues: [nope], window: {start: \"02:00\", end: \"07:00\", timezone: UTC}}",
		"bad zone":         "version: 1\nbackgroundWork: {reserveGiB: 4, maxQueues: 1, queues: [smartSearch], window: {start: \"02:00\", end: \"07:00\", timezone: Mars/Base}}",
		"control, no node": "version: 1\nbackgroundWork: {immich: {control: true, url: \"http://immich:2283\"}, reserveGiB: 4, maxQueues: 1, queues: [smartSearch], window: {start: \"02:00\", end: \"07:00\", timezone: UTC}}",
		"bad guest":        "version: 1\nupdates: {enabled: true, day: sun, time: \"04:00\", exclude: [5]}",
		"too large":        "version: 1\n#" + strings.Repeat("x", MaxSize),
	} {
		if _, err := Parse([]byte(file)); !errors.Is(err, ErrInvalidFile) {
			t.Errorf("%s: err = %v", name, err)
		}
	}
}

func TestParseErrorsAreReadable(t *testing.T) {
	for file, want := range map[string]string{
		"version: 1\nhealth: {chekcs: []}":                     "invalid settings file: line 2: unknown key chekcs",
		"version: 1\nupdates: {enabled: yes please}":           "invalid settings file: line 2: the value `yes please` has the wrong type",
		"version: 1\nbackups: {keep: {daily: many}, time: x}":  "invalid settings file: line 2: the value `many` has the wrong type",
		"version: 1\nhealth: {checks: [{name: a, type: [x]}]}": "invalid settings file: line 2: a value has the wrong type",
		"version: 1\n  bad: indent":                            "invalid settings file: line 2: mapping values are not allowed in this context",
	} {
		_, err := Parse([]byte(file))
		if err == nil || err.Error() != want {
			t.Errorf("%q: err = %v, want %q", file, err, want)
		}
	}
}

func TestDocsExampleParses(t *testing.T) {
	doc, err := os.ReadFile("../../docs/settings-file.md")
	if err != nil {
		t.Fatal(err)
	}
	_, example, _ := strings.Cut(string(doc), "```yaml\n")
	example, _, _ = strings.Cut(example, "```")

	file, err := Parse([]byte(example))
	if err != nil {
		t.Fatal(err)
	}
	if file.Health == nil || len(file.Health.Checks) != 2 || file.BackgroundWork == nil {
		t.Fatalf("file = %+v", file)
	}
}

func TestImportBackgroundWork(t *testing.T) {
	file := func(control bool, url string) []byte {
		return []byte(fmt.Sprintf(`
version: 1
backgroundWork:
  immich: {control: %t, url: %q, node: pve}
  workers: true
  reserveGiB: 6
  maxQueues: 2
  queues: [smartSearch, faceDetection]
  window: {enabled: false, start: "02:00", end: "07:00", timezone: Europe/Brussels}
`, control, url))
	}

	// Same server: saved, and the empty API key keeps the saved one.
	service, f := newService()
	result, err := service.Import(context.Background(), file(true, "http://192.168.1.30:2283/"), true)
	if err != nil || statuses(result)["backgroundWork"] != StatusApplied || len(f.workloads.saved) != 1 {
		t.Fatalf("result = %+v, %v, saved = %+v", result, err, f.workloads.saved)
	}
	saved := f.workloads.saved[0]
	if saved.APIKey != "" || saved.URL != "http://192.168.1.30:2283" || !saved.Workers || saved.ReserveGiB != 6 || saved.Window {
		t.Fatalf("saved = %+v", saved)
	}

	// Another server never gets the saved key.
	service, f = newService()
	result, _ = service.Import(context.Background(), file(false, "http://192.168.1.99:2283"), true)
	if statuses(result)["backgroundWork"] != StatusSkipped || len(f.workloads.saved) != 0 {
		t.Fatalf("result = %+v", result)
	}

	// Control needs a key first. Without control, the rest is saved.
	service, f = newService()
	f.workloads.settings.APIKey, f.workloads.settings.Enabled = "", false
	result, _ = service.Import(context.Background(), file(true, "http://192.168.1.30:2283"), true)
	if statuses(result)["backgroundWork"] != StatusSkipped || len(f.workloads.saved) != 0 {
		t.Fatalf("result = %+v", result)
	}
	result, _ = service.Import(context.Background(), file(false, "http://192.168.1.30:2283"), true)
	if statuses(result)["backgroundWork"] != StatusApplied || len(f.workloads.saved) != 1 {
		t.Fatalf("result = %+v", result)
	}
}

func TestImportKeepsNtfy(t *testing.T) {
	service, f := newService()
	f.updates.settings.Ntfy = updates.NtfySettings{Server: "https://ntfy.example.org", Topic: "alerts", Token: "tk_AgQdq7mVBoFD37zQVN29RhuMzNIz2"}

	// The token is never in the file.
	exported, err := service.Export(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(exported), "topic: alerts") || strings.Contains(string(exported), "tk_") {
		t.Fatalf("exported:\n%s", exported)
	}

	// A file without ntfy keeps it, and so does a file with the same ntfy.
	for _, notifications := range []string{
		`{}`,
		`{ntfy: {server: "https://ntfy.example.org/", topic: alerts}}`,
	} {
		result, err := service.Import(context.Background(), []byte("version: 1\nnotifications: "+notifications), true)
		if err != nil {
			t.Fatal(err)
		}
		if status := statuses(result)["notifications"]; status != StatusUnchanged {
			t.Fatalf("%s: %s", notifications, status)
		}
	}

	// An empty topic turns it off.
	if _, err := service.Import(context.Background(), []byte("version: 1\nnotifications: {ntfy: {topic: \"\"}}"), true); err != nil {
		t.Fatal(err)
	}
	if len(f.updates.saved) != 1 || f.updates.saved[0].Ntfy != (updates.NtfySettings{}) {
		t.Fatalf("saved = %+v", f.updates.saved)
	}
}
