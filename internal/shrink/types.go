// Package shrink lists and replaces selected library videos in the Inbox guest.
package shrink

import (
	"errors"
	"io/fs"
	"path"
	"strings"
	"time"
	"unicode"
)

// LibraryFolders are the defaults requested by the arr and Jellyfin installers.
// deploy/storage.sh binds the share, but has no library folder list. Keep the
// worker and arr path mapping on this one Go definition.
var LibraryFolders = [...]string{"anime", "series", "movies", "anime-movies"}

var ErrInvalid = errors.New("invalid library shrink request")

type Input struct {
	Paths   []string `json:"paths"`
	Profile string   `json:"profile"`
	Confirm string   `json:"confirm"`
}
type Settings struct {
	Paused *bool `json:"paused"`
}
type Audio struct {
	Index    int    `json:"index"`
	Codec    string `json:"codec"`
	Language string `json:"language"`
	Channels int    `json:"channels"`
}
type Estimates struct {
	Standard int64 `json:"standard"`
	High     int64 `json:"high"`
}
type File struct {
	Path      string    `json:"path"`
	Bytes     int64     `json:"bytes"`
	Modified  time.Time `json:"modified"`
	Duration  float64   `json:"duration"`
	Codec     string    `json:"codec"`
	Height    int       `json:"height"`
	BitDepth  int       `json:"bitDepth"`
	HDR       bool      `json:"hdr"`
	Audio     []Audio   `json:"audio"`
	Estimates Estimates `json:"estimates"`
	Status    string    `json:"status"`
	Reason    string    `json:"reason,omitempty"`
}
type Folder struct {
	Path    string   `json:"path"`
	Name    string   `json:"name"`
	Folders []Folder `json:"folders"`
	Files   []File   `json:"files"`
}
type Library struct {
	Kinds     []Folder  `json:"kinds"`
	Scanning  bool      `json:"scanning"`
	ScannedAt time.Time `json:"scannedAt,omitzero"`
	Error     string    `json:"error,omitempty"`
}
type Job struct {
	ID          string    `json:"id"`
	Path        string    `json:"path"`
	Profile     string    `json:"profile"`
	State       string    `json:"state"`
	Progress    float64   `json:"progress"`
	SourceBytes int64     `json:"sourceBytes"`
	ResultBytes int64     `json:"resultBytes"`
	SavedBytes  int64     `json:"savedBytes"`
	Modified    time.Time `json:"sourceModified"`
	Error       string    `json:"error,omitempty"`
	FinishedAt  time.Time `json:"finishedAt,omitzero"`
	Rescanned   bool      `json:"rescanned"`
}
type View struct {
	Paused bool  `json:"paused"`
	Jobs   []Job `json:"jobs"`
}

func ValidatePath(p string) error {
	if !fs.ValidPath(p) || p == "." || len(p) > 4096 || strings.Contains(p, "\\") || strings.ContainsFunc(p, unicode.IsControl) {
		return ErrInvalid
	}
	parts := strings.Split(p, "/")
	allowed := false
	for _, kind := range LibraryFolders {
		if parts[0] == kind {
			allowed = true
		}
	}
	if !allowed {
		return ErrInvalid
	}
	for _, part := range parts {
		if strings.HasPrefix(part, ".") {
			return ErrInvalid
		}
	}
	return nil
}
func Validate(in Input) error {
	if in.Confirm != "replace-originals" || (in.Profile != "standard" && in.Profile != "high") || len(in.Paths) == 0 || len(in.Paths) > 100 {
		return ErrInvalid
	}
	for _, p := range in.Paths {
		if ValidatePath(p) != nil || !strings.Contains(p, "/") {
			return ErrInvalid
		}
	}
	return nil
}
func titlePath(p string) string {
	parts := strings.Split(p, "/")
	if len(parts) > 1 {
		return path.Join(parts[0], parts[1])
	}
	return p
}
