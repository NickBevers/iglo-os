package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

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
		if !strings.Contains(string(data), "LIBRARY_BINDING=photos\n") || strings.Contains(string(data), "PASSWORD") {
			t.Fatalf("unsafe answers: %s", data)
		}
		started = true
		return nil
	}, func() bool { return false })
	a := ScopedAnswers{Storage: "local-lvm", LibraryBinding: "photos", BackupBinding: "backups", Cores: 4, MemoryMiB: 8192, DiskGB: 64}
	if err := i.Install(PhotosApp, InstallRequest{Scoped: a}); err != nil {
		t.Fatal(err)
	}
	if !started {
		t.Fatal("installer did not run")
	}
	saved, err := i.Saved(PhotosApp)
	if err != nil || saved.Scoped != a {
		t.Fatalf("retry lost binding IDs: %+v %v", saved, err)
	}
	for _, bad := range []ScopedAnswers{{Storage: "local-lvm", LibraryBinding: "photos/../../etc", BackupBinding: "backups", Cores: 4, MemoryMiB: 8192, DiskGB: 64}, {Storage: "local-lvm", LibraryBinding: "photos", BackupBinding: "photos", Cores: 4, MemoryMiB: 8192, DiskGB: 64}, {Storage: "local-lvm", LibraryBinding: "photos", BackupBinding: "backups", Cores: 4, MemoryMiB: 32768, DiskGB: 64}} {
		if bad.ValidateFor(PhotosApp) == nil {
			t.Fatal("accepted invalid scoped answers")
		}
	}
}
