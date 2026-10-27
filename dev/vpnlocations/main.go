// vpnlocations writes the VPN locations that Iglo-OS offers, from the
// servers.json of the Gluetun version that the media stack runs. Run it when
// the Gluetun version changes:
//
//	curl -sL https://raw.githubusercontent.com/qdm12/gluetun/v3.41.3/internal/storage/servers.json -o /tmp/servers.json
//	go run ./dev/vpnlocations -servers /tmp/servers.json -gluetun v3.41.3
package main

import (
	"encoding/json"
	"flag"
	"log"
	"os"
	"slices"

	"iglo/internal/vpn"
)

type server struct {
	Country     string `json:"country"`
	Region      string `json:"region"`
	VPN         string `json:"vpn"`
	PortForward bool   `json:"port_forward"`
}

func main() {
	servers := flag.String("servers", "", "servers.json of Gluetun")
	gluetun := flag.String("gluetun", "", "Gluetun version the file comes from")
	flag.Parse()
	if *servers == "" || *gluetun == "" {
		log.Fatal("-servers and -gluetun are required")
	}

	data, err := os.ReadFile(*servers)
	if err != nil {
		log.Fatal(err)
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		log.Fatal(err)
	}
	file := map[string]struct {
		Servers []server `json:"servers"`
	}{}
	for _, provider := range []string{"protonvpn", "private internet access"} {
		var entry struct {
			Servers []server `json:"servers"`
		}
		if err := json.Unmarshal(raw[provider], &entry); err != nil {
			log.Fatalf("%s: %v", provider, err)
		}
		file[provider] = entry
	}

	// Only servers with port forwarding: the stack turns on PORT_FORWARD_ONLY,
	// because qBittorrent needs a forwarded port.
	locations := vpn.Locations{Gluetun: *gluetun}
	for _, s := range file["protonvpn"].Servers {
		if s.PortForward && s.VPN == "wireguard" && s.Country != "" {
			locations.ProtonVPN = append(locations.ProtonVPN, s.Country)
		}
	}
	for _, s := range file["private internet access"].Servers {
		if s.PortForward && s.Region != "" {
			locations.PIA = append(locations.PIA, s.Region)
		}
	}
	slices.Sort(locations.ProtonVPN)
	slices.Sort(locations.PIA)
	locations.ProtonVPN = slices.Compact(locations.ProtonVPN)
	locations.PIA = slices.Compact(locations.PIA)

	out, err := json.MarshalIndent(locations, "", "    ")
	if err != nil {
		log.Fatal(err)
	}
	out = append(out, '\n')
	// The agent checks answers against the Go copy; the forms read the web copy.
	for _, path := range []string{"internal/vpn/locations.json", "web/src/lib/vpn-locations.json"} {
		if err := os.WriteFile(path, out, 0o644); err != nil {
			log.Fatal(err)
		}
	}
	log.Printf("%d ProtonVPN countries, %d PIA regions", len(locations.ProtonVPN), len(locations.PIA))
}
