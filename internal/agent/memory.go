package agent

import (
	"context"
	"errors"
	"net/http"
	"os"
	"strconv"
	"strings"
)

// HostMemory is the memory of the Proxmox host, as its kernel sees it.
type HostMemory struct {
	Node       string `json:"node"`
	TotalBytes int64  `json:"totalBytes"`
	// AvailableBytes is what the kernel can give to new work without swapping,
	// page cache included. It is the MemAvailable of /proc/meminfo.
	AvailableBytes int64 `json:"availableBytes"`
	// Pressure is the share of the last 10 seconds, 0 to 100, in which some
	// tasks waited for memory. It is -1 when the kernel has no PSI.
	Pressure float64 `json:"pressure"`
}

// parseHostMemory reads /proc/meminfo and /proc/pressure/memory.
func parseHostMemory(meminfo, pressure string) (HostMemory, error) {
	memory := HostMemory{Pressure: -1}
	available := false
	for line := range strings.Lines(meminfo) {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		kib, err := strconv.ParseInt(fields[1], 10, 64)
		if err != nil {
			continue
		}
		switch fields[0] {
		case "MemTotal:":
			memory.TotalBytes = kib << 10
		case "MemAvailable:":
			memory.AvailableBytes, available = kib<<10, true
		}
	}
	if memory.TotalBytes <= 0 || !available || memory.AvailableBytes < 0 || memory.AvailableBytes > memory.TotalBytes {
		return HostMemory{}, errors.New("could not read the memory of the host")
	}
	for line := range strings.Lines(pressure) {
		fields := strings.Fields(line)
		if len(fields) < 2 || fields[0] != "some" {
			continue
		}
		if value, ok := strings.CutPrefix(fields[1], "avg10="); ok {
			if share, err := strconv.ParseFloat(value, 64); err == nil && share >= 0 && share <= 100 {
				memory.Pressure = share
			}
		}
	}
	return memory, nil
}

// ReadHostMemory reads the memory of this host.
func ReadHostMemory() (HostMemory, error) {
	meminfo, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return HostMemory{}, err
	}
	// Without PSI, the pressure stays -1.
	pressure, _ := os.ReadFile("/proc/pressure/memory")
	memory, err := parseHostMemory(string(meminfo), string(pressure))
	if err != nil {
		return HostMemory{}, err
	}
	memory.Node, _ = os.Hostname()
	return memory, nil
}

// HostMemory reads the memory of the host from the agent.
func (c *Client) HostMemory(ctx context.Context) (HostMemory, error) {
	var memory HostMemory
	err := c.do(ctx, http.MethodGet, "/v1/memory", nil, &memory)
	return memory, err
}
