package agent

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strconv"

	"iglo/internal/shrink"
)

// RegisterShrink relays through the existing Inbox loopback transport. The
// worker repeats filesystem checks because only it can inspect the share.
func RegisterShrink(mux *http.ServeMux) {
	for _, route := range []struct{ method, path string }{
		{"GET", "/library"}, {"POST", "/library/scan"}, {"GET", "/shrink"}, {"POST", "/shrink"}, {"PUT", "/shrink/settings"}, {"POST", "/shrink/cancel/{id}"}, {"POST", "/shrink/rescanned/{id}"},
	} {
		mux.HandleFunc(route.method+" /v1/inbox/{vmid}"+route.path, func(w http.ResponseWriter, r *http.Request) {
			vmid, _ := strconv.Atoi(r.PathValue("vmid"))
			target := route.path
			var in any
			switch target {
			case "/shrink":
				if route.method == "POST" {
					var body shrink.Input
					if json.NewDecoder(http.MaxBytesReader(w, r.Body, 512<<10)).Decode(&body) != nil || shrink.Validate(body) != nil {
						http.Error(w, "invalid shrink request", 400)
						return
					}
					in = body
				}
			case "/shrink/settings":
				var body shrink.Settings
				if json.NewDecoder(http.MaxBytesReader(w, r.Body, 512)).Decode(&body) != nil || body.Paused == nil {
					http.Error(w, "give paused", 400)
					return
				}
				in = body
			case "/shrink/cancel/{id}", "/shrink/rescanned/{id}":
				id := r.PathValue("id")
				b, e := hex.DecodeString(id)
				if e != nil || len(b) != 16 {
					http.Error(w, "invalid job", 400)
					return
				}
				target = target[:len(target)-4] + id
			}
			var out json.RawMessage
			var output any
			if route.method == "GET" || (route.path == "/shrink" && route.method == "POST") {
				output = &out
			}
			if err := inboxCall(r.Context(), vmid, route.method, target, in, output); err != nil {
				http.Error(w, err.Error(), 502)
				return
			}
			if output != nil {
				w.Header().Set("Content-Type", "application/json")
				if route.method == "POST" {
					w.WriteHeader(202)
				}
				w.Write(out)
			} else if route.path == "/library/scan" {
				w.WriteHeader(202)
			} else {
				w.WriteHeader(204)
			}
		})
	}
}
func (c *Client) Library(ctx context.Context, id int) (shrink.Library, error) {
	var out shrink.Library
	err := c.do(ctx, "GET", "/v1/inbox/"+strconv.Itoa(id)+"/library", nil, &out)
	return out, err
}
func (c *Client) ShrinkJobs(ctx context.Context, id int) (shrink.View, error) {
	var out shrink.View
	err := c.do(ctx, "GET", "/v1/inbox/"+strconv.Itoa(id)+"/shrink", nil, &out)
	return out, err
}
func (c *Client) SubmitShrink(ctx context.Context, id int, in shrink.Input) ([]shrink.Job, error) {
	var out []shrink.Job
	err := c.do(ctx, "POST", "/v1/inbox/"+strconv.Itoa(id)+"/shrink", in, &out)
	return out, err
}
func (c *Client) ScanLibrary(ctx context.Context, id int) error {
	return c.do(ctx, "POST", "/v1/inbox/"+strconv.Itoa(id)+"/library/scan", nil, nil)
}
func (c *Client) ShrinkSettings(ctx context.Context, id int, paused bool) error {
	return c.do(ctx, "PUT", "/v1/inbox/"+strconv.Itoa(id)+"/shrink/settings", shrink.Settings{Paused: &paused}, nil)
}
func (c *Client) CancelShrink(ctx context.Context, id int, job string) error {
	return c.do(ctx, "POST", "/v1/inbox/"+strconv.Itoa(id)+"/shrink/cancel/"+job, nil, nil)
}
func (c *Client) ShrinkRescanned(ctx context.Context, id int, job string) error {
	return c.do(ctx, "POST", "/v1/inbox/"+strconv.Itoa(id)+"/shrink/rescanned/"+job, nil, nil)
}
