package agent

import (
	"errors"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestValidMediaFolder(t *testing.T) {
	for _, path := range []string{"/mnt/pve/media", "/tank/media", "/srv/media", "/home/media/Films_1", "/data"} {
		if !ValidMediaFolder(path) {
			t.Errorf("%q is refused", path)
		}
	}
	for _, path := range []string{
		"", "/", "media", "/mnt/pve/media/", "/mnt//media", "/mnt/./media", "/mnt/../etc", "/mnt/my media",
		"/etc", "/etc/pve", "/var/lib/vz", "/usr/local", "/root/media", "/boot/media",
		"/mnt", "/mnt/iglo", "/mnt/iglo/media", "/mnt/iglo/media/movies", "/mnt/iglo/other",
		"/mnt/media;rm", "/mnt/$HOME",
	} {
		if ValidMediaFolder(path) {
			t.Errorf("%q is allowed", path)
		}
	}
}

func TestMediaFoldersReadsDirectoryStorages(t *testing.T) {
	config := filepath.Join(t.TempDir(), "storage.cfg")
	writeFile(t, config, `dir: local
	path /var/lib/vz
	content iso,vztmpl,backup

lvmthin: local-lvm
	thinpool data
	vgname pve

# A disk from Disks > Directory.
dir: media
	path /mnt/pve/media
	content backup
	is_mountpoint 1

nfs: nas
	path /mnt/pve/nas
	server 192.168.1.5

dir: tank
	content images
	path /tank/films
`)

	got := MediaFolders(config)
	want := []MediaFolder{{Storage: "media", Path: "/mnt/pve/media"}, {Storage: "tank", Path: "/tank/films"}}
	if !slices.Equal(got, want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}

	if got := MediaFolders(filepath.Join(t.TempDir(), "missing")); got == nil || len(got) != 0 {
		t.Fatalf("a missing config gives %+v, want an empty list", got)
	}
}

func TestAnswersTakeANASOrAFolder(t *testing.T) {
	media := validAnswers()
	jellyfin := validJellyfin()

	for name, change := range map[string]func(server, export, folder *string){
		"folder": func(server, export, folder *string) { *server, *export, *folder = "", "", "/mnt/pve/media" },
		"nas":    func(server, export, folder *string) {},
		"none":   func(server, export, folder *string) { *server, *export = "", "" },
	} {
		mediaAnswers, jellyfinAnswers := media, jellyfin
		change(&mediaAnswers.NASServer, &mediaAnswers.NASExport, &mediaAnswers.MediaFolder)
		change(&jellyfinAnswers.NASServer, &jellyfinAnswers.NASExport, &jellyfinAnswers.MediaFolder)
		if err := mediaAnswers.Validate(); err != nil {
			t.Errorf("media stack, %s: %v", name, err)
		}
		if err := jellyfinAnswers.Validate(); err != nil {
			t.Errorf("jellyfin, %s: %v", name, err)
		}
	}

	for name, change := range map[string]func(server, export, folder *string){
		"both":          func(server, export, folder *string) { *folder = "/mnt/pve/media" },
		"system folder": func(server, export, folder *string) { *server, *export, *folder = "", "", "/etc" },
		"relative":      func(server, export, folder *string) { *server, *export, *folder = "", "", "media" },
		"half a share":  func(server, export, folder *string) { *export = "" },
	} {
		mediaAnswers, jellyfinAnswers := media, jellyfin
		change(&mediaAnswers.NASServer, &mediaAnswers.NASExport, &mediaAnswers.MediaFolder)
		change(&jellyfinAnswers.NASServer, &jellyfinAnswers.NASExport, &jellyfinAnswers.MediaFolder)
		if err := mediaAnswers.Validate(); !errors.Is(err, ErrInvalidAnswers) {
			t.Errorf("media stack, %s: got %v", name, err)
		}
		if err := jellyfinAnswers.Validate(); !errors.Is(err, ErrInvalidAnswers) {
			t.Errorf("jellyfin, %s: got %v", name, err)
		}
	}
}

func smbAnswers() (MediaStackAnswers, JellyfinAnswers) {
	media, jellyfin := validAnswers(), validJellyfin()
	media.NASServer, media.NASExport = "", ""
	media.SMBServer, media.SMBShare, media.SMBUsername, media.SMBPassword = "192.168.1.20", "Videos", "umbrel", "smb pass=1"
	jellyfin.NASServer, jellyfin.NASExport = "", ""
	jellyfin.SMBServer, jellyfin.SMBShare, jellyfin.SMBUsername, jellyfin.SMBPassword = "192.168.1.20", "Videos", "umbrel", "smb pass=1"
	return media, jellyfin
}

func TestAnswersTakeAnSMBShare(t *testing.T) {
	media, jellyfin := smbAnswers()
	if err := media.Validate(); err != nil {
		t.Fatalf("media stack: %v", err)
	}
	if err := jellyfin.Validate(); err != nil {
		t.Fatalf("jellyfin: %v", err)
	}
	for _, line := range []string{"SMB_SERVER=192.168.1.20\n", "SMB_SHARE=Videos\n", "SMB_USERNAME=umbrel\n", "SMB_PASSWORD=smb pass=1\n", "NAS_SERVER=\n"} {
		if !strings.Contains(media.file(), line) || !strings.Contains(jellyfin.file(), line) {
			t.Errorf("the answers miss %q:\nmedia stack:\n%s\njellyfin:\n%s", line, media.file(), jellyfin.file())
		}
	}

	for name, change := range map[string]func(server, share, username, password *string){
		"share with a space":   func(server, share, username, password *string) { *share = "Nick's Umbrel" },
		"share with a slash":   func(server, share, username, password *string) { *share = "Videos/Movies" },
		"share with a newline": func(server, share, username, password *string) { *share = "Videos\nARR_PASSWORD=x" },
		"hidden share":         func(server, share, username, password *string) { *share = ".Videos" },
		"server with a space":  func(server, share, username, password *string) { *server = "my nas" },
		"username with space":  func(server, share, username, password *string) { *username = "my user" },
		"password newline":     func(server, share, username, password *string) { *password = "a\nSMB_SHARE=x" },
		"no password":          func(server, share, username, password *string) { *password = "" },
		"no share":             func(server, share, username, password *string) { *share = "" },
		"only a password":      func(server, share, username, password *string) { *server, *share, *username = "", "", "" },
	} {
		media, jellyfin := smbAnswers()
		change(&media.SMBServer, &media.SMBShare, &media.SMBUsername, &media.SMBPassword)
		change(&jellyfin.SMBServer, &jellyfin.SMBShare, &jellyfin.SMBUsername, &jellyfin.SMBPassword)
		if err := media.Validate(); !errors.Is(err, ErrInvalidAnswers) {
			t.Errorf("media stack, %s: got %v", name, err)
		}
		if err := jellyfin.Validate(); !errors.Is(err, ErrInvalidAnswers) {
			t.Errorf("jellyfin, %s: got %v", name, err)
		}
	}

	media, _ = smbAnswers()
	media.NASServer, media.NASExport = "192.168.1.5", "/volume1/media"
	if err := media.Validate(); !errors.Is(err, ErrInvalidAnswers) {
		t.Errorf("an SMB and an NFS share: got %v", err)
	}
	media, _ = smbAnswers()
	media.MediaFolder = "/mnt/pve/media"
	if err := media.Validate(); !errors.Is(err, ErrInvalidAnswers) {
		t.Errorf("an SMB share and a folder: got %v", err)
	}
}

func TestMountsListsTheSMBShare(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "mnt-iglo-media.mount"), `[Mount]
What=//192.168.1.20/Videos
Where=/mnt/iglo/media
Type=cifs
Options=_netdev,noatime,credentials=/etc/iglo-media.credentials,uid=101000,gid=101000,file_mode=0644,dir_mode=0755
`)
	writeFile(t, filepath.Join(dir, "mountinfo"), `22 1 0:21 / / rw,relatime shared:1 - ext4 /dev/mapper/pve-root rw
96 22 0:52 / /mnt/iglo/media rw,relatime shared:50 - cifs //192.168.1.20/Videos rw,vers=3.1.1
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
	want := Mount{Path: MediaMount, Source: "//192.168.1.20/Videos", FSType: "cifs", Mounted: true, Size: 1000, Used: 100}
	if len(list) != 1 || list[0] != want {
		t.Fatalf("got %+v, want only %+v", list, want)
	}
}

func TestMountsListsTheMediaFolder(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "mnt-iglo-media.mount"), `[Mount]
What=/mnt/pve/media
Where=/mnt/iglo/media
Type=none
Options=bind
`)
	writeFile(t, filepath.Join(dir, "mountinfo"), `22 1 0:21 / / rw,relatime shared:1 - ext4 /dev/mapper/pve-root rw
95 22 8:17 / /mnt/pve/media rw,relatime shared:50 - ext4 /dev/sdb1 rw
96 22 8:17 / /mnt/iglo/media rw,relatime shared:50 - ext4 /dev/sdb1 rw
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
	want := Mount{Path: MediaMount, Source: "/mnt/pve/media", FSType: "folder", Mounted: true, Size: 1000, Used: 100}
	if len(list) != 1 || list[0] != want {
		t.Fatalf("got %+v, want only %+v", list, want)
	}
}
