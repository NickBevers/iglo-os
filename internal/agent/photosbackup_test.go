package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// hostDisks plays findmnt, lsblk and zpool on a host with the system on an
// NVMe disk, a USB disk at /mnt/usb, a second partition of the NVMe at
// /mnt/second, the NAS at /mnt/nas, a tmpfs at /mnt/ram, a second USB disk at
// /mnt/usb2 with the NAS mounted on its iglo-photos folder, a ZFS pool on two
// other disks at /tank, and a ZFS pool on the NVMe at /rpoolbak.
func hostDisks(_ context.Context, name string, args ...string) ([]byte, error) {
	last := args[len(args)-1]
	switch name {
	case "lsblk":
		disks := map[string]string{
			"/dev/mapper/pve-root": "/dev/mapper/pve-root lvm\n/dev/nvme0n1p3 part\n/dev/nvme0n1 disk\n",
			"/dev/sdb1":            "/dev/sdb1 part\n/dev/sdb disk\n",
			"/dev/sdc1":            "/dev/sdc1 part\n/dev/sdc disk\n",
			"/dev/sdd1":            "/dev/sdd1 part\n/dev/sdd disk\n",
			"/dev/sde1":            "/dev/sde1 part\n/dev/sde disk\n",
			"/dev/nvme0n1p4":       "/dev/nvme0n1p4 part\n/dev/nvme0n1 disk\n",
			"/dev/nvme0n1p5":       "/dev/nvme0n1p5 part\n/dev/nvme0n1 disk\n",
		}
		if out, ok := disks[last]; ok {
			return []byte(out), nil
		}
		return nil, errors.New("not a block device")
	case "zpool":
		// zpool list -v -H -P -L -o name: the pool, then its vdevs, tab separated.
		pools := map[string]string{
			"tank":     "tank\n\tmirror-0\n\t\t/dev/sdd1\n\t\t/dev/sde1\n",
			"rpoolbak": "rpoolbak\n\t/dev/nvme0n1p5\n",
		}
		if out, ok := pools[last]; ok {
			return []byte(out), nil
		}
		return nil, errors.New("cannot open pool")
	}
	// The longest mount that holds the path, like findmnt --target.
	best, line := "", "/ ext4 /dev/mapper/pve-root"
	for root, mount := range map[string]string{
		"/mnt/usb":              "/mnt/usb ext4 /dev/sdb1",
		"/mnt/btrfs":            "/mnt/btrfs btrfs /dev/sdb1[/backups]",
		"/mnt/second":           "/mnt/second ext4 /dev/nvme0n1p4",
		"/mnt/nas":              "/mnt/nas cifs //nas/photos",
		"/mnt/ram":              "/mnt/ram tmpfs tmpfs",
		"/mnt/usb2":             "/mnt/usb2 ext4 /dev/sdc1",
		"/mnt/usb2/iglo-photos": "/mnt/usb2/iglo-photos cifs //nas/backups",
		"/tank":                 "/tank zfs tank/backups",
		"/rpoolbak":             "/rpoolbak zfs rpoolbak",
		"/broken":               "/broken zfs gone/backups",
	} {
		if (last == root || strings.HasPrefix(last, root+"/")) && len(root) > len(best) {
			best, line = root, mount
		}
	}
	return []byte(line + "\n"), nil
}

func testBackups(t *testing.T) *PhotosBackups {
	backups := NewPhotosBackupsWith(t.TempDir(), hostDisks, nil)
	// A link in the host folder resolves somewhere else, like a link to the NAS.
	backups.resolve = func(path string) (string, error) {
		if path == "/mnt/usb/link" {
			return "/mnt/usb/real", nil
		}
		if strings.HasPrefix(path, "/mnt/gone") {
			return "", os.ErrNotExist
		}
		if info, err := os.Lstat(filepath.Join(backups.hostRoot, path)); err == nil && info.Mode()&os.ModeSymlink != 0 {
			return "/mnt/iglo/shares/backups/immich-restic", nil
		}
		return path, nil
	}
	return backups
}

// hostFolder makes folder path in the host folder of the tests.
func hostFolder(t *testing.T, backups *PhotosBackups, path string) string {
	folder := filepath.Join(backups.hostRoot, path)
	if err := os.MkdirAll(folder, 0o700); err != nil {
		t.Fatal(err)
	}
	return folder
}

