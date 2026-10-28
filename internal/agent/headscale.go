package agent

import (
	"context"
	"fmt"
	"net/netip"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const HeadscaleApp = "headscale"
const HeadscaleMemoryMiB = 512

// HeadscaleAnswers contain no credentials. Public DNS and router changes are manual.
type HeadscaleAnswers struct {
	Hostname      string `json:"hostname"`
	Address       string `json:"address"`
	Gateway       string `json:"gateway"`
	Storage       string `json:"storage"`
	RateMB        int    `json:"rateMB"`
	Prerequisites bool   `json:"prerequisites"`
}

var domainLabel = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$`)

func ValidHeadscaleHostname(host string) bool {
	if len(host) > 240 || host != strings.ToLower(host) || !strings.Contains(host, ".") {
		return false
	}
	for _, part := range strings.Split(host, ".") {
		if !domainLabel.MatchString(part) {
			return false
		}
	}
	if _, err := netip.ParseAddr(host); err == nil {
		return false
	}
	for _, suffix := range []string{".local", ".localhost", ".internal", ".test", ".invalid", ".example"} {
		if strings.HasSuffix(host, suffix) {
			return false
		}
	}
	return true
}

func (a HeadscaleAnswers) Validate() error {
	bad := func(message string) error { return fmt.Errorf("%w: %s", ErrInvalidAnswers, message) }
	if !ValidHeadscaleHostname(a.Hostname) {
		return bad("give a public lowercase DNS hostname, without scheme or port")
	}
	if !storageID.MatchString(a.Storage) || a.RateMB < 1 || a.RateMB > 25 {
		return bad("select storage and a 1–25 MiB/s network limit")
	}
	p, err := netip.ParsePrefix(a.Address)
	g, gerr := netip.ParseAddr(a.Gateway)
	if err != nil || gerr != nil || !p.Addr().Is4() || !p.Addr().IsPrivate() || p.Bits() < 16 || p.Bits() > 30 || !g.Is4() || !p.Contains(g) || g == p.Addr() {
		return bad("give a private IPv4 /16–/30 address and a different gateway in that subnet")
	}
	network := p.Masked().Addr().As4()
	last := network
	bits := 32 - p.Bits()
	for i := 0; i < bits; i++ {
		last[3-i/8] |= 1 << uint(i%8)
	}
	if p.Addr() == p.Masked().Addr() || p.Addr() == netip.AddrFrom4(last) || g == p.Masked().Addr() || g == netip.AddrFrom4(last) {
		return bad("the address and gateway cannot be network or broadcast addresses")
	}
	if !a.Prerequisites {
		return bad("confirm public DNS, port forwarding and a free static LAN address")
	}
	return nil
}

func (a HeadscaleAnswers) file() string {
	return fmt.Sprintf("HEADSCALE_HOSTNAME=%s\nHEADSCALE_ADDRESS=%s\nHEADSCALE_GATEWAY=%s\nSTORAGE=%s\nRATE_MB=%d\nPREREQUISITES=true\n", a.Hostname, a.Address, a.Gateway, a.Storage, a.RateMB)
}

// HeadscaleHostname reads the nonsecret marker written by the installer.
func HeadscaleHostname(description string) string {
	description = strings.ReplaceAll(description, `\n`, "\n")
	for line := range strings.Lines(description) {
		if host, ok := strings.CutPrefix(line, "iglo-headscale-hostname: "); ok && ValidHeadscaleHostname(strings.TrimSpace(host)) {
			return strings.TrimSpace(host)
		}
	}
	return ""
}

func (i *AppInstaller) RemoveHeadscale(ctx context.Context, vmid int, confirm string) error {
	if vmid < 100 || vmid > 999999999 || !ValidHeadscaleHostname(confirm) {
		return ErrInvalidAnswers
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	output, err := i.output(ctx, "pct", "config", strconv.Itoa(vmid))
	if err != nil {
		return fmt.Errorf("could not inspect the controller container")
	}
	text := string(output)
	managed := false
	for line := range strings.Lines(text) {
		if tags, ok := strings.CutPrefix(line, "tags: "); ok {
			tags = ";" + strings.TrimSpace(tags) + ";"
			managed = strings.Contains(tags, ";iglo;") && strings.Contains(tags, ";headscale;")
		}
	}
	description := ""
	for line := range strings.Lines(text) {
		if value, ok := strings.CutPrefix(line, "description: "); ok {
			// Proxmox 9 pct config percent-encodes descriptions, including colons
			// and newlines. Checked against PVE::ParseUtils on 28 October 2026.
			decoded, decodeErr := url.PathUnescape(value)
			if decodeErr != nil {
				return ErrInvalidAnswers
			}
			description = decoded
		}
	}
	if !managed || HeadscaleHostname(description) != confirm {
		return fmt.Errorf("%w: type the installed controller hostname to confirm removal", ErrInvalidAnswers)
	}
	return i.runOnContainer(HeadscaleApp, ActionRemove, vmid, "--confirm", confirm)
}

func (c *Client) RemoveHeadscale(ctx context.Context, vmid int, confirm string) error {
	return c.do(ctx, "POST", "/v1/apps/headscale/remove", AppContainerRequest{VMID: vmid, Confirm: confirm}, nil)
}
