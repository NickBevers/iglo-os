package apps

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"

	"iglo/internal/agent"
	"iglo/internal/proxmox"
	"iglo/internal/stacks/arr"
)

func TestMediaSettings(t *testing.T) {
	ctx := context.Background()
	settings := arr.DefaultMediaSettings()
	settings.Series.Subtitles = []string{"nl", "en"}
	fake := &fakeAgent{mediaSettings: settings}
	service := New(Options{Agent: fake, Proxmox: &fakeProxmox{resources: mediaResources("iglo;media")}})
	got, err := service.MediaSettings(ctx, "media")
	if err != nil || !reflect.DeepEqual(got, settings) || !reflect.DeepEqual(fake.mediaSettingsReads, []int{130}) {
		t.Fatalf("settings = %+v, reads = %v, error = %v", got, fake.mediaSettingsReads, err)
	}
}

func TestMediaSettingsRejectsUnavailableApps(t *testing.T) {
	for _, test := range []struct {
		name      string
		id        string
		resources []proxmox.Resource
		want      error
	}{
		{"unknown", "missing", mediaResources("iglo;media"), ErrUnknownApp},
		{"other app", "jellyfin", mediaResources("iglo;media"), ErrUnknownApp},
		{"not installed", "media", baseResources(), ErrNotInstalled},
		{"stopped", "media", append(baseResources(), proxmox.Resource{Type: "lxc", VMID: 130, Tags: "iglo;media", Status: "stopped"}), ErrNotRunning},
	} {
		t.Run(test.name, func(t *testing.T) {
			fake := &fakeAgent{}
			service := New(Options{Agent: fake, Proxmox: &fakeProxmox{resources: test.resources}})
			if _, err := service.MediaSettings(context.Background(), test.id); !errors.Is(err, test.want) {
				t.Fatalf("read error = %v, want %v", err, test.want)
			}
			if err := service.ChangeMediaSettings(context.Background(), t.Context(), test.id, arr.DefaultMediaSettings()); !errors.Is(err, test.want) {
				t.Fatalf("change error = %v, want %v", err, test.want)
			}
			if len(fake.mediaSettingsReads) != 0 || len(fake.mediaSettingsChanges) != 0 {
				t.Fatal("unavailable app reached the agent")
			}
		})
	}
}

func TestChangeMediaSettingsRejectsInvalidAndBusy(t *testing.T) {
	ctx := context.Background()
	for _, busy := range []bool{false, true} {
		t.Run(map[bool]string{false: "invalid", true: "busy"}[busy], func(t *testing.T) {
			fake := &fakeAgent{}
			service := New(Options{Agent: fake, Proxmox: &fakeProxmox{resources: mediaResources("iglo;media")}})
			settings := arr.DefaultMediaSettings()
			want := agent.ErrInvalidAnswers
			if busy {
				fake.status.State = agent.UpgradeRunning
				want = ErrBusy
			} else {
				settings.Series.Audio = "any"
			}
			if err := service.ChangeMediaSettings(ctx, t.Context(), "media", settings); !errors.Is(err, want) {
				t.Fatalf("error = %v, want %v", err, want)
			}
			if len(fake.mediaSettingsChanges) != 0 || len(fake.mediaSettingsReads) != 0 {
				t.Fatal("rejected settings reached the agent")
			}
		})
	}
}

func TestMediaSettingsAgentErrors(t *testing.T) {
	for _, test := range []struct {
		name        string
		err         error
		needsUpdate bool
	}{
		{"sentinel", agent.ErrMediaSettingsNeedsUpdate, true},
		{"HTTP text", errors.New("host agent /v1/apps/media/settings?vmid=130: 409 Conflict the media stack needs Update first to read its media settings"), true},
		{"other error", errors.New("host agent is offline"), false},
	} {
		t.Run(test.name, func(t *testing.T) {
			fake := &fakeAgent{mediaSettingsErr: test.err}
			service := New(Options{Agent: fake, Proxmox: &fakeProxmox{resources: mediaResources("iglo;media")}})
			_, readErr := service.MediaSettings(context.Background(), "media")
			changeErr := service.ChangeMediaSettings(context.Background(), t.Context(), "media", arr.DefaultMediaSettings())
			for _, err := range []error{readErr, changeErr} {
				// An old stack gets the plain sentence. Other errors keep their text.
				want := test.err.Error()
				if test.needsUpdate {
					want = ErrMediaSettingsNeedsUpdate.Error()
				}
				if err == nil || err.Error() != want || errors.Is(err, ErrMediaSettingsNeedsUpdate) != test.needsUpdate {
					t.Fatalf("error = %v, want %q and needsUpdate = %v", err, want, test.needsUpdate)
				}
			}
			if service.active != "" {
				t.Fatal("failed change started a monitor")
			}
		})
	}
}

