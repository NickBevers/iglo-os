package main

import (
	"os"
	"strings"
)

func mockHeadscaleHost() string {
	data, _ := os.ReadFile(statePath("headscale-answers"))
	for line := range strings.Lines(string(data)) {
		if host, ok := strings.CutPrefix(line, "HEADSCALE_HOSTNAME="); ok {
			return strings.TrimSpace(host)
		}
	}
	return "headscale.example.com"
}
