package agent

import (
	"fmt"
	"strings"
	"unicode"
)

const PhotosApp = "photos"
const MusicApp = "music"
const InboxApp = "inbox"
const GamingApp = "gaming"

// ScopedAnswers contain binding IDs, never NAS paths or NAS credentials.
type ScopedAnswers struct {
	Storage        string `json:"storage"`
	LibraryBinding string `json:"libraryBinding"`
	OutputBinding  string `json:"outputBinding"`
	BackupBinding  string `json:"backupBinding"`
	Cores          int    `json:"cores"`
	MemoryMiB      int    `json:"memoryMiB"`
	DiskGB         int    `json:"diskGB"`
	// Privileged confirms that Gaming may create a privileged container.
	Privileged bool `json:"privileged"`
	// BackupPassword is the password of the Photos backups, which you keep in
	// your password manager. The installer seals it on the host. It is never
	// kept for a retry and never shown again.
	BackupPassword string `json:"backupPassword,omitempty"`
}

// ValidBackupPassword is the check of the backup password, also in
// backup.sh: 12 to 128 characters, on one line.
func ValidBackupPassword(password string) bool {
	return len([]rune(password)) >= 12 && len(password) <= 128 && !strings.ContainsFunc(password, unicode.IsControl)
}

type scopedAppAnswers struct {
	App string
	ScopedAnswers
}

func ScopedApp(app string) bool {
	return app == PhotosApp || app == MusicApp || app == InboxApp || app == GamingApp
}
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
	if app == PhotosApp && !ValidBackupPassword(a.BackupPassword) {
		return bad("the backup password must be 12 to 128 characters, on one line")
	}
	if app != PhotosApp && a.BackupPassword != "" {
		return bad("only Photos has a backup password")
	}
	if app == InboxApp && !bindingID.MatchString(a.OutputBinding) {
		return bad("select a video output share binding")
	}
	if a.BackupBinding != "" && a.BackupBinding == a.LibraryBinding {
		return bad("backups need a different share binding")
	}
	if a.Privileged != (app == GamingApp) {
		return bad("only Gaming runs in a privileged container, and it needs your confirmation")
	}
	if a.Cores < 1 || a.Cores > 4 || a.DiskGB < 32 || a.DiskGB > 2048 {
		return bad("use 1–4 CPU cores and a 32–2048 GB local disk")
	}
	if app == GamingApp && (a.Cores < 2 || a.DiskGB < 64) {
		return bad("Gaming needs 2–4 CPU cores and a 64–2048 GB local disk for its emulator images")
	}
	minimum, maximum := 512, 2048
	if app == MusicApp {
		minimum = 1024
	}
	if app == PhotosApp {
		minimum, maximum = 8192, 8192
	}
	if app == GamingApp {
		minimum, maximum = 4096, 8192
	}
	if a.MemoryMiB < minimum || a.MemoryMiB > maximum {
		return bad(fmt.Sprintf("use %d–%d MiB of RAM", minimum, maximum))
	}
	return nil
}
func (a scopedAppAnswers) Validate() error { return a.ValidateFor(a.App) }

// file is the answers file for the installer, 0600 on the host. The
// installer reads the backup password from it, removes the file, and seals
// the password. The container keeps the answers without it.
func (a scopedAppAnswers) file() string {
	lines := []string{"STORAGE=" + a.Storage, "LIBRARY_BINDING=" + a.LibraryBinding, "OUTPUT_BINDING=" + a.OutputBinding, "BACKUP_BINDING=" + a.BackupBinding, fmt.Sprintf("CORES=%d\nMEMORY_MIB=%d\nDISK_GB=%d\nPRIVILEGED=%t", a.Cores, a.MemoryMiB, a.DiskGB, a.Privileged)}
	if a.App == PhotosApp {
		lines = append(lines, "BACKUP_PASSWORD="+a.BackupPassword)
	}
	return strings.Join(lines, "\n") + "\n"
}
