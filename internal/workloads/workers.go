package workloads

import (
	"context"
	"time"
)

// A short lease lets native workers fail closed while their manager is absent.
// Disabling external control writes -1, leaving native/manual controls in charge.
func (s *Service) reconcileWorkers(ctx context.Context, view *View) {
	if s.workers == nil {
		return
	}
	settings := s.state.Settings
	until := int64(-1)
	view.WorkerReason = "Native and manual worker controls"
	if settings.Workers {
		until = 0
		view.WorkerReason = "Waiting for host memory"
		resources, err := s.pve.Resources(ctx)
		found := false
		free := int64(0)
		if err == nil {
			for _, r := range resources {
				if r.Type == "node" && r.Node == settings.Node && r.Status == "online" && r.MaxMem > 0 && r.Mem >= 0 && r.Mem <= r.MaxMem {
					found = true
					free = r.MaxMem - r.Mem
				}
			}
		}
		threshold := int64(settings.ReserveGiB) * (1 << 30)
		if s.workerMemoryHeld {
			threshold += 512 << 20
		}
		if found {
			s.workerMemoryHeld = free < threshold
		}
		switch {
		case s.state.GamingUntil.After(s.now()):
			view.WorkerReason = "Gaming protection"
		case !InWindow(settings, s.now()):
			view.WorkerReason = "Outside processing window"
		case !found:
			view.WorkerError = "Host memory is unknown; new work is deferred"
		case s.workerMemoryHeld:
			view.WorkerReason = "Preserving RAM headroom"
		default:
			until = s.now().Add(time.Minute).Unix()
			// The lease lasts a minute, so renew it at the busy pace.
			s.busy = true
			view.WorkerReason = "Worker admission allowed"
		}
	}
	if err := s.workers(ctx, settings.Node, until); err != nil {
		view.WorkerError = err.Error()
	}
}
