package agent

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
)

// The Photos backups run on the host with restic (deploy/stacks/photos/backup.sh).
// The agent keeps their settings in a file that the script reads and checks
// again, and shows the state that the script writes after each run.
const (
	PhotosBackupConf  = "/etc/iglo/photos-backup.conf"
	PhotosBackupState = "/var/lib/iglo/photos-backup/state"
	// photosPassword is the host secret of the repositories (deploy/secrets.sh).
	photosPassword = "photos_restic_password"
	// OffsiteMarker is the file with the ID of the backup disk, in the folder
	// off the NAS. backup.sh only writes there while it finds that ID, so an
	// empty mount point of an unplugged disk never gets a repository.
	OffsiteMarker = ".iglo-photos-disk"
	// offsiteRepository is the restic repository in the folder off the NAS.
	offsiteRepository = "iglo-photos"
)

var (
	backupTimePattern  = regexp.MustCompile(`^([01][0-9]|2[0-3]):[0-5][0-9]$`)
	offsitePathPattern = regexp.MustCompile(`^/[A-Za-z0-9._/-]{1,200}$`)
	offsiteIDPattern   = regexp.MustCompile(`^[a-f0-9]{32}$`)
)

// PhotosBackupSettings are the settings of the Photos backups. Time is when
// the manager starts the nightly backup, in its own time zone. OffsitePath is
// a folder on a disk of the host for the copy off the NAS, or empty.
type PhotosBackupSettings struct {
	Time        string `json:"time"`
	OffsitePath string `json:"offsitePath"`
	KeepDaily   int    `json:"keepDaily"`
	KeepWeekly  int    `json:"keepWeekly"`
	KeepMonthly int    `json:"keepMonthly"`
	// OffsiteID is the ID in the marker of the backup disk. The agent sets it
	// when it saves the folder, so it never comes from the browser.
	OffsiteID string `json:"-"`
}

// DefaultPhotosBackupSettings back up at 01:00, before the processing window
// of background work (02:00) and the Proxmox backups (03:00), and keep 7
// daily, 4 weekly and 12 monthly snapshots.
func DefaultPhotosBackupSettings() PhotosBackupSettings {
	return PhotosBackupSettings{Time: "01:00", KeepDaily: 7, KeepWeekly: 4, KeepMonthly: 12}
}

// Validate checks the form of the settings. Whether the folder is on a disk of
// its own, only the host can say (PhotosBackups.Save).
func (s PhotosBackupSettings) Validate() error {
	invalid := func(message string) error { return fmt.Errorf("%w: %s", ErrInvalidAnswers, message) }
	switch {
	case !backupTimePattern.MatchString(s.Time):
		return invalid("give the time of the nightly backup as HH:MM, like 01:00")
	case s.KeepDaily < 0 || s.KeepWeekly < 0 || s.KeepMonthly < 0 || s.KeepDaily > 500 || s.KeepWeekly > 500 || s.KeepMonthly > 500:
		return invalid("keep 0 to 500 backups of each kind")
	case s.KeepDaily+s.KeepWeekly+s.KeepMonthly == 0:
		return invalid("keep at least one backup")
	case s.OffsitePath != "" && !validOffsitePath(s.OffsitePath):
		return invalid("give the folder off the NAS as an absolute path on a disk of the host, like /mnt/backup-disk")
	}
	return nil
}

// validOffsitePath is the check of offsite_path_valid in backup.sh.
func validOffsitePath(path string) bool {
	if !offsitePathPattern.MatchString(path) || strings.HasSuffix(path, "/") || strings.Contains(path, "//") {
		return false
	}
	for part := range strings.SplitSeq(path[1:], "/") {
		if part == "." || part == ".." {
			return false
		}
	}
	return path != "/mnt/iglo" && !strings.HasPrefix(path, "/mnt/iglo/")
}

func (s PhotosBackupSettings) file() string {
	return fmt.Sprintf("BACKUP_TIME=%s\nOFFSITE_PATH=%s\nOFFSITE_ID=%s\nKEEP_DAILY=%d\nKEEP_WEEKLY=%d\nKEEP_MONTHLY=%d\n",
		s.Time, s.OffsitePath, s.OffsiteID, s.KeepDaily, s.KeepWeekly, s.KeepMonthly)
}

