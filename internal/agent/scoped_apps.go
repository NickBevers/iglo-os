package agent

import (
	"fmt"
	"strings"
)

const PhotosApp = "photos"
const MusicApp = "music"
const InboxApp = "inbox"

// ScopedAnswers contain binding IDs, never NAS paths or credentials.
type ScopedAnswers struct {
	Storage        string `json:"storage"`
	LibraryBinding string `json:"libraryBinding"`
	OutputBinding  string `json:"outputBinding"`
	BackupBinding  string `json:"backupBinding"`
	Cores          int    `json:"cores"`
	MemoryMiB      int    `json:"memoryMiB"`
	DiskGB         int    `json:"diskGB"`
}
type scopedAppAnswers struct {
	App string
	ScopedAnswers
}

func ScopedApp(app string) bool { return app == PhotosApp || app == MusicApp || app == InboxApp }
func (a ScopedAnswers) ValidateFor(app string) error {
	bad := func(message string) error { return fmt.Errorf("%w: %s", ErrInvalidAnswers, message) }
	if !ScopedApp(app) {
		return ErrUnknownApp
	}
	if !storageID.MatchString(a.Storage) || !bindingID.MatchString(a.LibraryBinding) {
		return bad("select container storage and a library share binding")
	}
	if app == PhotosApp && !bindingID.MatchString(a.BackupBinding) {
		return bad("select a separate backups share binding")
	}
	if app == InboxApp && !bindingID.MatchString(a.OutputBinding) {
		return bad("select a video output share binding")
	}
	if a.BackupBinding != "" && a.BackupBinding == a.LibraryBinding {
		return bad("backups need a different share binding")
	}
	if a.Cores < 1 || a.Cores > 4 || a.DiskGB < 32 || a.DiskGB > 2048 {
		return bad("use 1–4 CPU cores and a 32–2048 GB local disk")
	}
	minimum, maximum := 512, 2048
	if app == MusicApp {
		minimum = 1024
	}
	if app == PhotosApp {
		minimum, maximum = 8192, 8192
	}
	if a.MemoryMiB < minimum || a.MemoryMiB > maximum {
		return bad(fmt.Sprintf("use %d–%d MiB of RAM", minimum, maximum))
	}
	return nil
}
func (a scopedAppAnswers) Validate() error { return a.ValidateFor(a.App) }
func (a scopedAppAnswers) file() string {
	return strings.Join([]string{"STORAGE=" + a.Storage, "LIBRARY_BINDING=" + a.LibraryBinding, "OUTPUT_BINDING=" + a.OutputBinding, "BACKUP_BINDING=" + a.BackupBinding, fmt.Sprintf("CORES=%d\nMEMORY_MIB=%d\nDISK_GB=%d", a.Cores, a.MemoryMiB, a.DiskGB)}, "\n") + "\n"
}
