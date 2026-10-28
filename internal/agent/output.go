package agent

import (
	"bytes"
	"fmt"
	"sync"
)

// boundedCommandOutput bounds logs while the command runs. Result lines are parsed
// before log eviction, so a verbose check cannot hide its first packages.
type boundedCommandOutput struct {
	mu        sync.Mutex
	log       []byte
	line      []byte
	dropping  bool
	truncated bool
	result    scriptResult
}

func (o *boundedCommandOutput) Write(p []byte) (int, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	count := len(p)
	if len(p) >= maxLogBytes {
		o.log = append(o.log[:0], p[len(p)-maxLogBytes:]...)
		o.truncated = true
	} else {
		if excess := len(o.log) + len(p) - maxLogBytes; excess > 0 {
			copy(o.log, o.log[excess:])
			o.log = o.log[:len(o.log)-excess]
			o.truncated = true
		}
		o.log = append(o.log, p...)
	}
	for len(p) > 0 {
		end := bytes.IndexByte(p, '\n')
		part := p
		if end >= 0 {
			part = p[:end]
		}
		if len(o.line)+len(part) > 16<<10 {
			o.dropping = true
			o.line = nil
		}
		if !o.dropping {
			o.line = append(o.line, part...)
		}
		if end < 0 {
			break
		}
		if !o.dropping {
			if err := o.record(); err != nil {
				return count, err
			}
		}
		o.line, o.dropping = o.line[:0], false
		p = p[end+1:]
	}
	return count, nil
}

func (o *boundedCommandOutput) record() error {
	if !bytes.HasPrefix(o.line, []byte("IGLO ")) {
		return nil
	}
	parsed := parseOutput(string(o.line))
	if len(o.result.packages)+len(o.result.images)+len(parsed.packages)+len(parsed.images) > 4096 {
		return fmt.Errorf("too many update results")
	}
	o.result.packages = append(o.result.packages, parsed.packages...)
	o.result.images = append(o.result.images, parsed.images...)
	o.result.unsupported = o.result.unsupported || parsed.unsupported
	return nil
}

func (o *boundedCommandOutput) finish() (string, scriptResult, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	var err error
	if !o.dropping {
		err = o.record()
	}
	log := string(o.log)
	if o.truncated {
		log = "…" + log
	}
	if o.result.packages == nil {
		o.result.packages = []Package{}
	}
	if o.result.images == nil {
		o.result.images = []Image{}
	}
	return log, o.result, err
}