func TestPhotosBackupSettingsValidate(t *testing.T) {
	good := DefaultPhotosBackupSettings()
	if err := good.Validate(); err != nil {
		t.Fatalf("defaults: %v", err)
	}
	for name, change := range map[string]func(*PhotosBackupSettings){
		"time":          func(s *PhotosBackupSettings) { s.Time = "25:00" },
		"time format":   func(s *PhotosBackupSettings) { s.Time = "1:00" },
		"negative":      func(s *PhotosBackupSettings) { s.KeepDaily = -1 },
		"too many":      func(s *PhotosBackupSettings) { s.KeepMonthly = 501 },
		"none kept":     func(s *PhotosBackupSettings) { s.KeepDaily, s.KeepWeekly, s.KeepMonthly = 0, 0, 0 },
		"relative path": func(s *PhotosBackupSettings) { s.OffsitePath = "mnt/usb" },
		"dot dot":       func(s *PhotosBackupSettings) { s.OffsitePath = "/mnt/usb/../etc" },
		"line break":    func(s *PhotosBackupSettings) { s.OffsitePath = "/mnt/usb\nKEEP_DAILY=0" },
		"trailing":      func(s *PhotosBackupSettings) { s.OffsitePath = "/mnt/usb/" },
		"root":          func(s *PhotosBackupSettings) { s.OffsitePath = "/" },
		"share":         func(s *PhotosBackupSettings) { s.OffsitePath = "/mnt/iglo/shares/backups" },
	} {
		settings := good
		change(&settings)
		if err := settings.Validate(); !errors.Is(err, ErrInvalidAnswers) {
			t.Errorf("%s: got %v, want invalid answers", name, err)
		}
	}
}

func TestOffsiteProblem(t *testing.T) {
	backups := testBackups(t)
	hostFolder(t, backups, "/mnt/usb2/iglo-photos")
	hostFolder(t, backups, "/mnt/usb/linked")
	if err := os.Symlink("/mnt/iglo/shares/backups/immich-restic", filepath.Join(backups.hostRoot, "/mnt/usb/linked/iglo-photos")); err != nil {
		t.Fatal(err)
	}
	for path, want := range map[string]string{
		"/mnt/usb":              "",
		"/mnt/usb/photos":       "",
		"/mnt/btrfs/photos":     "",
		"/var/backups":          "root disk",
		"/mnt/second/photos":    "same disk as the system",
		"/mnt/nas/backups":      "network share",
		"/mnt/ram":              "file system tmpfs",
		"/mnt/usb/link":         "does not exist",
		"/mnt/gone":             "does not exist",
		"/mnt/iglo/shares/nas1": "like /mnt/backup-disk",
		"/tank/photos":          "",
		"/rpoolbak/photos":      "same disk as the system",
		"/broken/photos":        "could not find the disk of gone/backups",
		"/mnt/usb2":             "iglo-photos folder is on another file system",
		"/mnt/usb/linked":       "iglo-photos folder is a link",
	} {
		got := backups.OffsiteProblem(context.Background(), path)
		if want == "" && got != "" || want != "" && !strings.Contains(got, want) {
			t.Errorf("%s: got %q, want %q", path, got, want)
		}
	}
}

// The root disk of a ZFS system must be known, or no disk counts as another one.
func TestOffsiteProblemWithUnknownRootDisk(t *testing.T) {
	backups := testBackups(t)
	backups.output = func(ctx context.Context, name string, args ...string) ([]byte, error) {
		if name == "findmnt" && args[len(args)-1] == "/" {
			return []byte("/ zfs rpool/ROOT/pve-1\n"), nil
		}
		return hostDisks(ctx, name, args...)
	}
	if got := backups.OffsiteProblem(context.Background(), "/mnt/usb"); !strings.Contains(got, "disk of the system") {
		t.Fatalf("got %q", got)
	}
}

func TestOffsiteProblemWithoutMounts(t *testing.T) {
	backups := testBackups(t)
	backups.output = func(context.Context, string, ...string) ([]byte, error) { return nil, errors.New("findmnt failed") }
	if got := backups.OffsiteProblem(context.Background(), "/mnt/usb"); !strings.Contains(got, "not mounted") {
		t.Fatalf("got %q", got)
	}
}

