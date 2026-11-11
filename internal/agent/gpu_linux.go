package agent

import (
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unsafe"

	"golang.org/x/sys/unix"
)

// i915PMU is the perf PMU of Intel's i915 driver. Its events folder lists one
// "<engine>-busy" counter per engine, in nanoseconds, with its perf config:
// rcs0 is 3D, vcs0 and vcs1 video decode and encode, vecs0 video enhance.
// Checked on the host's kernel 7.0.14-28-pve on 2026-11-11, and the same as
// what intel_gpu_top reads.
const i915PMU = "/sys/bus/event_source/devices/i915"

var busyEvent = regexp.MustCompile(`^(rcs|vcs|vecs)\d+-busy$`)

type gpuCounters struct {
	render int
	video  []int
}

func openGPUCounters() (*gpuCounters, error) {
	pmuType, err := readSysInt(filepath.Join(i915PMU, "type"))
	if err != nil {
		return nil, errors.New("no i915 GPU counters")
	}
	// The PMU counts for the whole GPU on the first CPU of its mask.
	mask, err := os.ReadFile(filepath.Join(i915PMU, "cpumask"))
	if err != nil {
		return nil, err
	}
	cpu, err := strconv.Atoi(strings.FieldsFunc(strings.TrimSpace(string(mask)), func(r rune) bool { return r == ',' || r == '-' })[0])
	if err != nil {
		return nil, fmt.Errorf("i915 cpumask: %w", err)
	}
	entries, err := os.ReadDir(filepath.Join(i915PMU, "events"))
	if err != nil {
		return nil, err
	}
	names := []string{}
	for _, entry := range entries {
		if busyEvent.MatchString(entry.Name()) {
			names = append(names, entry.Name())
		}
	}
	sort.Strings(names)

	counters := &gpuCounters{render: -1}
	for _, name := range names {
		config, err := readEventConfig(filepath.Join(i915PMU, "events", name))
		if err != nil {
			counters.Close()
			return nil, err
		}
		attr := unix.PerfEventAttr{Type: uint32(pmuType), Size: uint32(unsafe.Sizeof(unix.PerfEventAttr{})), Config: config}
		fd, err := unix.PerfEventOpen(&attr, -1, cpu, -1, unix.PERF_FLAG_FD_CLOEXEC)
		if err != nil {
			counters.Close()
			return nil, fmt.Errorf("open %s: %w", name, err)
		}
		if name == "rcs0-busy" {
			counters.render = fd
		} else if !strings.HasPrefix(name, "rcs") {
			counters.video = append(counters.video, fd)
		} else {
			unix.Close(fd)
		}
	}
	if counters.render < 0 {
		counters.Close()
		return nil, errors.New("no 3D engine counter")
	}
	return counters, nil
}

func (c *gpuCounters) Read() (gpuBusy, error) {
	render, err := readCounter(c.render)
	if err != nil {
		return gpuBusy{}, err
	}
	busy := gpuBusy{render: render}
	for _, fd := range c.video {
		value, err := readCounter(fd)
		if err != nil {
			return gpuBusy{}, err
		}
		busy.video = append(busy.video, value)
	}
	return busy, nil
}

func (c *gpuCounters) Close() {
	if c.render >= 0 {
		unix.Close(c.render)
	}
	for _, fd := range c.video {
		unix.Close(fd)
	}
}

func readCounter(fd int) (uint64, error) {
	buffer := make([]byte, 8)
	if _, err := unix.Read(fd, buffer); err != nil {
		return 0, err
	}
	return binary.NativeEndian.Uint64(buffer), nil
}

func readSysInt(path string) (int, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(strings.TrimSpace(string(data)))
}

// readEventConfig reads an event file like "config=0x2010".
func readEventConfig(path string) (uint64, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	value, ok := strings.CutPrefix(strings.TrimSpace(string(data)), "config=")
	if !ok || strings.Contains(value, ",") {
		return 0, fmt.Errorf("%s: unexpected event %q", filepath.Base(path), data)
	}
	return strconv.ParseUint(value, 0, 64)
}
