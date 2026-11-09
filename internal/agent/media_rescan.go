package agent

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"iglo/internal/stacks/arr"
)

type MediaLibraryRequest struct {
	VMID  int      `json:"vmid"`
	Paths []string `json:"paths"`
}

func (i *AppInstaller) mediaLibrary(ctx context.Context, request MediaLibraryRequest, command string, out any) error {
	if request.VMID < 100 || request.VMID > 999999999 {
		return ErrInvalidContainer
	}
	if err := arr.ValidateLibraryPaths(request.Paths); err != nil {
		return err
	}
	if command != "rescan" && command != "shrink-check" {
		return errors.New("invalid library command")
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	args := []string{"exec", strconv.Itoa(request.VMID), "--", "/usr/local/bin/iglo-arr", command}
	args = append(args, request.Paths...)
	b, err := i.output(ctx, "pct", args...)
	if err != nil {
		return errors.New("media library command failed; check that the media stack is updated")
	}
	if err = json.Unmarshal(b, out); err != nil {
		return errors.New("invalid media library response")
	}
	return nil
}
func (i *AppInstaller) ShrinkCheck(ctx context.Context, r MediaLibraryRequest) ([]arr.ShrinkCheckResult, error) {
	var out []arr.ShrinkCheckResult
	err := i.mediaLibrary(ctx, r, "shrink-check", &out)
	return out, err
}
func (i *AppInstaller) RescanLibrary(ctx context.Context, r MediaLibraryRequest) ([]arr.RescanResult, error) {
	var out []arr.RescanResult
	err := i.mediaLibrary(ctx, r, "rescan", &out)
	return out, err
}
func RegisterMediaLibrary(mux *http.ServeMux, i *AppInstaller) {
	for _, command := range []string{"rescan", "shrink-check"} {
		mux.HandleFunc("POST /v1/apps/media/"+command, func(w http.ResponseWriter, r *http.Request) {
			var in MediaLibraryRequest
			if json.NewDecoder(http.MaxBytesReader(w, r.Body, 512<<10)).Decode(&in) != nil || in.VMID < 100 || in.VMID > 999999999 || arr.ValidateLibraryPaths(in.Paths) != nil {
				http.Error(w, "invalid media library request", 400)
				return
			}
			var out any
			var err error
			if command == "rescan" {
				out, err = i.RescanLibrary(r.Context(), in)
			} else {
				out, err = i.ShrinkCheck(r.Context(), in)
			}
			if err != nil {
				http.Error(w, err.Error(), 502)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(out)
		})
	}
}
func (c *Client) ShrinkCheck(ctx context.Context, r MediaLibraryRequest) ([]arr.ShrinkCheckResult, error) {
	var out []arr.ShrinkCheckResult
	err := c.do(ctx, "POST", "/v1/apps/media/shrink-check", r, &out)
	return out, err
}
func (c *Client) RescanLibrary(ctx context.Context, r MediaLibraryRequest) ([]arr.RescanResult, error) {
	var out []arr.RescanResult
	err := c.do(ctx, "POST", "/v1/apps/media/rescan", r, &out)
	return out, err
}
