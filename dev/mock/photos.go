package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"iglo/internal/agent"
)

// The fake host has these disks for the copy off the NAS. /mnt/backup-disk
// is a USB disk of its own, /mnt/nas a share of the NAS and /mnt/second a
// second partition of the system disk. Every other folder is on the root
// disk. With .dev/backup-disk-unplugged, the USB disk is gone.
func fakeDisks(_ context.Context, name string, args ...string) ([]byte, error) {
	last := args[len(args)-1]
	if name == "lsblk" {
		switch last {
		case "/dev/sdb1":
			return []byte("/dev/sdb1 part\n/dev/sdb disk\n"), nil
		case "/dev/nvme0n1p4":
			return []byte("/dev/nvme0n1p4 part\n/dev/nvme0n1 disk\n"), nil
		case "/dev/mapper/pve-root":
			return []byte("/dev/mapper/pve-root lvm\n/dev/nvme0n1p3 part\n/dev/nvme0n1 disk\n"), nil
		}
		return nil, fmt.Errorf("lsblk: %s: not a block device", last)
	}
	_, unplugged := os.Stat(statePath("backup-disk-unplugged"))
	switch {
	case under(last, "/mnt/backup-disk") && unplugged != nil:
		return []byte("/mnt/backup-disk ext4 /dev/sdb1\n"), nil
	case under(last, "/mnt/nas"):
		return []byte("/mnt/nas cifs //umbrel.local/photos\n"), nil
	case under(last, "/mnt/second"):
		return []byte("/mnt/second ext4 /dev/nvme0n1p4\n"), nil
	}
	return []byte("/ ext4 /dev/mapper/pve-root\n"), nil
}

func under(path, root string) bool {
	return path == root || strings.HasPrefix(path, root+"/")
}

// newPhotosBackups keeps the folders of the fake host in
// .dev/photos-backups/host. Every folder under /mnt exists, so the agent can
// write the marker of the backup disk there.
func newPhotosBackups() *agent.PhotosBackups {
	dir := statePath("photos-backups")
	return agent.NewPhotosBackupsWith(dir, fakeDisks, func(path string) (string, error) {
		if under(path, "/mnt") {
			if err := os.MkdirAll(filepath.Join(dir, "host", path), 0o700); err != nil {
				return "", err
			}
		}
		return path, nil
	})
}

