// Package vpn holds the VPN providers and locations of the media stack, and
// checks a choice against them.
package vpn

import (
	_ "embed"
	"encoding/json"
	"slices"
)

// The providers that the media stack can use through Gluetun.
const (
	ProtonVPN = "protonvpn"
	PIA       = "pia"
)

// Dedicated is the location of a PIA dedicated IP. It goes alone: Gluetun
// connects to that one server.
const Dedicated = "Dedicated IP"

// Locations are the countries of ProtonVPN and the regions of PIA that have
// servers with port forwarding, in the Gluetun version that the stack runs.
type Locations struct {
	Gluetun   string   `json:"gluetun"`
	ProtonVPN []string `json:"protonvpn"`
	PIA       []string `json:"pia"`
}

//go:embed locations.json
var locationsJSON []byte

// Known are the locations made by dev/vpnlocations.
var Known = func() Locations {
	var locations Locations
	if err := json.Unmarshal(locationsJSON, &locations); err != nil {
		panic(err)
	}
	return locations
}()

// ValidProvider says whether provider is one the stack supports.
func ValidProvider(provider string) bool {
	return provider == ProtonVPN || provider == PIA
}

// ValidLocations checks one to ten locations of provider. Dedicated is only
// for PIA, and only on its own.
func ValidLocations(provider string, locations []string) bool {
	if len(locations) == 0 || len(locations) > 10 {
		return false
	}
	known := Known.ProtonVPN
	if provider == PIA {
		if slices.Contains(locations, Dedicated) {
			return len(locations) == 1
		}
		known = Known.PIA
	}
	for _, location := range locations {
		if !slices.Contains(known, location) {
			return false
		}
	}

	return !hasDuplicates(locations)
}

func hasDuplicates(values []string) bool {
	sorted := slices.Clone(values)
	slices.Sort(sorted)
	return len(slices.Compact(sorted)) != len(values)
}