func TestSavePhotosBackupSettings(t *testing.T) {
	backups := testBackups(t)
	folder := hostFolder(t, backups, "/mnt/usb/photos")
	settings := PhotosBackupSettings{Time: "00:30", OffsitePath: "/mnt/usb/photos", KeepDaily: 5, KeepWeekly: 0, KeepMonthly: 24, OffsiteID: "from the browser"}
	if err := backups.Save(context.Background(), settings); err != nil {
		t.Fatal(err)
	}
	marker, err := os.ReadFile(filepath.Join(folder, OffsiteMarker))
	if err != nil {
		t.Fatal(err)
	}
	id := strings.TrimSpace(string(marker))
	if !offsiteIDPattern.MatchString(id) {
		t.Fatalf("marker %q", marker)
	}
	data, _ := os.ReadFile(backups.ConfPath)
	if string(data) != "BACKUP_TIME=00:30\nOFFSITE_PATH=/mnt/usb/photos\nOFFSITE_ID="+id+"\nKEEP_DAILY=5\nKEEP_WEEKLY=0\nKEEP_MONTHLY=24\n" {
		t.Fatalf("conf: %q", data)
	}
	if info, _ := os.Stat(backups.ConfPath); info.Mode().Perm() != 0o600 {
		t.Fatalf("mode %v", info.Mode())
	}
	got, err := backups.Settings()
	settings.OffsiteID = id
	if err != nil || got != settings || !backups.MarkedDisk(got) {
		t.Fatalf("got %+v, %v", got, err)
	}
	// Saving again keeps the disk.
	if err := backups.Save(context.Background(), got); err != nil {
		t.Fatal(err)
	}
	if again, _ := backups.Settings(); again.OffsiteID != id {
		t.Fatalf("new ID %q for the same disk", again.OffsiteID)
	}
	// The browser never sees the ID.
	if encoded, _ := json.Marshal(got); strings.Contains(string(encoded), id) {
		t.Fatalf("the ID is in %s", encoded)
	}
	// Another disk in that place is not the saved one.
	os.WriteFile(filepath.Join(folder, OffsiteMarker), []byte("0123456789abcdef0123456789abcdef\n"), 0o600)
	if backups.MarkedDisk(got) {
		t.Fatal("another disk counts as the saved one")
	}

	// The root disk is refused, and the saved settings stay.
	settings.OffsitePath = "/srv/backups"
	if err := backups.Save(context.Background(), settings); !errors.Is(err, ErrInvalidAnswers) || !strings.Contains(err.Error(), "root disk") {
		t.Fatalf("root disk: %v", err)
	}
	if got, _ := backups.Settings(); got.OffsitePath != "/mnt/usb/photos" {
		t.Fatalf("settings changed: %+v", got)
	}
	// Without a folder off the NAS, there is no ID.
	settings.OffsitePath = ""
	if err := backups.Save(context.Background(), settings); err != nil {
		t.Fatal(err)
	}
	if got, _ := backups.Settings(); got.OffsiteID != "" {
		t.Fatalf("ID without a folder: %+v", got)
	}
}

func TestPhotosBackupDefaultsWithoutFiles(t *testing.T) {
	view, err := testBackups(t).View()
	if err != nil {
		t.Fatal(err)
	}
	if view.Settings != DefaultPhotosBackupSettings() || view.HasPassword || !view.Status.Finished.IsZero() {
		t.Fatalf("got %+v", view)
	}
}

func TestPhotosBackupStatus(t *testing.T) {
	backups := testBackups(t)
	state := strings.Join([]string{
		"BACKUP_STARTED=2026-10-29T00:00:03Z",
		"BACKUP_FINISHED=2026-10-29T00:40:00Z",
		"BACKUP_RESULT=ok",
		"SNAPSHOT=1a2b3c4d",
		"NAS_BYTES=412316860416",
		"NAS_SNAPSHOTS=23",
		"OFFSITE_RESULT=missing",
		"OFFSITE_MESSAGE=/mnt/usb cannot be used: its disk is not mounted",
		"OFFSITE_FINISHED=2026-10-27T00:50:00Z",
		"OFFSITE_BYTES=-5",
		"VERIFY_RESULT=maybe",
		"VERIFY_MESSAGE=<script>alert(1)</script>",
		"VERIFY_SNAPSHOT=../../etc",
		"UNKNOWN=1",
	}, "\n") + "\n"
	if err := os.WriteFile(backups.StatePath, []byte(state), 0o600); err != nil {
		t.Fatal(err)
	}
	os.MkdirAll(filepath.Join(backups.CredentialsDir, "host"), 0o700)
	os.WriteFile(filepath.Join(backups.CredentialsDir, "host", "photos_restic_password.cred"), []byte("sealed"), 0o600)

	view, err := backups.View()
	if err != nil {
		t.Fatal(err)
	}
	status := view.Status
	if !view.HasPassword {
		t.Fatal("the sealed password is not seen")
	}
	if status.Result != "ok" || status.Snapshot != "1a2b3c4d" || status.NAS.Bytes != 412316860416 || status.NAS.Snapshots != 23 {
		t.Fatalf("backup: %+v", status)
	}
	if !status.Finished.Equal(time.Date(2026, 10, 29, 0, 40, 0, 0, time.UTC)) {
		t.Fatalf("finished: %v", status.Finished)
	}
	if status.Offsite.Result != "missing" || !strings.Contains(status.Offsite.Message, "not mounted") || status.Offsite.Bytes != 0 || status.Offsite.Finished.IsZero() {
		t.Fatalf("offsite: %+v", status.Offsite)
	}
	// Values that backup.sh never writes don't reach the page.
	if status.VerifyResult != "" || status.VerifyMessage != "" || status.VerifySnapshot != "" {
		t.Fatalf("verify: %+v", status)
	}
}

