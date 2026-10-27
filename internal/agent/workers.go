package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"iglo/internal/music"
	"net/http"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

func workerGuest(ctx context.Context, app string, vmid int) error {
	if (app != MusicApp && app != InboxApp) || vmid < 100 || vmid > 999999999 {
		return ErrInvalidContainer
	}
	config, err := containerConfig(ctx, vmid)
	if err != nil {
		return errors.New("could not inspect the worker container")
	}
	for line := range strings.Lines(config) {
		if tags, ok := strings.CutPrefix(line, "tags: "); ok {
			found := map[string]bool{}
			for _, tag := range strings.Split(tags, ";") {
				found[strings.TrimSpace(tag)] = true
			}
			if found["iglo"] && found[app] {
				return nil
			}
		}
	}
	return errors.New("this container is not a managed worker")
}
func musicCall(ctx context.Context, vmid int, method, path string, body any, out any) error {
	ctx, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()
	if err := workerGuest(ctx, MusicApp, vmid); err != nil {
		return err
	}
	args := []string{"exec", strconv.Itoa(vmid), "--", "curl", "--fail-with-body", "--silent", "--show-error", "--max-time", "8", "--request", method, "http://127.0.0.1:8788" + path}
	var data []byte
	if body != nil {
		data, _ = json.Marshal(body)
		args = append(args, "--header", "Content-Type: application/json", "--data-binary", "@-")
	}
	cmd := exec.CommandContext(ctx, "pct", args...)
	cmd.Stdin = bytes.NewReader(data)
	result, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("music worker refused the request: %s", strings.TrimSpace(string(result)))
	}
	if out != nil {
		return json.Unmarshal(result, out)
	}
	return nil
}
func SetWorkerAdmission(ctx context.Context, app string, vmid int, until int64) error {
	if until < -1 || (until > 0 && (until < time.Now().Unix() || until > time.Now().Add(2*time.Minute).Unix())) {
		return ErrInvalidAnswers
	}
	ctx, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()
	if err := workerGuest(ctx, app, vmid); err != nil {
		return err
	}
	path := "/var/lib/iglo-music"
	if app == InboxApp {
		path = "/var/lib/iglo-inbox/admission"
	}
	// Only fixed paths and a checked integer reach this constant shell program.
	cmd := exec.CommandContext(ctx, "pct", "exec", strconv.Itoa(vmid), "--", "sh", "-c", `set -eu; test -d "$1"; printf '%s\n' "$2" > "$1/admit-until.tmp"; chmod 0644 "$1/admit-until.tmp"; mv "$1/admit-until.tmp" "$1/admit-until"`, "admission", path, strconv.FormatInt(until, 10))
	if err := cmd.Run(); err != nil {
		return errors.New("could not update worker admission")
	}
	return nil
}
func RegisterWorkers(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/music/{vmid}", func(w http.ResponseWriter, r *http.Request) {
		id, _ := strconv.Atoi(r.PathValue("vmid"))
		var view music.View
		if err := musicCall(r.Context(), id, "GET", "/jobs", nil, &view); err != nil {
			http.Error(w, err.Error(), 502)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(view)
	})
	mux.HandleFunc("POST /v1/music/{vmid}", func(w http.ResponseWriter, r *http.Request) {
		id, _ := strconv.Atoi(r.PathValue("vmid"))
		var in music.Input
		if json.NewDecoder(http.MaxBytesReader(w, r.Body, 2048)).Decode(&in) != nil {
			http.Error(w, "invalid request", 400)
			return
		}
		canonical, err := music.Canonical(in.URL)
		if err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		in.URL = canonical
		var j music.Job
		if err = musicCall(r.Context(), id, "POST", "/jobs", in, &j); err != nil {
			http.Error(w, err.Error(), 502)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(202)
		json.NewEncoder(w).Encode(j)
	})
	mux.HandleFunc("PUT /v1/music/{vmid}/pause", func(w http.ResponseWriter, r *http.Request) {
		id, _ := strconv.Atoi(r.PathValue("vmid"))
		var in struct {
			Paused *bool `json:"paused"`
		}
		if json.NewDecoder(http.MaxBytesReader(w, r.Body, 128)).Decode(&in) != nil || in.Paused == nil {
			http.Error(w, "give paused true or false", 400)
			return
		}
		if err := musicCall(r.Context(), id, "PUT", "/pause", in, nil); err != nil {
			http.Error(w, err.Error(), 502)
			return
		}
		w.WriteHeader(204)
	})
	mux.HandleFunc("PUT /v1/workers/{app}/{vmid}/admission", func(w http.ResponseWriter, r *http.Request) {
		id, _ := strconv.Atoi(r.PathValue("vmid"))
		var in struct {
			Until int64 `json:"until"`
		}
		if json.NewDecoder(http.MaxBytesReader(w, r.Body, 128)).Decode(&in) != nil {
			http.Error(w, "invalid admission lease", 400)
			return
		}
		if err := SetWorkerAdmission(r.Context(), r.PathValue("app"), id, in.Until); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		w.WriteHeader(204)
	})
}
func (c *Client) MusicJobs(ctx context.Context, vmid int) (music.View, error) {
	var v music.View
	err := c.do(ctx, http.MethodGet, "/v1/music/"+strconv.Itoa(vmid), nil, &v)
	return v, err
}

func MusicStats(ctx context.Context, vmid int) (AppStats, error) {
	var view music.View
	if err := musicCall(ctx, vmid, "GET", "/jobs", nil, &view); err != nil {
		return AppStats{}, err
	}
	counts := map[string]int64{}
	activity := []Activity{}
	for _, j := range view.Jobs {
		counts[j.State]++
		if j.State == "queued" || j.State == "running" {
			activity = append(activity, Activity{Title: j.URL, Detail: j.State, Progress: -1})
		}
	}
	return AppStats{Figures: []Figure{{Label: "Queued", Value: counts["queued"]}, {Label: "Running", Value: counts["running"]}, {Label: "Completed (recent jobs)", Value: counts["succeeded"]}, {Label: "Failed (recent jobs)", Value: counts["failed"]}}, Activity: activity}, nil
}
func (c *Client) SubmitMusic(ctx context.Context, vmid int, in music.Input) (music.Job, error) {
	var j music.Job
	err := c.do(ctx, http.MethodPost, "/v1/music/"+strconv.Itoa(vmid), in, &j)
	return j, err
}
func (c *Client) PauseMusic(ctx context.Context, vmid int, paused bool) error {
	return c.do(ctx, http.MethodPut, "/v1/music/"+strconv.Itoa(vmid)+"/pause", map[string]bool{"paused": paused}, nil)
}
func (c *Client) WorkerAdmission(ctx context.Context, app string, vmid int, until int64) error {
	if app != MusicApp && app != InboxApp {
		return ErrUnknownApp
	}
	return c.do(ctx, http.MethodPut, "/v1/workers/"+app+"/"+strconv.Itoa(vmid)+"/admission", map[string]int64{"until": until}, nil)
}