// PhotosRepository is one restic repository: on the NAS, or off it.
type PhotosRepository struct {
	Bytes     int64 `json:"bytes,omitempty"`
	Snapshots int   `json:"snapshots,omitempty"`
	// Result of the last copy off the NAS: ok, failed, missing (the disk
	// can't be used) or off (no folder set). Empty for the NAS.
	Result  string `json:"result,omitempty"`
	Message string `json:"message,omitempty"`
	// Finished is the last copy off the NAS that worked.
	Finished time.Time `json:"finished,omitzero"`
	// disk is the ID of the backup disk that the result belongs to.
	disk string
}

// PhotosBackupStatus is what the last runs did, from the state of backup.sh.
type PhotosBackupStatus struct {
	Started  time.Time `json:"started,omitzero"`
	Finished time.Time `json:"finished,omitzero"`
	// Result is ok or failed, or empty before the first backup.
	Result   string           `json:"result,omitempty"`
	Message  string           `json:"message,omitempty"`
	Snapshot string           `json:"snapshot,omitempty"`
	NAS      PhotosRepository `json:"nas"`
	Offsite  PhotosRepository `json:"offsite"`
	// The last Verify recovery.
	Verified       time.Time `json:"verified,omitzero"`
	VerifyResult   string    `json:"verifyResult,omitempty"`
	VerifyMessage  string    `json:"verifyMessage,omitempty"`
	VerifySnapshot string    `json:"verifySnapshot,omitempty"`
}

// PhotosBackupView is the settings and the state of the Photos backups.
// HasPassword says that the host has the password of the repositories. The
// password itself never leaves the host.
type PhotosBackupView struct {
	Settings    PhotosBackupSettings `json:"settings"`
	Status      PhotosBackupStatus   `json:"status"`
	HasPassword bool                 `json:"hasPassword"`
}

// PhotosBackups keeps the settings of the Photos backups and reads their state.
type PhotosBackups struct {
	ConfPath  string
	StatePath string
	// CredentialsDir has the sealed secrets (deploy/secrets.sh).
	CredentialsDir string
	// output runs findmnt, lsblk and zpool, and resolve follows a path to the
	// real folder. Tests and the dev mock replace them.
	output  func(ctx context.Context, name string, args ...string) ([]byte, error)
	resolve func(path string) (string, error)
	// hostRoot is where the folders of the host are: "" on the host, a test
	// folder in tests and the dev mock. Only the marker and the repository
	// folder are looked up in it.
	hostRoot string
}

func NewPhotosBackups() *PhotosBackups {
	return &PhotosBackups{ConfPath: PhotosBackupConf, StatePath: PhotosBackupState, CredentialsDir: "/etc/iglo/credentials", output: commandOutput, resolve: resolveFolder}
}

// NewPhotosBackupsWith keeps its files in dir, and asks output and resolve
// about disks. The dev mock uses it to play a host with a backup disk.
func NewPhotosBackupsWith(dir string, output func(context.Context, string, ...string) ([]byte, error), resolve func(string) (string, error)) *PhotosBackups {
	return &PhotosBackups{ConfPath: filepath.Join(dir, "photos-backup.conf"), StatePath: filepath.Join(dir, "photos-backup.state"),
		CredentialsDir: filepath.Join(dir, "credentials"), output: output, resolve: resolve, hostRoot: filepath.Join(dir, "host")}
}

func resolveFolder(path string) (string, error) {
	real, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(real)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", errors.New("not a folder")
	}
	return real, nil
}

// Settings reads the saved settings. Missing ones are the defaults.
func (p *PhotosBackups) Settings() (PhotosBackupSettings, error) {
	settings := DefaultPhotosBackupSettings()
	data, err := os.ReadFile(p.ConfPath)
	if errors.Is(err, os.ErrNotExist) {
		return settings, nil
	}
	if err != nil {
		return settings, err
	}
	for line := range strings.Lines(string(data)) {
		key, value, _ := strings.Cut(strings.TrimRight(line, "\n"), "=")
		number, _ := strconv.Atoi(value)
		switch key {
		case "BACKUP_TIME":
			settings.Time = value
		case "OFFSITE_PATH":
			settings.OffsitePath = value
		case "OFFSITE_ID":
			settings.OffsiteID = value
		case "KEEP_DAILY":
			settings.KeepDaily = number
		case "KEEP_WEEKLY":
			settings.KeepWeekly = number
		case "KEEP_MONTHLY":
			settings.KeepMonthly = number
		}
	}
	return settings, nil
}