func TestPhotosBackupRoutes(t *testing.T) {
	backups := testBackups(t)
	mux := http.NewServeMux()
	RegisterPhotosBackups(mux, backups, slog.New(slog.DiscardHandler))
	server := httptest.NewServer(mux)
	defer server.Close()
	client := &Client{http: server.Client()}
	client.http.Transport = rewriteTo(server.URL)

	view, err := client.PhotosBackups(context.Background())
	if err != nil || view.Settings.Time != "01:00" {
		t.Fatalf("view: %+v, %v", view, err)
	}
	settings := DefaultPhotosBackupSettings()
	settings.OffsitePath = "/mnt/nas/backups"
	err = client.SavePhotosBackups(context.Background(), settings)
	if !errors.Is(err, ErrInvalidAnswers) || !strings.Contains(err.Error(), "network share") || strings.Count(err.Error(), "invalid answers") != 1 {
		t.Fatalf("NAS folder: %v", err)
	}
	settings.OffsitePath = "/mnt/usb"
	hostFolder(t, backups, "/mnt/usb")
	if err := client.SavePhotosBackups(context.Background(), settings); err != nil {
		t.Fatal(err)
	}
	if view, _ := client.PhotosBackups(context.Background()); view.Settings.OffsitePath != "/mnt/usb" {
		t.Fatalf("not saved: %+v", view.Settings)
	}
}

// rewriteTo sends the requests of a Client, which go to http://agent, to url.
type rewriteTo string

func (r rewriteTo) RoundTrip(request *http.Request) (*http.Response, error) {
	clone := request.Clone(request.Context())
	target := fmt.Sprintf("%s%s", string(r), request.URL.Path)
	parsed, err := http.NewRequestWithContext(request.Context(), request.Method, target, request.Body)
	if err != nil {
		return nil, err
	}
	clone.URL, clone.Host = parsed.URL, parsed.Host
	return http.DefaultTransport.RoundTrip(clone)
}

// A copy off the NAS belongs to its disk: after another disk is saved, the
// page shows it as not copied yet, without the time and the size of the old one.
func TestPhotosBackupOffsiteOfAnotherDisk(t *testing.T) {
	backups := testBackups(t)
	first, second := "0123456789abcdef0123456789abcdef", "fedcba9876543210fedcba9876543210"
	conf := func(id string) {
		data := "BACKUP_TIME=01:00\nOFFSITE_PATH=/mnt/usb\nOFFSITE_ID=" + id + "\nKEEP_DAILY=7\nKEEP_WEEKLY=4\nKEEP_MONTHLY=12\n"
		if err := os.WriteFile(backups.ConfPath, []byte(data), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	state := func(disk string) {
		lines := "BACKUP_RESULT=ok\nOFFSITE_RESULT=ok\nOFFSITE_FINISHED=2026-10-29T00:40:00Z\nOFFSITE_BYTES=1000\nOFFSITE_SNAPSHOTS=3\n"
		if disk != "" {
			lines += "OFFSITE_DISK=" + disk + "\n"
		}
		if err := os.WriteFile(backups.StatePath, []byte(lines), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	offsite := func() PhotosRepository {
		view, err := backups.View()
		if err != nil {
			t.Fatal(err)
		}
		return view.Status.Offsite
	}

	conf(first)
	state(first)
	if got := offsite(); got.Result != "ok" || got.Bytes != 1000 || got.Finished.IsZero() {
		t.Fatalf("same disk: %+v", got)
	}
	conf(second)
	if got := offsite(); got.Result != "" || got.Bytes != 0 || got.Snapshots != 0 || !got.Finished.IsZero() {
		t.Fatalf("another disk shows the old copy: %+v", got)
	}
	// A result from before the disk was recorded belongs to no disk.
	conf(first)
	state("")
	if got := offsite(); got.Result != "" || !got.Finished.IsZero() {
		t.Fatalf("a result without its disk: %+v", got)
	}
}
