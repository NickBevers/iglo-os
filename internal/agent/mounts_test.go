package agent

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func newTestMounts(t *testing.T, mountInfo string) *Mounts {
	t.Helper()
	dir := t.TempDir()

	writeFile(t, filepath.Join(dir, "mnt-iglo-media.mount"), `[Unit]
Description=Iglo-OS media share on the NAS

[Mount]
What=192.168.1.10:/volume1/media
Where=/mnt/iglo/media
Type=nfs
Options=_netdev,hard,noatime
`)
	writeFile(t, filepath.Join(dir, "boot.mount"), "[Mount]\nWhat=/dev/sda1\nWhere=/boot\nType=ext4\n")
	writeFile(t, filepath.Join(dir, "fstab"), `# <file system> <mount point> <type> <options>
/dev/pve/root / ext4 errors=remount-ro 0 1
//nas/backup\040files /mnt/backup cifs credentials=/root/.smb 0 0
nas:/volume1/old /mnt/old nfs noauto 0 0
`)
	writeFile(t, filepath.Join(dir, "mountinfo"), mountInfo)

	return &Mounts{
		UnitDir:   dir,
		Fstab:     filepath.Join(dir, "fstab"),
		MountInfo: filepath.Join(dir, "mountinfo"),
		Timeout:   100 * time.Millisecond,
		statfs: func(string) (int64, int64, error) {
			return 1000, 950, nil
		},
		pending: map[string]bool{},
	}
}

func TestMountsListsExpectedAndMountedShares(t *testing.T) {
	mounts := newTestMounts(t, `22 1 0:21 / / rw,relatime shared:1 - ext4 /dev/mapper/pve-root rw
90 22 0:50 / /mnt/iglo/media rw,noatime shared:40 - nfs 192.168.1.10:/volume1/media rw,vers=4.2
91 22 0:51 / /mnt/pve/nas-backups rw shared:41 - nfs 192.168.1.10:/volume1/pve rw
92 22 0:52 / /mnt/extra rw shared:42 - nfs4 nas:/volume1/extra rw
`)

	list := mounts.List()
	if len(list) != 3 {
		t.Fatalf("got %d mounts, want 3: %+v", len(list), list)
	}

	backup, extra, media := list[0], list[1], list[2]
	if backup.Path != "/mnt/backup" || backup.Source != "//nas/backup files" || backup.Mounted {
		t.Errorf("backup = %+v, want an unmounted cifs share", backup)
	}
	if extra.Path != "/mnt/extra" || !extra.Mounted || extra.FSType != "nfs4" {
		t.Errorf("extra = %+v", extra)
	}
	if media.Source != "192.168.1.10:/volume1/media" || !media.Mounted || media.Size != 1000 || media.Used != 950 {
		t.Errorf("media = %+v", media)
	}
}

func TestMountsReportsHungShare(t *testing.T) {
	mounts := newTestMounts(t, "90 22 0:50 / /mnt/iglo/media rw - nfs 192.168.1.10:/volume1/media rw\n")
	release := make(chan struct{})
	mounts.statfs = func(string) (int64, int64, error) {
		<-release
		return 0, 0, nil
	}
	defer close(release)

	media := mounts.List()[1]
	if media.Error != "not responding" {
		t.Fatalf("media = %+v, want not responding", media)
	}

	// The first statfs is still stuck, so the second call must not wait again.
	started := time.Now()
	if media := mounts.List()[1]; media.Error != "not responding" || time.Since(started) > 50*time.Millisecond {
		t.Fatalf("second call: %+v after %s", media, time.Since(started))
	}
}

func TestMountsListsAScopedShare(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "mnt-iglo-shares-videos.mount"), `[Mount]
What=//192.168.1.20/Videos
Where=/mnt/iglo/shares/videos
Type=cifs
Options=credentials=/etc/iglo/storage/videos.cred,vers=3.0,uid=100000,gid=101000,file_mode=0664,dir_mode=0775
`)
	writeFile(t, filepath.Join(dir, "mountinfo"), `22 1 0:21 / / rw,relatime shared:1 - ext4 /dev/mapper/pve-root rw
96 22 0:52 / /mnt/iglo/shares/videos rw,relatime shared:50 - cifs //192.168.1.20/Videos rw,vers=3.0
`)
	mounts := &Mounts{
		UnitDir:   dir,
		Fstab:     filepath.Join(dir, "fstab"),
		MountInfo: filepath.Join(dir, "mountinfo"),
		Timeout:   100 * time.Millisecond,
		statfs:    func(string) (int64, int64, error) { return 1000, 100, nil },
		pending:   map[string]bool{},
	}

	list := mounts.List()
	want := Mount{Path: "/mnt/iglo/shares/videos", Source: "//192.168.1.20/Videos", FSType: "cifs", Mounted: true, Size: 1000, Used: 100}
	if len(list) != 1 || list[0] != want {
		t.Fatalf("got %+v, want only %+v", list, want)
	}
}
