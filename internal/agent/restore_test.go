package agent

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeHost answers pct and pvesm like a Proxmox host, and records the calls.
type fakeHost struct {
	mu      sync.Mutex
	calls   []string
	pveDir  string
	archive string
	// free is the free space of the storage, in KiB.
	free     int64
	failOn   string
	state    string
	statusCT string
	config   string
}

func (f *fakeHost) run(_ context.Context, name string, args ...string) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	call := name + " " + strings.Join(args, " ")
	f.calls = append(f.calls, call)
	if f.failOn != "" && strings.HasPrefix(call, f.failOn) {
		return []byte("something broke"), errors.New("exit status 255")
	}

	switch {
	case call == "pvesm path "+args[len(args)-1]:
		return []byte(f.archive + "\n"), nil
	case strings.HasPrefix(call, "pvesm status"):
		return []byte("Name Type Status Total Used Available %\nlocal-lvm lvmthin active 400000000 100000000 " +
			strconv.FormatInt(f.free, 10) + " 25.00%\n"), nil
	case strings.HasPrefix(call, "pct restore 99"):
		// The copy appears like a real restore makes it.
		dir := filepath.Join(f.pveDir, "nodes", "pve", "lxc")
		os.MkdirAll(dir, 0o755)
		os.WriteFile(filepath.Join(dir, args[1]+".conf"), nil, 0o644)
	case strings.HasPrefix(call, "pct config"):
		if f.config != "" {
			return []byte(f.config), nil
		}
		return []byte("arch: amd64\nunprivileged: 1\nhostname: media\nmp0: /mnt/iglo/shares/videos,mp=/data/media,backup=0\nmp1: local-lvm:vm-9900-disk-1,mp=/data/downloads,size=200G\nmp2: /run/iglo/secrets/130,mp=/mnt/iglo-secrets,ro=1\nnet0: name=eth0,bridge=vmbr0,hwaddr=BC:24:11:00:00:01,ip=dhcp\nonboot: 1\ntags: iglo;media\n"), nil
	case strings.HasPrefix(call, "pct exec"):
		return []byte(f.state + "\n"), nil
	case strings.HasPrefix(call, "pct status"):
		return []byte(f.statusCT + "\n"), nil
	case strings.HasPrefix(call, "pct destroy"):
		os.Remove(filepath.Join(f.pveDir, "nodes", "pve", "lxc", args[1]+".conf"))
	}
	return nil, nil
}

func newTestRestorer(t *testing.T) (*Restorer, *fakeHost) {
	t.Helper()
	archive := filepath.Join(t.TempDir(), "vzdump-lxc-130.tar.zst")
	os.WriteFile(archive, make([]byte, 1<<20), 0o644)
	host := &fakeHost{pveDir: t.TempDir(), archive: archive, free: 100 << 20, state: "degraded", statusCT: "status: stopped"}
	// An existing guest takes the first ID of the check range.
	os.MkdirAll(filepath.Join(host.pveDir, "nodes", "pve", "qemu-server"), 0o755)
	os.WriteFile(filepath.Join(host.pveDir, "nodes", "pve", "qemu-server", "9900.conf"), nil, 0o644)
	return &Restorer{run: host.run, PVEDir: host.pveDir, now: time.Now}, host
}