// playPhotosRecovery plays backup.sh: it writes the state that the agent
// shows, like the real script. With .dev/fail-photos-backup, the backup
// fails while it dumps the database.
func playPhotosRecovery(backups *agent.PhotosBackups, action string, write func(string)) int {
	state := map[string]string{}
	keys := []string{"BACKUP_STARTED", "BACKUP_FINISHED", "BACKUP_RESULT", "BACKUP_MESSAGE", "SNAPSHOT", "NAS_BYTES", "NAS_SNAPSHOTS",
		"OFFSITE_RESULT", "OFFSITE_MESSAGE", "OFFSITE_FINISHED", "OFFSITE_BYTES", "OFFSITE_SNAPSHOTS", "OFFSITE_DISK", "VERIFY_FINISHED", "VERIFY_RESULT", "VERIFY_MESSAGE", "VERIFY_SNAPSHOT"}
	if data, err := os.ReadFile(backups.StatePath); err == nil {
		for line := range strings.Lines(string(data)) {
			if key, value, found := strings.Cut(strings.TrimSpace(line), "="); found {
				state[key] = value
			}
		}
	}
	save := func() {
		var out strings.Builder
		for _, key := range keys {
			if state[key] != "" {
				fmt.Fprintf(&out, "%s=%s\n", key, state[key])
			}
		}
		os.MkdirAll(statePath("photos-backups"), 0o700)
		os.WriteFile(backups.StatePath, []byte(out.String()), 0o600)
	}
	now := func() string { return time.Now().UTC().Format(time.RFC3339) }
	settings, _ := backups.Settings()
	snapshot := fmt.Sprintf("%08x", time.Now().Unix())

	write("==> Development simulation: no restic, no files read and no database restored")
	if action == "--verify" {
		if state["SNAPSHOT"] == "" {
			write("error: There is no Photos backup on the NAS yet. Back up Photos first")
			state["VERIFY_RESULT"], state["VERIFY_MESSAGE"], state["VERIFY_FINISHED"] = "failed", "The check failed while looking for the backups on the NAS", now()
			save()
			return 1
		}
		write("==> Checking the backups on the NAS, and reading 5% of their data")
		write("==> Restoring the database of snapshot " + state["SNAPSHOT"] + " into a disposable database")
		// Like backup.sh: a copy off the NAS that is set must be checked.
		if settings.OffsitePath != "" {
			problem := backups.OffsiteProblem(context.Background(), settings.OffsitePath)
			switch {
			case problem == "" && !backups.MarkedDisk(settings):
				problem = "the backup disk is not there, or not the disk saved in Backup settings"
			case problem == "" && (state["OFFSITE_DISK"] != settings.OffsiteID || state["OFFSITE_FINISHED"] == ""):
				problem = "there is no copy on the disk yet"
			}
			if problem != "" {
				message := "The backups on the NAS are fine, but the copy off the NAS is missing: " + problem
				write("error: " + message)
				state["VERIFY_RESULT"], state["VERIFY_MESSAGE"], state["VERIFY_FINISHED"] = "failed", message, now()
				save()
				return 1
			}
			write("==> Checking the copy off the NAS, and reading 5% of its data")
		}
		state["VERIFY_RESULT"], state["VERIFY_MESSAGE"], state["VERIFY_SNAPSHOT"], state["VERIFY_FINISHED"] = "ok", "", state["SNAPSHOT"], now()
		save()
		write("==> The Photos backups are checked. Live data was not changed")
		return 0
	}

	state["BACKUP_STARTED"] = now()
	sealed, _ := os.ReadFile(sealedPasswordPath(backups))
	if len(sealed) == 0 {
		write("error: Photos has no backup password. Set one with iglo-photos-key set on the host.")
		state["BACKUP_RESULT"], state["BACKUP_MESSAGE"], state["BACKUP_FINISHED"] = "failed", "The backup failed while installing restic", now()
		save()
		return 1
	}
	// The first backup makes the repository with the sealed password.
	key, err := os.ReadFile(repositoryKeyPath(backups))
	if err != nil {
		os.WriteFile(repositoryKeyPath(backups), sealed, 0o600)
		key = sealed
	}
	if string(key) != string(sealed) {
		write("Fatal: wrong password or no key found")
		state["BACKUP_RESULT"], state["BACKUP_MESSAGE"], state["BACKUP_FINISHED"] = "failed", "The backup failed while backing up to the NAS", now()
		save()
		return 1
	}
	write("==> Dumping the Immich database")
	if _, err := os.Stat(statePath("fail-photos-backup")); err == nil {
		write("error: the fake dump failed, because .dev/fail-photos-backup exists")
		state["BACKUP_RESULT"], state["BACKUP_MESSAGE"], state["BACKUP_FINISHED"] = "failed", "The backup failed while dumping the database", now()
		save()
		return 1
	}
	write("==> Backing up the database and the originals to the NAS")
	count := 1
	fmt.Sscan(state["NAS_SNAPSHOTS"], &count)
	count = min(count+1, 23)
	state["SNAPSHOT"], state["NAS_BYTES"], state["NAS_SNAPSHOTS"] = snapshot, "412316860416", fmt.Sprint(count)
	write("==> Snapshot " + snapshot + " is saved. Forgetting old snapshots")
	state["OFFSITE_DISK"] = settings.OffsiteID
	switch {
	case settings.OffsitePath == "":
		state["OFFSITE_RESULT"], state["OFFSITE_MESSAGE"] = "off", ""
	default:
		problem := backups.OffsiteProblem(context.Background(), settings.OffsitePath)
		if problem == "" && !backups.MarkedDisk(settings) {
			problem = "the backup disk is not there, or not the disk saved in Backup settings"
		}
		if problem != "" {
			write("==> No copy off the NAS: " + settings.OffsitePath + " cannot be used, because " + problem)
			state["OFFSITE_RESULT"], state["OFFSITE_MESSAGE"] = "missing", settings.OffsitePath+" cannot be used: "+problem
		} else {
			write("==> Backing up off the NAS, to " + settings.OffsitePath)
			state["OFFSITE_RESULT"], state["OFFSITE_MESSAGE"], state["OFFSITE_FINISHED"] = "ok", "", now()
			state["OFFSITE_BYTES"], state["OFFSITE_SNAPSHOTS"] = "409095634944", fmt.Sprint(count)
		}
	}
	state["BACKUP_RESULT"], state["BACKUP_MESSAGE"], state["BACKUP_FINISHED"] = "ok", "", now()
	save()
	write("==> Photos is backed up in snapshot " + snapshot)
	return 0
}

// The fake host keeps a hash of the backup password where the real one seals
// the password, and a hash of the password of the fake repository on the NAS.
// Neither is the password.
func sealedPasswordPath(backups *agent.PhotosBackups) string {
	return filepath.Join(backups.CredentialsDir, "host", "photos_restic_password.cred")
}

func repositoryKeyPath(backups *agent.PhotosBackups) string {
	return filepath.Join(statePath("photos-backups"), "repository-key")
}

func passwordHash(password string) []byte {
	sum := sha256.Sum256([]byte("iglo-dev-mock " + password))
	return []byte(hex.EncodeToString(sum[:]))
}

// sealBackupPassword plays photos_install_password of the installer: the
// password from the answers must open the fake repository when there is one,
// and is then sealed.
func sealBackupPassword(answers string, write func(string)) bool {
	password := ""
	for line := range strings.Lines(answers) {
		if value, ok := strings.CutPrefix(strings.TrimSuffix(line, "\n"), "BACKUP_PASSWORD="); ok {
			password = value
		}
	}
	if !agent.ValidBackupPassword(password) {
		write("error: The backup password must be 12 to 128 characters, on one line")
		return false
	}
	hash := passwordHash(password)
	if key, err := os.ReadFile(repositoryKeyPath(photoBackups)); err == nil {
		write("==> Checking that the backup password opens the Photos backups in /mnt/iglo/shares/backups/immich-restic")
		if string(key) != string(hash) {
			write("error: The backup password does not open the Photos backups in /mnt/iglo/shares/backups/immich-restic. Use the password of those backups")
			return false
		}
	}
	os.MkdirAll(filepath.Dir(sealedPasswordPath(photoBackups)), 0o700)
	os.WriteFile(sealedPasswordPath(photoBackups), hash, 0o600)
	write("==> The backup password is sealed on this host")
	return true
}

// photoBackups are the Photos backups of the fake host.
var photoBackups *agent.PhotosBackups

func addPhotosBackupRoutes(mux *http.ServeMux) {
	agent.RegisterPhotosBackups(mux, photoBackups, slog.Default())
}
