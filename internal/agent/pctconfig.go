package agent

import (
	"context"
	"os/exec"
	"strconv"
	"sync"
	"time"
)

// configTTL is how long the agent keeps the config of a container for its
// checks. Each pct call starts a Perl program on the host, and the pages and
// loops check the same containers every few seconds. Only an install, which
// takes minutes, changes the tags and shares that the checks read.
const configTTL = 30 * time.Second

var configs struct {
	sync.Mutex
	entries map[int]cachedConfig
}

type cachedConfig struct {
	text string
	at   time.Time
}

// containerConfig is the output of pct config for a container, at most
// configTTL old. Errors are never kept.
func containerConfig(ctx context.Context, vmid int) (string, error) {
	configs.Lock()
	if entry, ok := configs.entries[vmid]; ok && time.Since(entry.at) < configTTL {
		configs.Unlock()
		return entry.text, nil
	}
	configs.Unlock()

	data, err := exec.CommandContext(ctx, "pct", "config", strconv.Itoa(vmid)).Output()
	if err != nil {
		return "", err
	}
	configs.Lock()
	if configs.entries == nil {
		configs.entries = map[int]cachedConfig{}
	}
	configs.entries[vmid] = cachedConfig{text: string(data), at: time.Now()}
	configs.Unlock()
	return string(data), nil
}
