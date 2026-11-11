//go:build !linux

package agent

import "errors"

type gpuCounters struct{}

func openGPUCounters() (*gpuCounters, error) {
	return nil, errors.New("GPU counters are read on Linux only")
}

func (c *gpuCounters) Read() (gpuBusy, error) { return gpuBusy{}, nil }

func (c *gpuCounters) Close() {}
