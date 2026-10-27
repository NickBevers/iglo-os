package workloads

import (
	"context"
	"time"
)

// pressureLimit is the share of time, 0 to 100, in which tasks of the host
// wait for memory, above which new heavy work waits. The kernel measures it
// over 10 seconds, so a short burst doesn't count.
const pressureLimit = 10

// HostMemory is the memory of the host, as its kernel sees it. The host agent
// reads it from /proc on the host.
type HostMemory struct {
	Node      string
	Total     int64
	Available int64
	// Pressure is -1 when the kernel has no PSI.
	Pressure float64
}

// memoryReading is the memory of the node of Immich, for the checks.
type memoryReading struct {
	total, free int64
	// pressure is -1 when it is unknown, like when the reading came from Proxmox.
	pressure float64
}

// memory reads the memory of node. The host agent knows what the kernel can
// give back, page cache included, and whether tasks wait for memory, so it
// comes first when it runs on that node. Otherwise Proxmox says what is used.
func (s *Service) memory(ctx context.Context, node string) (memoryReading, bool) {
	if s.hostMemory != nil {
		ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
		memory, err := s.hostMemory(ctx)
		cancel()
		if err == nil && memory.Node == node && memory.Total > 0 && memory.Available >= 0 && memory.Available <= memory.Total {
			return memoryReading{total: memory.Total, free: memory.Available, pressure: memory.Pressure}, true
		}
	}
	resources, err := s.pve.Resources(ctx)
	if err != nil {
		return memoryReading{}, false
	}
	for _, r := range resources {
		if r.Type == "node" && r.Node == node && r.Status == "online" && r.MaxMem > 0 && r.Mem >= 0 && r.Mem <= r.MaxMem {
			return memoryReading{total: r.MaxMem, free: r.MaxMem - r.Mem, pressure: -1}, true
		}
	}
	return memoryReading{}, false
}
