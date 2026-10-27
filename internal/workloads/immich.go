package workloads

import (
	"context"
	"fmt"
	"net"
	"net/netip"
	"net/url"
	"time"

	"iglo/internal/proxmox"
)

// locateInterval is how often the manager looks for the container of Immich
// again. A change of the URL looks right away.
const locateInterval = 30 * time.Minute

// immichGuest is the container whose address the Immich URL has.
type immichGuest struct {
	url     string
	checked time.Time
	// node, vmid and name are empty when no running container has the address.
	node string
	vmid int
	name string
}

func (g immichGuest) describe(settings Settings) (guest, warning string) {
	if g.url != settings.URL || g.node == "" {
		return "", ""
	}
	guest = fmt.Sprintf("%s (%d) on %s", g.name, g.vmid, g.node)
	if settings.Node != "" && settings.Node != g.node {
		warning = fmt.Sprintf("Immich runs in %s, but the memory checks read node %s. Select %s.", guest, settings.Node, g.node)
	}
	return guest, warning
}

type containerInterfaces interface {
	ContainerInterfaces(ctx context.Context, node string, vmid int) ([]proxmox.Interface, error)
}

// locateImmich finds the running container with the address of the Immich
// URL. When no node is chosen yet, like after a Photos install, it saves the
// node of that container. A chosen node is never changed; the view warns.
func (s *Service) locateImmich(ctx context.Context) {
	settings := s.state.Settings
	if settings.URL == "" || (s.immich.url == settings.URL && s.now().Sub(s.immich.checked) < locateInterval) {
		return
	}
	pve, ok := s.pve.(containerInterfaces)
	if !ok {
		return
	}
	address, err := immichAddress(ctx, settings.URL)
	if err != nil {
		return
	}
	resources, err := s.pve.Resources(ctx)
	if err != nil {
		return
	}
	found := immichGuest{url: settings.URL, checked: s.now()}
	for _, resource := range resources {
		if resource.Type != "lxc" || resource.Status != "running" {
			continue
		}
		interfaces, err := pve.ContainerInterfaces(ctx, resource.Node, resource.VMID)
		if err != nil {
			continue
		}
		for _, iface := range interfaces {
			if prefix, err := netip.ParsePrefix(iface.Inet); err == nil && prefix.Addr() == address {
				found.node, found.vmid, found.name = resource.Node, resource.VMID, resource.Name
			}
		}
	}
	s.immich = found
	if settings.Node == "" && found.node != "" {
		s.state.Settings.Node = found.node
		if err := s.save(); err != nil {
			s.state.Settings.Node = ""
		}
	}
}

// immichAddress is the IPv4 address of the host in the Immich URL.
func immichAddress(ctx context.Context, server string) (netip.Addr, error) {
	u, err := url.Parse(server)
	if err != nil {
		return netip.Addr{}, err
	}
	if address, err := netip.ParseAddr(u.Hostname()); err == nil {
		return address.Unmap(), nil
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	addresses, err := net.DefaultResolver.LookupNetIP(ctx, "ip4", u.Hostname())
	if err != nil || len(addresses) == 0 {
		return netip.Addr{}, fmt.Errorf("could not resolve %s", u.Hostname())
	}
	return addresses[0].Unmap(), nil
}
