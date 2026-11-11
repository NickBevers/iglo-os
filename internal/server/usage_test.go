package server

import (
	"math"
	"testing"

	"iglo/internal/agent"
	"iglo/internal/proxmox"
)

func TestDownsampleAveragesAndSkipsMissingValues(t *testing.T) {
	value := func(v float64) *float64 { return &v }
	points := withGPU([]proxmox.UsagePoint{
		{Time: 0, CPU: value(0.2)},
		{Time: 60, CPU: value(0.4)},
		{Time: 120, CPU: value(0.6)},
		{Time: 180},
		{Time: 240},
	}, agent.GPUHistory{})

	result := downsample(points, 3)

	if len(result) != 3 {
		t.Fatalf("expected 3 points, got %d", len(result))
	}
	if result[0].Time != 0 || math.Abs(*result[0].CPU-0.3) > 1e-9 {
		t.Errorf("unexpected first point: time %d cpu %v", result[0].Time, *result[0].CPU)
	}
	if result[1].Time != 120 || *result[1].CPU != 0.6 {
		t.Errorf("a missing value should not count in the average: %+v", result[1])
	}
	if result[2].CPU != nil {
		t.Errorf("a bucket without values should stay empty: %+v", result[2])
	}
	if got := downsample(points, 10); len(got) != len(points) {
		t.Errorf("short series should not change, got %d points", len(got))
	}
}

func TestWithGPUMatchesPointsByTime(t *testing.T) {
	value := func(v float64) *float64 { return &v }
	points := []proxmox.UsagePoint{{Time: 0, CPU: value(0.1)}, {Time: 60, CPU: value(0.2)}, {Time: 120}}
	gpu := agent.GPUHistory{Available: true, Points: []agent.GPUPoint{
		{Time: 60, Render: 0.3, Video: 0.7},
		{Time: 90, Render: 1, Video: 1},
	}}

	result := withGPU(points, gpu)

	if result[0].GPURender != nil || result[2].GPUVideo != nil {
		t.Errorf("points without a GPU sample should stay empty: %+v %+v", result[0], result[2])
	}
	if result[1].GPURender == nil || *result[1].GPURender != 0.3 || *result[1].GPUVideo != 0.7 {
		t.Errorf("unexpected GPU load on the matching point: %+v", result[1])
	}
	if *result[1].CPU != 0.2 {
		t.Errorf("the Proxmox values should stay: %+v", result[1])
	}
}