func TestChangeMediaSettingsOperation(t *testing.T) {
	for _, succeeded := range []bool{true, false} {
		t.Run(map[bool]string{true: "succeeded", false: "failed"}[succeeded], func(t *testing.T) {
			ctx := t.Context()
			fake := &fakeAgent{flareSolverr: true}
			messages := make(chan string, 1)
			changes := make(chan struct{}, 2)
			service := New(Options{
				Agent: fake, Proxmox: &fakeProxmox{resources: mediaResources("iglo;media")}, PollInterval: time.Hour,
				Notify:  func(_ context.Context, message string) { messages <- message },
				Changed: func(string) { changes <- struct{}{} },
			})
			// Populate the card caches before the change.
			if _, err := service.Status(ctx); err != nil {
				t.Fatal(err)
			}
			settings := arr.DefaultMediaSettings()
			settings.Films.Subtitles = []string{"nl"}
			if err := service.ChangeMediaSettings(ctx, ctx, "media", settings); err != nil {
				t.Fatal(err)
			}
			<-changes
			if !reflect.DeepEqual(fake.mediaSettingsChanges, []int{130}) || !reflect.DeepEqual(fake.mediaSettings, settings) {
				t.Fatalf("change = %v, settings = %+v", fake.mediaSettingsChanges, fake.mediaSettings)
			}
			view, err := service.Status(ctx)
			if err != nil {
				t.Fatal(err)
			}
			op := appByID(t, view, "media").Operation
			if op == nil || op.Action != agent.ActionMediaSettings || op.State != agent.UpgradeRunning {
				t.Fatalf("operation = %+v", op)
			}
			if err := service.ChangeMediaSettings(ctx, ctx, "media", settings); !errors.Is(err, ErrBusy) {
				t.Fatalf("second change = %v", err)
			}
			state, want := agent.UpgradeFailed, "❌ Media settings change failed: could not reach Bazarr"
			if succeeded {
				state, want = agent.UpgradeSucceeded, "✅ Media settings of the media stack changed."
			}
			fake.set(agent.AppInstallStatus{App: "media", Action: agent.ActionMediaSettings, State: state, Message: "could not reach Bazarr", FinishedAt: time.Now()})
			service.Wake()
			select {
			case message := <-messages:
				if message != want {
					t.Fatalf("notice = %q, want %q", message, want)
				}
			case <-time.After(2 * time.Second):
				t.Fatal("no completion notice")
			}
			select {
			case <-changes:
			case <-time.After(2 * time.Second):
				t.Fatal("operation did not finish")
			}
			if _, err := service.Status(ctx); err != nil {
				t.Fatal(err)
			}
			fake.mu.Lock()
			reads := fake.flareSolverrReads
			fake.mu.Unlock()
			if reads != 2 {
				t.Fatalf("FlareSolverr cache was not reset: reads = %d", reads)
			}
		})
	}
}

func TestMediaSettingsUnavailable(t *testing.T) {
	for _, failure := range []error{
		agent.ErrMediaSettingsUnavailable,
		errors.New("host agent /v1/apps/media/settings?vmid=130: 503 Service Unavailable Bazarr is not answering yet. Try again in a minute."),
	} {
		fake := &fakeAgent{mediaSettingsErr: failure}
		service := New(Options{Agent: fake, Proxmox: &fakeProxmox{resources: mediaResources("iglo;media")}})
		_, readErr := service.MediaSettings(t.Context(), "media")
		changeErr := service.ChangeMediaSettings(t.Context(), t.Context(), "media", arr.DefaultMediaSettings())
		for _, err := range []error{readErr, changeErr} {
			if !errors.Is(err, ErrMediaSettingsUnavailable) || err.Error() != "Bazarr is not answering yet. Try again in a minute." {
				t.Fatalf("lost unavailable message: %v", err)
			}
		}
	}
}

func TestMediaSettingsUnreadable(t *testing.T) {
	for _, advice := range []string{
		"profile 1 has more than 10 distinct language codes. Fix it in Bazarr under Settings, Languages.",
		"its settings file is missing or can't be opened. Update the media stack on the Apps page.",
		"Bazarr refused the API key. Update the media stack on the Apps page.",
	} {
		want := agent.ErrMediaSettingsUnreadable.Error() + ": " + advice
		for _, failure := range []error{
			fmt.Errorf("%w: %s", agent.ErrMediaSettingsUnreadable, advice),
			errors.New("host agent /v1/apps/media/settings?vmid=130: 409 Conflict " + want),
		} {
			fake := &fakeAgent{mediaSettingsErr: failure}
			service := New(Options{Agent: fake, Proxmox: &fakeProxmox{resources: mediaResources("iglo;media")}})
			_, readErr := service.MediaSettings(t.Context(), "media")
			changeErr := service.ChangeMediaSettings(t.Context(), t.Context(), "media", arr.DefaultMediaSettings())
			for _, err := range []error{readErr, changeErr} {
				if !errors.Is(err, ErrMediaSettingsUnreadable) || err.Error() != want {
					t.Fatalf("lost unreadable message: %v", err)
				}
			}
		}
	}
}