// View returns the settings and the state of the last runs.
func (p *PhotosBackups) View() (PhotosBackupView, error) {
	settings, err := p.Settings()
	if err != nil {
		return PhotosBackupView{}, err
	}
	status, err := p.status()
	if err != nil {
		return PhotosBackupView{}, err
	}
	// The result off the NAS of another disk, or of no disk, says nothing
	// about the disk saved now: it is not copied yet.
	if settings.OffsitePath != "" && status.Offsite.disk != settings.OffsiteID {
		status.Offsite = PhotosRepository{}
	}
	_, err = os.Stat(filepath.Join(p.CredentialsDir, "host", photosPassword+".cred"))
	return PhotosBackupView{Settings: settings, Status: status, HasPassword: err == nil}, nil
}

// Save checks the settings and saves them for the next run. A folder off the
// NAS must be on a mounted local disk that is not the root disk.
func (p *PhotosBackups) Save(ctx context.Context, settings PhotosBackupSettings) error {
	if err := settings.Validate(); err != nil {
		return err
	}
	settings.OffsiteID = ""
	if settings.OffsitePath != "" {
		if problem := p.OffsiteProblem(ctx, settings.OffsitePath); problem != "" {
			return fmt.Errorf("%w: %s can't hold the copy off the NAS: %s", ErrInvalidAnswers, settings.OffsitePath, problem)
		}
		id, err := p.markDisk(settings.OffsitePath)
		if err != nil {
			return fmt.Errorf("could not mark the backup disk: %w", err)
		}
		settings.OffsiteID = id
	}
	if err := os.MkdirAll(filepath.Dir(p.ConfPath), 0o755); err != nil {
		return err
	}
	temp := p.ConfPath + ".new"
	if err := os.WriteFile(temp, []byte(settings.file()), 0o600); err != nil {
		return err
	}
	return os.Rename(temp, p.ConfPath)
}

// markDisk writes the marker of the backup disk in folder path, and returns
// its ID. A marker that is there keeps its ID, so saving the settings again
// keeps the disk.
func (p *PhotosBackups) markDisk(path string) (string, error) {
	marker := filepath.Join(p.hostRoot, path, OffsiteMarker)
	if info, err := os.Lstat(marker); err == nil && info.Mode().IsRegular() {
		if data, err := os.ReadFile(marker); err == nil && offsiteIDPattern.MatchString(strings.TrimSpace(string(data))) {
			return strings.TrimSpace(string(data)), nil
		}
	}
	random := make([]byte, 16)
	if _, err := rand.Read(random); err != nil {
		return "", err
	}
	id := hex.EncodeToString(random)
	temp := marker + ".new"
	if err := os.WriteFile(temp, []byte(id+"\n"), 0o600); err != nil {
		return "", err
	}
	return id, os.Rename(temp, marker)
}

// MarkedDisk says whether the folder off the NAS of settings has the marker of
// the saved disk. backup.sh checks the same before each restic run there.
func (p *PhotosBackups) MarkedDisk(settings PhotosBackupSettings) bool {
	marker := filepath.Join(p.hostRoot, settings.OffsitePath, OffsiteMarker)
	info, err := os.Lstat(marker)
	if err != nil || !info.Mode().IsRegular() || settings.OffsiteID == "" {
		return false
	}
	data, err := os.ReadFile(marker)
	return err == nil && strings.TrimSpace(string(data)) == settings.OffsiteID
}

// networkFilesystems are shares on another machine, like the NAS.
var networkFilesystems = []string{"cifs", "smb3", "smbfs", "nfs", "nfs4", "fuse.sshfs", "9p"}

// localFilesystems can hold a restic repository on a disk of the host.
var localFilesystems = []string{"ext4", "ext3", "xfs", "btrfs", "zfs", "exfat", "vfat", "ntfs3", "f2fs"}

// OffsiteProblem says why path can't hold the copy off the NAS, or "". It is
// offsite_problem of backup.sh, which checks again before every run.
func (p *PhotosBackups) OffsiteProblem(ctx context.Context, path string) string {
	if !validOffsitePath(path) {
		return "use a folder like /mnt/backup-disk"
	}
	if real, err := p.resolve(path); err != nil || real != path {
		return "the folder does not exist"
	}
	target, fstype, source, err := p.mountOf(ctx, path)
	rootTarget, rootFstype, rootSource, rootErr := p.mountOf(ctx, "/")
	switch {
	case err != nil || rootErr != nil || rootTarget != "/":
		return "its disk is not mounted"
	case slices.Contains(networkFilesystems, fstype):
		return "it is on a network share, like the NAS"
	case !slices.Contains(localFilesystems, fstype):
		return "its disk is not mounted, or it has the file system " + fstype
	case target == "/":
		return "it is on the root disk, not a disk of its own"
	}
	disks := p.disksOf(ctx, source, fstype)
	if len(disks) == 0 {
		return "could not find the disk of " + source
	}
	rootDisks := p.disksOf(ctx, rootSource, rootFstype)
	if len(rootDisks) == 0 {
		return "could not find the disk of the system"
	}
	for _, disk := range rootDisks {
		if slices.Contains(disks, disk) {
			return "it is on the same disk as the system"
		}
	}
	// The repository folder, when there is one, must be a real folder on the
	// same file system: not a link to the NAS, and not a mount of its own.
	repository := path + "/" + offsiteRepository
	if _, err := os.Lstat(filepath.Join(p.hostRoot, repository)); err == nil {
		if real, err := p.resolve(repository); err != nil || real != repository {
			return "its iglo-photos folder is a link or not a folder"
		}
		repoTarget, repoFstype, repoSource, err := p.mountOf(ctx, repository)
		if err != nil || repoTarget != target || repoFstype != fstype || repoSource != source {
			return "its iglo-photos folder is on another file system"
		}
	}
	return ""
}

