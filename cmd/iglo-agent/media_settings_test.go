package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"testing"

	"iglo/internal/agent"
	"iglo/internal/stacks/arr"
)

func TestMediaSettingsGetClient(t *testing.T) {
	unreadable := fmt.Errorf("%w: config.yaml is missing or unreadable. Fix it in Bazarr under Settings, Languages.", agent.ErrMediaSettingsUnreadable)
	for _, test := range []struct {
		name   string
		err    error
		status string
	}{
		{"success", nil, "200 OK"},
		{"needs update", agent.ErrMediaSettingsNeedsUpdate, "409 Conflict"},
		{"unreadable data", unreadable, "409 Conflict"},
		{"not answering", agent.ErrMediaSettingsUnavailable, "503 Service Unavailable"},
	} {
		t.Run(test.name, func(t *testing.T) {
			mux := http.NewServeMux()
			want := arr.DefaultMediaSettings()
			want.Series.Subtitles = []string{"nl"}
			registerMediaSettingsGet(mux, func(_ context.Context, vmid int) (arr.MediaSettings, error) {
				if vmid != 130 {
					t.Errorf("vmid = %d", vmid)
				}
				return want, test.err
			}, slog.New(slog.NewTextHandler(io.Discard, nil)))
			path := filepath.Join(socketDir(t), "media.sock")
			listener, err := net.Listen("unix", path)
			if err != nil {
				t.Fatal(err)
			}
			statuses := make(chan string, 1)
			server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				record := httptest.NewRecorder()
				mux.ServeHTTP(record, r)
				for key, values := range record.Header() {
					w.Header()[key] = values
				}
				w.WriteHeader(record.Code)
				w.Write(record.Body.Bytes())
				statuses <- fmt.Sprintf("%d %s", record.Code, http.StatusText(record.Code))
			})}
			t.Cleanup(func() { server.Close() })
			go server.Serve(listener)
			got, err := agent.NewClient(path).MediaSettings(t.Context(), 130)
			if status := <-statuses; status != test.status {
				t.Fatalf("status = %s, want %s", status, test.status)
			}
			if test.err == nil {
				if err != nil || !reflect.DeepEqual(got, want) {
					t.Fatalf("settings = %+v, error = %v", got, err)
				}
				return
			}
			exact := "host agent /v1/apps/media/settings?vmid=130: " + test.status + " " + test.err.Error()
			if err == nil || err.Error() != exact {
				t.Fatalf("got %v, want %s", err, exact)
			}
		})
	}
}
