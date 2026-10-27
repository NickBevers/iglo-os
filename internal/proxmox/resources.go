package proxmox

import (
	"context"
	"fmt"
	"net/url"
	"slices"
	"time"
)

const (
	// resourcesTTL is how long one answer of /cluster/resources is used.
	resourcesTTL = 2 * time.Second
	// versionTTL is how long the version of a node is used. It only changes
	// with an upgrade of Proxmox.
	versionTTL = 10 * time.Minute
)

// Resource is one row of /cluster/resources. Which fields are set depends on Type.
type Resource struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Node     string `json:"node"`
	VMID     int    `json:"vmid"`
	Name     string `json:"name"`
	Storage  string `json:"storage"`
	Status   string `json:"status"`
	Template int    `json:"template"`
	// Tags are separated by semicolons, like "iglo;media".
	Tags    string  `json:"tags"`
	CPU     float64 `json:"cpu"`
	MaxCPU  float64 `json:"maxcpu"`
	Mem     int64   `json:"mem"`
	MaxMem  int64   `json:"maxmem"`
	Disk    int64   `json:"disk"`
	MaxDisk int64   `json:"maxdisk"`
	Uptime  int64   `json:"uptime"`
}

type NodeStatus struct {
	PVEVersion string `json:"pveversion"`
}

type GuestType string

const (
	LXC  GuestType = "lxc"
	QEMU GuestType = "qemu"
)

type GuestAction string

const (
	Start    GuestAction = "start"
	Shutdown GuestAction = "shutdown"
	Reboot   GuestAction = "reboot"
	Stop     GuestAction = "stop"
)

func (c *Client) Resources(ctx context.Context) ([]Resource, error) {
	c.cacheMu.Lock()
	if c.cached != nil && time.Since(c.cachedAt) < resourcesTTL {
		resources := slices.Clone(c.cached)
		c.cacheMu.Unlock()
		return resources, nil
	}
	c.cacheMu.Unlock()

	result, err, _ := c.resources.Do("resources", func() (any, error) {
		var resources []Resource
		if err := c.get(ctx, "/cluster/resources", &resources); err != nil {
			return nil, err
		}
		c.cacheMu.Lock()
		c.cached, c.cachedAt = resources, time.Now()
		c.cacheMu.Unlock()
		return resources, nil
	})
	if err != nil {
		return nil, err
	}

	return slices.Clone(result.([]Resource)), nil
}

// forgetResources makes the next call ask Proxmox again, after an action
// that changes a guest.
func (c *Client) forgetResources() {
	c.cacheMu.Lock()
	c.cached = nil
	c.cacheMu.Unlock()
}

func (c *Client) NodeStatus(ctx context.Context, node string) (NodeStatus, error) {
	c.cacheMu.Lock()
	if at, ok := c.versionsAt[node]; ok && time.Since(at) < versionTTL {
		status := c.versions[node]
		c.cacheMu.Unlock()
		return status, nil
	}
	c.cacheMu.Unlock()

	var status NodeStatus
	err := c.get(ctx, "/nodes/"+url.PathEscape(node)+"/status", &status)
	if err == nil {
		c.cacheMu.Lock()
		if c.versions == nil {
			c.versions, c.versionsAt = map[string]NodeStatus{}, map[string]time.Time{}
		}
		c.versions[node], c.versionsAt[node] = status, time.Now()
		c.cacheMu.Unlock()
	}

	return status, err
}

// RunGuestAction starts a power action and returns the Proxmox task ID (UPID).
func (c *Client) RunGuestAction(ctx context.Context, node string, guestType GuestType, vmid int, action GuestAction) (string, error) {
	var upid string
	path := fmt.Sprintf("/nodes/%s/%s/%d/status/%s", url.PathEscape(node), guestType, vmid, action)
	err := c.post(ctx, path, url.Values{}, &upid)
	c.forgetResources()

	return upid, err
}

// ContainerDescription returns the notes of a container, as they are in the Proxmox UI.
func (c *Client) ContainerDescription(ctx context.Context, node string, vmid int) (string, error) {
	var config struct {
		Description string `json:"description"`
	}
	err := c.get(ctx, containerPath(node, vmid)+"/config", &config)

	return config.Description, err
}
