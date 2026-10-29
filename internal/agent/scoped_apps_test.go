package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const backupPassword = "correct horse battery staple"

func TestBackupPasswordIsChecked(t *testing.T) {
	good := ScopedAnswers{Storage: "local-lvm", LibraryBinding: "photos", BackupBinding: "backups", Cores: 4, MemoryMiB: 8192, DiskGB: 64, BackupPassword: backupPassword}
	if err := good.ValidateFor(PhotosApp); err != nil {
		t.Fatal(err)
	}
	for name, password := range map[string]string{"missing": "", "short": "elevenchars", "line break": "twelve chars\nKEEP_DAILY=0", "too long": strings.Repeat("x", 129)} {
		bad := good
		bad.BackupPassword = password
		if bad.ValidateFor(PhotosApp) == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	music := ScopedAnswers{Storage: "local-lvm", LibraryBinding: "music", Cores: 2, MemoryMiB: 1024, DiskGB: 64, BackupPassword: backupPassword}
	if music.ValidateFor(MusicApp) == nil {
		t.Error("Music took a backup password")
	}
}

func TestScopedInstallerKeepsShareIDsForRetry(t *testing.T) {
	dir := t.TempDir()
	stacks := filepath.Join(dir, "stacks")
	os.MkdirAll(filepath.Join(stacks, "photos"), 0700)
	os.WriteFile(filepath.Join(stacks, "photos", "install.sh"), nil, 0700)
	started := false
	i := NewAppInstallerWith(filepath.Join(dir, "state"), stacks, func(_ string, args []string, cleanup, _, _ string) error {
		data, err := os.ReadFile(cleanup)
		if err != nil {
			t.Fatal(err)
		}
		// The answers file is 0600 and the installer removes it. Only the
		// backup password is in it, for the installer to seal.
		if !strings.Contains(string(data), "LIBRARY_BINDING=photos\n") || strings.Count(string(data), "PASSWORD") != 1 ||
			!strings.Contains(string(data), "BACKUP_PASSWORD="+backupPassword+"\n") {
			t.Fatalf("unsafe answers: %s", data)
		}
		// Like the unit of the real installer, which removes the answers when it ends.
		os.Remove(cleanup)
		started = true
		return nil
	}, func() bool { return false })
	a := ScopedAnswers{Storage: "local-lvm", LibraryBinding: "photos", BackupBinding: "backups", Cores: 4, MemoryMiB: 8192, DiskGB: 64, BackupPassword: backupPassword}
	if err := i.Install(PhotosApp, InstallRequest{Scoped: a}); err != nil {
		t.Fatal(err)
	}
	if !started {
		t.Fatal("installer did not run")
	}
	saved, err := i.Saved(PhotosApp)
	withoutPassword := a
	withoutPassword.BackupPassword = ""
	if err != nil || saved.Scoped != withoutPassword {
		t.Fatalf("retry lost binding IDs: %+v %v", saved, err)
	}
	// The backup password is in no file the agent keeps, and a retry asks for it again.
	filepath.WalkDir(filepath.Join(dir, "state"), func(path string, entry os.DirEntry, err error) error {
		if data, _ := os.ReadFile(path); err == nil && !entry.IsDir() && strings.Contains(string(data), backupPassword) {
			t.Errorf("%s keeps the backup password", path)
		}
		return nil
	})
	if err := i.Retry(PhotosApp); err == nil || !strings.Contains(err.Error(), "backup password is not kept") {
		t.Fatalf("retry without the password: %v", err)
	}
	for _, bad := range []ScopedAnswers{{Storage: "local-lvm", LibraryBinding: "photos/../../etc", BackupBinding: "backups", Cores: 4, MemoryMiB: 8192, DiskGB: 64}, {Storage: "local-lvm", LibraryBinding: "photos", BackupBinding: "photos", Cores: 4, MemoryMiB: 8192, DiskGB: 64}, {Storage: "local-lvm", LibraryBinding: "photos", BackupBinding: "backups", Cores: 4, MemoryMiB: 32768, DiskGB: 64}} {
		if bad.ValidateFor(PhotosApp) == nil {
			t.Fatal("accepted invalid scoped answers")
		}
	}
}