// mountOf returns the mounted file system that holds path.
func (p *PhotosBackups) mountOf(ctx context.Context, path string) (target, fstype, source string, err error) {
	output, err := p.output(ctx, "findmnt", "--raw", "--noheadings", "--output", "TARGET,FSTYPE,SOURCE", "--target", path)
	if err != nil {
		return "", "", "", err
	}
	line, _, _ := strings.Cut(string(output), "\n")
	fields := strings.Fields(line)
	if len(fields) != 3 {
		return "", "", "", errors.New("unexpected findmnt output")
	}
	return unescapeRaw(fields[0]), fields[1], unescapeRaw(fields[2]), nil
}

// unescapeRaw undoes the \x20 escapes of findmnt --raw.
func unescapeRaw(value string) string {
	if !strings.Contains(value, `\x`) {
		return value
	}
	var out strings.Builder
	for i := 0; i < len(value); i++ {
		if value[i] == '\\' && i+3 < len(value) && value[i+1] == 'x' {
			if b, err := strconv.ParseUint(value[i+2:i+4], 16, 8); err == nil {
				out.WriteByte(byte(b))
				i += 3
				continue
			}
		}
		out.WriteByte(value[i])
	}
	return out.String()
}

// disksOf returns the physical disks under a file system. For ZFS, those are
// the disks of the vdevs of its pool: zpool list -v -H -P -L prints the real
// path of each vdev (zpool-list.8 of OpenZFS 2.4.4, the zfs-linux 2.4.4-pve1 of
// Proxmox VE 9, checked 29 October 2026).
func (p *PhotosBackups) disksOf(ctx context.Context, source, fstype string) []string {
	// A Btrfs subvolume shows as /dev/sdb1[/backups].
	source, _, _ = strings.Cut(source, "[")
	devices := []string{source}
	if fstype == "zfs" {
		pool, _, _ := strings.Cut(source, "/")
		output, err := p.output(ctx, "zpool", "list", "-v", "-H", "-P", "-L", "-o", "name", pool)
		if err != nil {
			return nil
		}
		devices = nil
		for _, field := range strings.Fields(string(output)) {
			if strings.HasPrefix(field, "/dev/") {
				devices = append(devices, field)
			}
		}
	}
	var disks []string
	for _, device := range devices {
		output, err := p.output(ctx, "lsblk", "--list", "--noheadings", "--inverse", "--paths", "--output", "NAME,TYPE", device)
		if err != nil {
			continue
		}
		for line := range strings.Lines(string(output)) {
			fields := strings.Fields(line)
			if len(fields) == 2 && fields[1] == "disk" && !slices.Contains(disks, fields[0]) {
				disks = append(disks, fields[0])
			}
		}
	}
	return disks
}

// snapshotID is the short ID of a restic snapshot, or "".
func snapshotID(value string) string {
	if snapshotPattern.MatchString(value) {
		return value
	}
	return ""
}

var snapshotPattern = regexp.MustCompile(`^[a-f0-9]{8}$`)

// stateText is what backup.sh writes in the state: plain text on one line.
var stateText = regexp.MustCompile(`^[A-Za-z0-9 ._:/()+,-]{0,240}$`)