func waitRestore(t *testing.T, restorer *Restorer) RestoreStatus {
	t.Helper()
	for range 200 {
		if status := restorer.Status(); !status.Running {
			return status
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("the restore did not finish")
	return RestoreStatus{}
}

const testVolid = "nas-backups:backup/vzdump-lxc-130-2026_10_28-03_00_01.tar.zst"

func TestRestoreCheck(t *testing.T) {
	restorer, host := newTestRestorer(t)
	if _, err := restorer.Start(RestoreRequest{VMID: 130, Volid: testVolid, Storage: "local-lvm", Check: true}); err != nil {
		t.Fatal(err)
	}
	status := waitRestore(t, restorer)
	if !status.Succeeded || !strings.Contains(status.Message, "container 9901") {
		t.Fatalf("status = %+v", status)
	}

	order := []string{
		"pct restore 9901 " + testVolid + " --storage local-lvm --unique 1",
		"pct set 9901 --onboot 0 --tags iglo-check --hostname restore-check --delete mp0,mp1,mp2,net0",
		"pct start 9901",
		"pct exec 9901 -- systemctl is-system-running --wait",
		"pct destroy 9901 --purge 1 --destroy-unreferenced-disks 1",
	}
	last := -1
	for _, call := range order {
		index := slices.Index(host.calls, call)
		if index <= last {
			t.Fatalf("%q out of order in %v", call, host.calls)
		}
		last = index
	}
	for _, call := range host.calls {
		if strings.Contains(call, " 130") && !strings.Contains(call, testVolid) {
			t.Fatalf("touched the original: %q", call)
		}
	}
}

func TestRestoreCheckRemovesTheCopyWhenItFails(t *testing.T) {
	restorer, host := newTestRestorer(t)
	host.state = "starting"
	restorer.Start(RestoreRequest{VMID: 130, Volid: testVolid, Storage: "local-lvm", Check: true})
	if status := waitRestore(t, restorer); status.Succeeded || !strings.Contains(status.Message, `"starting"`) {
		t.Fatalf("status = %+v", status)
	}
	if !slices.Contains(host.calls, "pct destroy 9901 --purge 1 --destroy-unreferenced-disks 1") {
		t.Fatalf("the copy stayed: %v", host.calls)
	}

	restorer, host = newTestRestorer(t)
	host.failOn = "pct start"
	restorer.Start(RestoreRequest{VMID: 130, Volid: testVolid, Storage: "local-lvm", Check: true})
	if status := waitRestore(t, restorer); status.Succeeded || !slices.Contains(host.calls, "pct destroy 9901 --purge 1 --destroy-unreferenced-disks 1") {
		t.Fatalf("status = %+v, calls = %v", status, host.calls)
	}
}

func TestRestoreCheckNeedsSpace(t *testing.T) {
	restorer, host := newTestRestorer(t)
	host.free = 1 << 20 // 1 GiB, in KiB
	restorer.Start(RestoreRequest{VMID: 130, Volid: testVolid, Storage: "local-lvm", Check: true})
	if status := waitRestore(t, restorer); status.Succeeded || !strings.Contains(status.Message, "needs about") {
		t.Fatalf("status = %+v", status)
	}
	if slices.ContainsFunc(host.calls, func(call string) bool { return strings.HasPrefix(call, "pct restore") }) {
		t.Fatal("restored without space")
	}
}

func TestRestoreInPlace(t *testing.T) {
	restorer, host := newTestRestorer(t)
	restorer.Start(RestoreRequest{VMID: 130, Volid: testVolid, Storage: "local-lvm"})
	if status := waitRestore(t, restorer); !status.Succeeded ||
		!slices.Contains(host.calls, "pct restore 130 "+testVolid+" --storage local-lvm --force 1") {
		t.Fatalf("status = %+v, calls = %v", status, host.calls)
	}

	// A running container is never overwritten.
	restorer, host = newTestRestorer(t)
	host.statusCT = "status: running"
	restorer.Start(RestoreRequest{VMID: 130, Volid: testVolid, Storage: "local-lvm"})
	if status := waitRestore(t, restorer); status.Succeeded || slices.ContainsFunc(host.calls, func(call string) bool { return strings.HasPrefix(call, "pct restore") }) {
		t.Fatalf("status = %+v, calls = %v", status, host.calls)
	}
}

func TestRestoreRefuses(t *testing.T) {
	restorer, _ := newTestRestorer(t)
	for name, request := range map[string]RestoreRequest{
		"another guest": {VMID: 131, Volid: testVolid, Storage: "local-lvm"},
		"a VM":          {VMID: 130, Volid: "nas:backup/vzdump-qemu-130-2026_10_28-03_00_01.vma.zst", Storage: "local-lvm"},
		"a path":        {VMID: 130, Volid: "nas:backup/../../etc/shadow", Storage: "local-lvm"},
		"bad storage":   {VMID: 130, Volid: testVolid, Storage: "local lvm;id"},
	} {
		if _, err := restorer.Start(request); !errors.Is(err, ErrInvalidRestore) {
			t.Errorf("%s: %v", name, err)
		}
	}

	restorer.Start(RestoreRequest{VMID: 130, Volid: testVolid, Storage: "local-lvm", Check: true})
	if _, err := restorer.Start(RestoreRequest{VMID: 130, Volid: testVolid, Storage: "local-lvm"}); err != nil && !errors.Is(err, ErrRestoreRunning) {
		t.Fatalf("second restore: %v", err)
	}
	waitRestore(t, restorer)
}

func TestRestoreCheckIsolation(t *testing.T) {
	for _, config := range []string{
		"unprivileged: 0\n",
		"hostname: privileged-by-default\n",
		"unprivileged: 1\nlxc.mount.entry: /dev mnt/dev none bind,create=dir\n",
		"unprivileged: 1\nlxc.apparmor.profile: unconfined\n",
	} {
		t.Run(config, func(t *testing.T) {
			restorer, host := newTestRestorer(t)
			host.config = config
			restorer.Start(RestoreRequest{VMID: 130, Volid: testVolid, Storage: "local-lvm", Check: true})
			if status := waitRestore(t, restorer); status.Succeeded {
				t.Fatal("unsafe copy started")
			}
			for _, call := range host.calls {
				if strings.HasPrefix(call, "pct start") {
					t.Fatal(call)
				}
			}
			if !slices.Contains(host.calls, "pct destroy 9901 --purge 1 --destroy-unreferenced-disks 1") {
				t.Fatal("copy was not removed")
			}
		})
	}
	restorer, host := newTestRestorer(t)
	host.config = "unprivileged: 1\ndev0: /dev/dri/renderD128\nhookscript: local:snippets/hook.pl\nnet0: name=eth0\n"
	restorer.Start(RestoreRequest{VMID: 130, Volid: testVolid, Storage: "local-lvm", Check: true})
	if status := waitRestore(t, restorer); !status.Succeeded {
		t.Fatal(status)
	}
	if !slices.Contains(host.calls, "pct set 9901 --onboot 0 --tags iglo-check --hostname restore-check --delete dev0,hookscript,net0") {
		t.Fatal(host.calls)
	}
}

func TestRestoreCheckNeverDeletesConcurrentGuest(t *testing.T) {
	restorer, host := newTestRestorer(t)
	restorer.run = func(ctx context.Context, name string, args ...string) ([]byte, error) {
		if name == "pct" && args[0] == "restore" {
			path := filepath.Join(host.pveDir, "nodes", "pve", "lxc", args[1]+".conf")
			os.MkdirAll(filepath.Dir(path), 0755)
			os.WriteFile(path, []byte("hostname: unrelated\n"), 0600)
			return nil, errors.New("ID already exists")
		}
		return host.run(ctx, name, args...)
	}
	restorer.Start(RestoreRequest{VMID: 130, Volid: testVolid, Storage: "local-lvm", Check: true})
	if status := waitRestore(t, restorer); status.Succeeded {
		t.Fatal(status)
	}
	if !restorer.exists(9901) {
		t.Fatal("deleted another creator's guest")
	}
	for _, call := range host.calls {
		if strings.HasPrefix(call, "pct stop") || strings.HasPrefix(call, "pct destroy") {
			t.Fatal(call)
		}
	}
}
