package agent

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strconv"

	"iglo/internal/inbox"
)

func inboxCall(ctx context.Context, id int, method, path string, in, out any) error {
	return nativeWorkerCall(ctx, InboxApp, id, method, path, in, out)
}
func InboxJobs(ctx context.Context, id int) (inbox.View, error) {
	var view inbox.View
	err := inboxCall(ctx, id, "GET", "/jobs", nil, &view)
	return view, err
}
func RegisterInbox(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/inbox/{vmid}", func(w http.ResponseWriter, r *http.Request) {
		id, _ := strconv.Atoi(r.PathValue("vmid"))
		view, err := InboxJobs(r.Context(), id)
		if err != nil {
			http.Error(w, err.Error(), 502)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(view)
	})
	mux.HandleFunc("GET /v1/inbox/{vmid}/events", func(w http.ResponseWriter, r *http.Request) {
		id, _ := strconv.Atoi(r.PathValue("vmid"))
		if err := workerGuest(r.Context(), InboxApp, id); err != nil {
			http.Error(w, "Inbox is unavailable", 502)
			return
		}
		relayGuestStream(w, r, id, []string{"http://127.0.0.1:8789/events"}, "inbox", func(name string) bool { return name == "jobs" })
	})
	for _, route := range []struct{ pattern, method, path string }{
		{"POST /v1/inbox/{vmid}", "POST", "/jobs"},
		{"PUT /v1/inbox/{vmid}/settings", "PUT", "/settings"},
		{"POST /v1/inbox/{vmid}/scan", "POST", "/scan"},
		{"POST /v1/inbox/{vmid}/retry/{id}", "POST", ""},
	} {
		mux.HandleFunc(route.pattern, func(w http.ResponseWriter, r *http.Request) {
			id, _ := strconv.Atoi(r.PathValue("vmid"))
			path := route.path
			var body any
			switch path {
			case "/jobs":
				var in inbox.Input
				if json.NewDecoder(http.MaxBytesReader(w, r.Body, 2048)).Decode(&in) != nil || inbox.Validate(in) != nil {
					http.Error(w, "invalid video request", 400)
					return
				}
				body = in
			case "/settings":
				var in inbox.Settings
				if json.NewDecoder(http.MaxBytesReader(w, r.Body, 512)).Decode(&in) != nil || in.Paused == nil || inbox.Validate(inbox.Input{Path: "clip.mp4", Profile: in.Profile}) != nil {
					http.Error(w, "give paused and a supported profile", 400)
					return
				}
				body = in
			case "":
				job := r.PathValue("id")
				decoded, err := hex.DecodeString(job)
				if err != nil || len(decoded) != 16 {
					http.Error(w, "invalid job", 400)
					return
				}
				path = "/jobs/" + job + "/retry"
			}
			var out any
			if path == "/jobs" {
				out = &inbox.Job{}
			}
			if err := inboxCall(r.Context(), id, route.method, path, body, out); err != nil {
				http.Error(w, err.Error(), 502)
				return
			}
			if out != nil {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(202)
				json.NewEncoder(w).Encode(out)
			} else {
				w.WriteHeader(204)
			}
		})
	}
}
func (c *Client) InboxJobs(ctx context.Context, id int) (inbox.View, error) {
	var v inbox.View
	err := c.do(ctx, "GET", "/v1/inbox/"+strconv.Itoa(id), nil, &v)
	return v, err
}
func (c *Client) InboxCommand(ctx context.Context, id int, command string, in any) error {
	method := "POST"
	if command == "settings" {
		method = "PUT"
	}
	return c.do(ctx, method, "/v1/inbox/"+strconv.Itoa(id)+"/"+command, in, nil)
}
func (c *Client) SubmitInbox(ctx context.Context, id int, in inbox.Input) (inbox.Job, error) {
	var j inbox.Job
	err := c.do(ctx, "POST", "/v1/inbox/"+strconv.Itoa(id), in, &j)
	return j, err
}
func (c *Client) InboxEvents(ctx context.Context, id int, onEvent func(string)) error {
	return c.follow(ctx, "/v1/inbox/"+strconv.Itoa(id)+"/events", onEvent)
}

func InboxStats(ctx context.Context, id int) (AppStats, error) {
	v, err := InboxJobs(ctx, id)
	if err != nil {
		return AppStats{}, err
	}
	counts := map[string]int64{}
	activity := []Activity{}
	for _, j := range v.Jobs {
		counts[j.State]++
		if j.State == "queued" || j.State == "running" {
			activity = append(activity, Activity{Title: j.Path, Detail: j.State, Progress: -1})
		}
	}
	return AppStats{Figures: []Figure{{Label: "Queued", Value: counts["queued"]}, {Label: "Running", Value: counts["running"]}, {Label: "Completed (recent jobs)", Value: counts["succeeded"]}, {Label: "Failed (recent jobs)", Value: counts["failed"]}}, Activity: activity}, nil
}
