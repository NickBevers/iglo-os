package agent

import (
	"log/slog"
	"math"
	"path/filepath"
	"testing"
	"time"
)

func TestGPULoadSinceUsesTheBusiestVideoEngine(t *testing.T) {
	earlier := gpuBusy{render: 1_000, video: []uint64{0, 5_000, 0}}
	now := gpuBusy{render: 30_001_000, video: []uint64{6_000_000_000, 45_005_000, 70_000_000_000}}

	render, video := now.loadSince(earlier, time.Minute)

	if math.Abs(render-0.0005) > 1e-9 {
		t.Errorf("render = %v, want 0.0005", render)
	}
	// The third engine ran ahead of the clock, so it stops at 1.
	if video != 1 {
		t.Errorf("video = %v, want 1", video)
	}
	if render, video := earlier.loadSince(now, time.Minute); render != 0 || video != 0 {
		t.Errorf("a counter that went back should count as idle, got %v %v", render, video)
	}
}

func TestGPUHistoryAveragesLikeProxmox(t *testing.T) {
	gpu := NewGPU()
	gpu.Logger = slog.New(slog.DiscardHandler)
	gpu.File = filepath.Join(t.TempDir(), "gpu-history.json")
	gpu.available = true

	// One sample a minute from 10:00 to 10:59 UTC, 3D at 20% and video at 60%,
	// then one sample at 11:00.
	start := time.Date(2026, 11, 11, 10, 0, 30, 0, time.UTC).Unix()
	closed := 0
	for minute := range 61 {
		if gpu.add(start+int64(minute)*60, 0.2, 0.6) {
			closed++
		}
	}
	if closed != 2 {
		t.Errorf("expected the two 30 minute intervals to close, got %d", closed)
	}

	now := time.Unix(start+60*60, 0)
	hour, err := gpu.History("hour", now)
	if err != nil {
		t.Fatal(err)
	}
	// The last hour holds 60 points, like the Proxmox RRD.
	if !hour.Available || len(hour.Points) != 60 {
		t.Fatalf("expected 60 minute points, got %d", len(hour.Points))
	}
	if first := hour.Points[0]; first.Time != start+30 || first.Render != 0.2 || first.Video != 0.6 {
		t.Errorf("a point should start on the minute: %+v", first)
	}

	week, err := gpu.History("week", now)
	if err != nil {
		t.Fatal(err)
	}
	// Two closed intervals, and the one from 11:00 that is still filling.
	if len(week.Points) != 3 || week.Points[0].Time%1800 != 0 || week.Points[2].Time != start-30+3600 {
		t.Errorf("unexpected week points: %+v", week.Points)
	}

	gpu.save()
	restarted := NewGPU()
	restarted.Logger = gpu.Logger
	restarted.File = gpu.File
	restarted.available = true
	restarted.load()
	week, _ = restarted.History("week", now)
	if len(week.Points) != 2 {
		t.Errorf("a restart should keep the closed 30 minute intervals, got %+v", week.Points)
	}
	if hour, _ := restarted.History("hour", now); len(hour.Points) != 0 {
		t.Errorf("the minute points start over after a restart, got %d", len(hour.Points))
	}

	if _, err := gpu.History("decade", now); err == nil {
		t.Error("an unknown timeframe should fail")
	}
}

func TestGPUTierDropsTheOldestPoints(t *testing.T) {
	tier := &gpuTier{step: 60, size: 3}
	for minute := range 6 {
		tier.add(int64(minute)*60, float64(minute), 0)
	}
	if len(tier.points) != 3 || tier.points[0].Time != 120 {
		t.Errorf("expected the newest three closed points, got %+v", tier.points)
	}
}

func TestGPUHistoryWithoutAGPU(t *testing.T) {
	history, err := NewGPU().History("day", time.Now())
	if err != nil || history.Available || history.Points == nil {
		t.Errorf("expected an empty, unavailable history, got %+v %v", history, err)
	}
}
