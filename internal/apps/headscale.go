package apps

import (
	"context"
	"fmt"
	"strings"

	"iglo/internal/agent"
	"iglo/internal/proxmox"
)

// CheckHeadscaleBudget counts every guest cap on this node, including stopped
// guests, and keeps 4 GiB for the host. Caps are not reservations.
func CheckHeadscaleBudget(resources []proxmox.Resource, memory agent.HostMemory) error {
	if memory.TotalBytes <= 0 || memory.Node == "" {
		return fmt.Errorf("%w: cannot verify host memory", agent.ErrInvalidAnswers)
	}
	memory.TotalBytes = min(memory.TotalBytes, 32<<30)
	found := false
	total := int64((agent.HeadscaleMemoryMiB + 4096) << 20)
	for _, r := range resources {
		if r.Type == "node" && r.Node == memory.Node {
			found = true
		}
		if r.Node == memory.Node && (r.Type == "lxc" || r.Type == "qemu") && r.Template != 1 {
			if r.MaxMem <= 0 || r.MaxMem > memory.TotalBytes {
				return fmt.Errorf("%w: cannot verify memory cap for guest %d", agent.ErrInvalidAnswers, r.VMID)
			}
			if r.MaxMem > memory.TotalBytes-total {
				return fmt.Errorf("%w: existing guest caps plus Headscale and 4 GiB host headroom exceed host RAM; review guest caps first", agent.ErrInvalidAnswers)
			}
			total += r.MaxMem
		}
	}
	if !found || total > memory.TotalBytes || memory.AvailableBytes < int64((agent.HeadscaleMemoryMiB+4096)<<20) {
		return fmt.Errorf("%w: Headscale needs 512 MiB while keeping 4 GiB host headroom", agent.ErrInvalidAnswers)
	}
	return nil
}

func (s *Service) headscaleBudget(ctx context.Context) error {
	a, ok := s.Agent.(interface {
		HostMemory(context.Context) (agent.HostMemory, error)
	})
	if !ok {
		return fmt.Errorf("could not check host memory")
	}
	memory, err := a.HostMemory(ctx)
	if err != nil {
		return err
	}
	resources, err := s.Proxmox.Resources(ctx)
	if err != nil {
		return err
	}
	memory.Node = strings.Split(memory.Node, ".")[0]
	return CheckHeadscaleBudget(resources, memory)
}

func (s *Service) RemoveHeadscale(ctx, background context.Context, confirm string) error {
	app, g, err := s.managedGuest(ctx, agent.HeadscaleApp)
	if err != nil {
		return err
	}
	if err = s.checkIdle(ctx); err != nil {
		return err
	}
	description, err := s.Proxmox.ContainerDescription(ctx, g.Node, g.VMID)
	if err != nil {
		return err
	}
	if !agent.ValidHeadscaleHostname(confirm) || agent.HeadscaleHostname(description) != confirm {
		return fmt.Errorf("%w: type the installed controller hostname to confirm removal", agent.ErrInvalidAnswers)
	}
	a, ok := s.Agent.(interface {
		RemoveHeadscale(context.Context, int, string) error
	})
	if !ok {
		return ErrPlanned
	}
	if err = a.RemoveHeadscale(ctx, g.VMID, confirm); err != nil {
		return err
	}
	s.watch(background, operation{app: app, guest: g, action: agent.ActionRemove})
	return nil
}