// status reads the state that backup.sh writes. Each value is checked, so a
// broken file shows as no state, never as text that the page didn't expect.
func (p *PhotosBackups) status() (PhotosBackupStatus, error) {
	var status PhotosBackupStatus
	file, err := os.Open(p.StatePath)
	if errors.Is(err, os.ErrNotExist) {
		return status, nil
	}
	if err != nil {
		return status, err
	}
	defer file.Close()

	scanner := bufio.NewScanner(io.LimitReader(file, 16*1024))
	for scanner.Scan() {
		key, value, found := strings.Cut(scanner.Text(), "=")
		if !found || !stateText.MatchString(value) {
			continue
		}
		when := func() time.Time {
			t, _ := time.Parse(time.RFC3339, value)
			return t
		}
		number := func() int64 {
			n, _ := strconv.ParseInt(value, 10, 64)
			return max(n, 0)
		}
		result := func(allowed ...string) string {
			if slices.Contains(allowed, value) {
				return value
			}
			return ""
		}
		switch key {
		case "BACKUP_STARTED":
			status.Started = when()
		case "BACKUP_FINISHED":
			status.Finished = when()
		case "BACKUP_RESULT":
			status.Result = result("ok", "failed")
		case "BACKUP_MESSAGE":
			status.Message = value
		case "SNAPSHOT":
			status.Snapshot = snapshotID(value)
		case "NAS_BYTES":
			status.NAS.Bytes = number()
		case "NAS_SNAPSHOTS":
			status.NAS.Snapshots = int(number())
		case "OFFSITE_RESULT":
			status.Offsite.Result = result("ok", "failed", "missing", "off")
		case "OFFSITE_MESSAGE":
			status.Offsite.Message = value
		case "OFFSITE_FINISHED":
			status.Offsite.Finished = when()
		case "OFFSITE_BYTES":
			status.Offsite.Bytes = number()
		case "OFFSITE_SNAPSHOTS":
			status.Offsite.Snapshots = int(number())
		case "OFFSITE_DISK":
			if offsiteIDPattern.MatchString(value) {
				status.Offsite.disk = value
			}
		case "VERIFY_FINISHED":
			status.Verified = when()
		case "VERIFY_RESULT":
			status.VerifyResult = result("ok", "failed")
		case "VERIFY_MESSAGE":
			status.VerifyMessage = value
		case "VERIFY_SNAPSHOT":
			status.VerifySnapshot = snapshotID(value)
		}
	}
	return status, scanner.Err()
}

// RegisterPhotosBackups serves the settings and the state of the Photos backups.
func RegisterPhotosBackups(mux *http.ServeMux, backups *PhotosBackups, logger *slog.Logger) {
	mux.HandleFunc("GET /v1/apps/photos/backups", func(w http.ResponseWriter, r *http.Request) {
		view, err := backups.View()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(view)
	})
	mux.HandleFunc("PUT /v1/apps/photos/backups", func(w http.ResponseWriter, r *http.Request) {
		var settings PhotosBackupSettings
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024)).Decode(&settings); err != nil {
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return
		}
		err := backups.Save(r.Context(), settings)
		switch {
		case errors.Is(err, ErrInvalidAnswers):
			http.Error(w, err.Error(), http.StatusBadRequest)
		case err != nil:
			logger.Error("save photos backup settings", "error", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
		default:
			logger.Info("photos backup settings saved", "offsite", settings.OffsitePath != "")
			w.WriteHeader(http.StatusNoContent)
		}
	})
}

// PhotosBackups returns the settings and the state of the Photos backups.
func (c *Client) PhotosBackups(ctx context.Context) (PhotosBackupView, error) {
	var view PhotosBackupView
	err := c.do(ctx, http.MethodGet, "/v1/apps/photos/backups", nil, &view)
	return view, err
}

// SavePhotosBackups saves the settings of the Photos backups. A folder that
// the host refuses comes back as ErrInvalidAnswers, with the reason.
func (c *Client) SavePhotosBackups(ctx context.Context, settings PhotosBackupSettings) error {
	data, err := json.Marshal(settings)
	if err != nil {
		return err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPut, "http://agent/v1/apps/photos/backups", bytes.NewReader(data))
	if err != nil {
		return err
	}
	response, err := c.http.Do(request)
	if err != nil {
		return fmt.Errorf("host agent: %w", err)
	}
	defer response.Body.Close()
	message, _ := io.ReadAll(io.LimitReader(response.Body, 512))
	switch {
	case response.StatusCode == http.StatusBadRequest:
		text := strings.TrimPrefix(strings.TrimSpace(string(message)), ErrInvalidAnswers.Error()+": ")
		return fmt.Errorf("%w: %s", ErrInvalidAnswers, text)
	case response.StatusCode >= 300:
		return fmt.Errorf("host agent /v1/apps/photos/backups: %s %s", response.Status, strings.TrimSpace(string(message)))
	}
	return nil
}
