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

// memoryReading is the memory of the host, for the checks.
type memoryReading struct {
	total, free int64
	// pressure is -1 when the kernel has no PSI.
	pressure float64
}

// memory reads the memory of node from the host agent: what the kernel can
// give back, page cache included, and whether tasks wait for memory. Without
// that reading, new heavy work waits. Proxmox's own figure is no stand-in: it
// counts page cache as used and knows nothing about memory pressure.
func (s *Service) memory(ctx context.Context, node string) (memoryReading, bool) {
	if s.hostMemory == nil {
		return memoryReading{}, false
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	memory, err := s.hostMemory(ctx)
	cancel()
	if err != nil || memory.Node != node || memory.Total <= 0 || memory.Available < 0 || memory.Available > memory.Total {
		return memoryReading{}, false
	}
	return memoryReading{total: memory.Total, free: memory.Available, pressure: memory.Pressure}, true
}
