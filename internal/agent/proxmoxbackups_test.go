package agent

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDirStoragesReadsStorageCfg(t *testing.T) {
	config := "dir: local\n\tpath /var/lib/vz\n\tcontent iso,vztmpl,backup\n\nlvmthin: local-lvm\n\tthinpool data\n\tvgname pve\n\ndir: iglo-backups\n\tpath /mnt/iglo/shares/backups/\n\tcontent backup\n\tis_mountpoint yes\n"
	got := dirStorages(config)
	if len(got) != 2 || got["/var/lib/vz"] != "local" || got["/mnt/iglo/shares/backups"] != "iglo-backups" {
		t.Fatalf("storages = %v", got)
	}
}

func TestProxmoxBackupsOnABackupsShare(t *testing.T) {
	s := testBindings(t)
	s.StorageConfig = filepath.Join(t.TempDir(), "storage.cfg")
	ctx := context.Background()
	for _, in := range []BindingInput{
		{ID: "backups", Role: "backups", Server: "nas.lan", Share: "Backups", Username: "pve", Password: "secret"},
		{ID: "readonly", Role: "backups", Server: "nas.lan", Share: "Old", Username: "pve", Password: "secret", ReadOnly: true},
		{ID: "photos", Role: "photos", Server: "nas.lan", Share: "Photos", Username: "immich", Password: "secret"},
	} {
		if err := s.Create(ctx, in); err != nil {
			t.Fatal(err)
		}
	}

	for _, id := range []string{"readonly", "photos"} {
		if err := s.UseForProxmoxBackups(ctx, id, true); err == nil {
			t.Errorf("%s: became backup storage", id)
		}
	}
	if err := s.UseForProxmoxBackups(ctx, "backups", true); err != nil {
		t.Fatal(err)
	}
	// Twice is fine.
	if err := s.UseForProxmoxBackups(ctx, "backups", true); err != nil {
		t.Fatal(err)
	}
	config, _ := os.ReadFile(s.StorageConfig)
	if strings.Count(string(config), "dir: iglo-backups") != 1 || !strings.Contains(string(config), "is_mountpoint yes") {
		t.Fatalf("storage.cfg:\n%s", config)
	}
	list, _ := s.List(ctx)
	for _, binding := range list {
		if (binding.ID == "backups") != (binding.ProxmoxStorage == "iglo-backups") {
			t.Fatalf("%s: proxmox storage %q", binding.ID, binding.ProxmoxStorage)
		}
	}

	// A share that Proxmox uses can't be disconnected.
	if err := s.Delete(ctx, "backups"); err == nil || !strings.Contains(err.Error(), "iglo-backups") {
		t.Fatalf("delete: %v", err)
	}
	if err := s.UseForProxmoxBackups(ctx, "backups", false); err != nil {
		t.Fatal(err)
	}
	if config, _ := os.ReadFile(s.StorageConfig); strings.Contains(string(config), "iglo-backups") {
		t.Fatalf("storage.cfg:\n%s", config)
	}
	if err := s.Delete(ctx, "backups"); err != nil {
		t.Fatal(err)
	}

	// A storage that someone made in Proxmox on the same path stays theirs.
	os.WriteFile(s.StorageConfig, []byte("dir: nas\n\tpath "+filepath.Join(s.Root, "readonly")+"\n\tcontent backup\n"), 0o600)
	if err := s.UseForProxmoxBackups(ctx, "readonly", false); err == nil {
		t.Fatal("removed a storage that Iglo-OS didn't make")
	}
}
