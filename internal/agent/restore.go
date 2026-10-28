package agent

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Restores of container backups run on the host, as root: Proxmox lets only
// root@pam restore a bind mount (pve-container 7d99e49, src/PVE/API2/LXC.pm,
// "restoring 'mpX' to bind mount is only possible for root"), and every app
// container of Iglo-OS has one, for a NAS share or its secrets.

var (
	ErrRestoreRunning = errors.New("a restore is already running")
	ErrInvalidRestore = errors.New("invalid restore")

	// containerBackup is the volume ID of a vzdump archive of a container.
	containerBackup  = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_.-]{0,63}:backup/vzdump-lxc-([0-9]{3,9})-[0-9_-]+\.tar(\.(zst|gz|lzo))?$`)
	checkPassthrough = regexp.MustCompile(`^(mp|net|dev)[0-9]+$`)
)

// The containers of a restore check get an ID from this range, which Iglo-OS
// doesn't use for anything else.
const (
	checkFirstID = 9900
	checkLastID  = 9999
)

// RestoreRequest restores the container backup Volid to storage Storage.
// Without Check, it replaces container VMID, which must be stopped. With
// Check, it restores to a new container in the check range, with a new MAC
// address and without network or mount points, starts it once and removes
// it again: a test that the backup restores, which can't reach the original
// or its data on the NAS.
type RestoreRequest struct {
	VMID    int    `json:"vmid"`
	Volid   string `json:"volid"`
	Storage string `json:"storage"`
	Check   bool   `json:"check"`
}

func (r RestoreRequest) validate() error {
	match := containerBackup.FindStringSubmatch(r.Volid)
	switch {
	case match == nil:
		return fmt.Errorf("%w: %q is not a container backup", ErrInvalidRestore, r.Volid)
	case match[1] != strconv.Itoa(r.VMID):
		return fmt.Errorf("%w: the backup is not of container %d", ErrInvalidRestore, r.VMID)
	case !storageID.MatchString(r.Storage):
		return fmt.Errorf("%w: invalid storage", ErrInvalidRestore)
	}
	return nil
}

// RestoreStatus is the restore that runs or ran last.
type RestoreStatus struct {
	Running    bool           `json:"running"`
	Request    RestoreRequest `json:"request"`
	Succeeded  bool           `json:"succeeded"`
	Message    string         `json:"message"`
	StartedAt  time.Time      `json:"startedAt,omitzero"`
	FinishedAt time.Time      `json:"finishedAt,omitzero"`
}

// Restorer runs one restore at a time, in the background.
type Restorer struct {
	// run runs a command on the host and returns its output. Tests replace it.
	run func(ctx context.Context, name string, args ...string) ([]byte, error)
	// PVEDir is /etc/pve, with the configs of every guest of the cluster.
	PVEDir string
	now    func() time.Time

	mu     sync.Mutex
	status RestoreStatus
}

func NewRestorer() *Restorer {
	return &Restorer{
		run: func(ctx context.Context, name string, args ...string) ([]byte, error) {
			return exec.CommandContext(ctx, name, args...).CombinedOutput()
		},
		PVEDir: "/etc/pve",
		now:    time.Now,
	}
}

// Start starts a restore and returns at once. Status says when it is done.
func (r *Restorer) Start(request RestoreRequest) (RestoreStatus, error) {
	if err := request.validate(); err != nil {
		return RestoreStatus{}, err
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if r.status.Running {
		return RestoreStatus{}, ErrRestoreRunning
	}
	r.status = RestoreStatus{Running: true, Request: request, StartedAt: r.now()}

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Hour)
		defer cancel()

		var message string
		var err error
		if request.Check {
			message, err = r.check(ctx, request)
		} else {
			message, err = r.restore(ctx, request)
		}

		r.mu.Lock()
		defer r.mu.Unlock()
		r.status.Running, r.status.Succeeded, r.status.FinishedAt = false, err == nil, r.now()
		r.status.Message = message
		if err != nil {
			r.status.Message = err.Error()
		}
	}()

	return r.status, nil
}

func (r *Restorer) Status() RestoreStatus {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.status
}

// pct runs pct and returns its trimmed output, or an error with it.
func (r *Restorer) pct(ctx context.Context, args ...string) (string, error) {
	output, err := r.run(ctx, "pct", args...)
	text := strings.TrimSpace(string(output))
	if err != nil {
		return text, fmt.Errorf("pct %s: %v: %s", args[0], err, lastLine(text))
	}
	return text, nil
}

func (r *Restorer) restore(ctx context.Context, request RestoreRequest) (string, error) {
	id := strconv.Itoa(request.VMID)
	status, err := r.pct(ctx, "status", id)
	if err != nil {
		return "", err
	}
	if status != "status: stopped" {
		return "", fmt.Errorf("container %d must be stopped first, it is %s", request.VMID, strings.TrimPrefix(status, "status: "))
	}
	if _, err := r.pct(ctx, "restore", id, request.Volid, "--storage", request.Storage, "--force", "1"); err != nil {
		return "", err
	}
	return "restored", nil
}

// check restores to a copy, starts it once and removes it.
func (r *Restorer) check(ctx context.Context, request RestoreRequest) (message string, err error) {
	if err := r.checkSpace(ctx, request); err != nil {
		return "", err
	}
	copyID, err := r.freeCheckID()
	if err != nil {
		return "", err
	}
	id := strconv.Itoa(copyID)

	// Whatever happens, the copy goes again. Only a config in the check range
	// is ever removed here.
	defer func() {
		if !r.exists(copyID) {
			return
		}
		cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		r.pct(cleanup, "stop", id)
		if _, removeErr := r.pct(cleanup, "destroy", id, "--purge", "1", "--destroy-unreferenced-disks", "1"); removeErr != nil {
			err = errors.Join(err, fmt.Errorf("could not remove the copy, container %d: %w", copyID, removeErr))
		}
	}()

	started := r.now()
	if _, err := r.pct(ctx, "restore", id, request.Volid, "--storage", request.Storage, "--unique", "1"); err != nil {
		return "", err
	}

	// No network and no mount points: the copy can't reach the original's
	// NAS shares or secrets. It doesn't start at boot, and its tag keeps it
	// out of the Apps page.
	config, err := r.pct(ctx, "config", id)
	if err != nil {
		return "", err
	}
	var remove []string
	unprivileged := false
	for line := range strings.Lines(config) {
		key, value, _ := strings.Cut(line, ":")
		key, value = strings.TrimSpace(key), strings.TrimSpace(value)
		if key == "unprivileged" && value == "1" {
			unprivileged = true
		}
		// Raw LXC options can mount host paths or weaken isolation. Refuse
		// them instead of trying to interpret an unrestricted LXC config.
		if strings.HasPrefix(key, "lxc.") {
			return "", fmt.Errorf("restore checks do not start containers with custom LXC settings (%s)", key)
		}
		// devN and hookscript are deletable options in pve-container
		// 7d99e49, LXC/Config.pm, checked on 28 October 2026.
		if checkPassthrough.MatchString(key) || key == "hookscript" {
			remove = append(remove, key)
		}
	}
	if !unprivileged {
		return "", fmt.Errorf("restore checks do not start privileged containers")
	}
	args := []string{"set", id, "--onboot", "0", "--tags", "iglo-check", "--hostname", "restore-check"}
	if len(remove) > 0 {
		args = append(args, "--delete", strings.Join(remove, ","))
	}
	if _, err := r.pct(ctx, args...); err != nil {
		return "", err
	}

	if _, err := r.pct(ctx, "start", id); err != nil {
		return "", err
	}
	// The apps in it fail without their shares and network, so degraded is fine:
	// the system booted.
	wait, cancel := context.WithTimeout(ctx, 3*time.Minute)
	state, _ := r.pct(wait, "exec", id, "--", "systemctl", "is-system-running", "--wait")
	cancel()
	state = lastLine(state)
	if state != "running" && state != "degraded" {
		return "", fmt.Errorf("the copy started, but its system is %q", state)
	}

	return fmt.Sprintf("restored as container %d, booted (%s) and removed, in %s", copyID, state, r.now().Sub(started).Round(time.Second)), nil
}

// checkSpace makes sure the copy fits: three times the archive, plus 2 GiB.
// An archive is compressed, and a thin volume only takes what is written.
func (r *Restorer) checkSpace(ctx context.Context, request RestoreRequest) error {
	path, err := r.run(ctx, "pvesm", "path", request.Volid)
	if err != nil {
		return fmt.Errorf("could not find the backup: %v", err)
	}
	info, err := os.Stat(strings.TrimSpace(string(path)))
	if err != nil {
		return fmt.Errorf("could not read the backup: %v", err)
	}
	status, err := r.run(ctx, "pvesm", "status", "--storage", request.Storage)
	if err != nil {
		return fmt.Errorf("could not read storage %s: %v", request.Storage, err)
	}
	// Name Type Status Total Used Available %, in KiB.
	lines := strings.Split(strings.TrimSpace(string(status)), "\n")
	fields := strings.Fields(lines[len(lines)-1])
	if len(fields) < 6 || fields[0] != request.Storage {
		return fmt.Errorf("could not read storage %s", request.Storage)
	}
	available, err := strconv.ParseInt(fields[5], 10, 64)
	if err != nil {
		return fmt.Errorf("could not read storage %s", request.Storage)
	}
	if needed := 3*info.Size() + 2<<30; available*1024 < needed {
		return fmt.Errorf("storage %s has %d GiB free, and the copy needs about %d GiB", request.Storage, available>>20, needed>>30)
	}
	return nil
}

func (r *Restorer) freeCheckID() (int, error) {
	for id := checkFirstID; id <= checkLastID; id++ {
		if !r.exists(id) {
			return id, nil
		}
	}
	return 0, fmt.Errorf("no free container ID from %d to %d", checkFirstID, checkLastID)
}

// exists says whether a guest with this ID has a config on any node.
func (r *Restorer) exists(id int) bool {
	for _, kind := range []string{"lxc", "qemu-server"} {
		matches, _ := filepath.Glob(filepath.Join(r.PVEDir, "nodes", "*", kind, strconv.Itoa(id)+".conf"))
		if len(matches) > 0 {
			return true
		}
	}
	return false
}

func lastLine(text string) string {
	lines := strings.Split(strings.TrimSpace(text), "\n")
	return strings.TrimSpace(lines[len(lines)-1])
}
