package agent

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// GPUPoint is the average load of the GPU over one interval. Time is the
// start of the interval, like the points of the Proxmox RRD.
type GPUPoint struct {
	Time int64 `json:"time"`
	// Render is the busy share of the 3D engine, from 0 to 1.
	Render float64 `json:"render"`
	// Video is the busy share of the busiest video engine, from 0 to 1. Those
	// decode and encode video for Jellyfin, video tools and Moonlight.
	Video float64 `json:"video"`
}

// GPUHistory is the load of the host's GPU over a timeframe. Available is
// false on a host without a GPU whose counters the agent can read.
type GPUHistory struct {
	Available bool       `json:"available"`
	Points    []GPUPoint `json:"points"`
}

// gpuTimeframes match the steps of the Proxmox RRD (PVE 9.2, checked
// 2026-11-11 with pvesh on the host), so the manager can put GPU points next to
// the CPU points of the same time.
var gpuTimeframes = map[string]struct {
	step int64
	span int64
}{
	"hour":  {60, 3600},
	"day":   {60, 86400},
	"week":  {1800, 7 * 86400},
	"month": {1800, 30 * 86400},
	"year":  {21600, 365 * 86400},
}

// gpuTier keeps averages of one step, the oldest first.
type gpuTier struct {
	step   int64
	size   int
	points []GPUPoint
	// open is the interval that is still filling.
	open                 int64
	render, video, count float64
}

func (t *gpuTier) add(at int64, render, video float64) (closed bool) {
	start := at / t.step * t.step
	if t.count > 0 && start != t.open {
		t.points = append(t.points, t.average())
		if extra := len(t.points) - t.size; extra > 0 {
			t.points = append(t.points[:0], t.points[extra:]...)
		}
		t.render, t.video, t.count = 0, 0, 0
		closed = true
	}
	t.open = start
	t.render += render
	t.video += video
	t.count++
	return closed
}

func (t *gpuTier) average() GPUPoint {
	return GPUPoint{Time: t.open, Render: t.render / t.count, Video: t.video / t.count}
}

// since returns the points from a time on, with the interval that is still
// filling as the newest, like Proxmox.
func (t *gpuTier) since(from int64) []GPUPoint {
	points := []GPUPoint{}
	for _, point := range t.points {
		if point.Time >= from {
			points = append(points, point)
		}
	}
	if t.count > 0 && t.open >= from {
		points = append(points, t.average())
	}
	return points
}

// GPU samples the load of the host's GPU once a minute, and keeps averages
// for the timeframes of the overview's charts. A sample is a few reads of
// kernel counters, with no program to start.
type GPU struct {
	// File keeps the 30 minute and 6 hour averages over a restart. The
	// minute averages start over.
	File   string
	Logger *slog.Logger

	mu        sync.Mutex
	available bool
	tiers     []*gpuTier
}

func NewGPU() *GPU {
	return &GPU{
		File: "/var/lib/iglo-agent/gpu-history.json",
		tiers: []*gpuTier{
			{step: 60, size: 1440},
			{step: 1800, size: 1440},
			{step: 21600, size: 1460},
		},
	}
}

type gpuFile struct {
	Tiers map[int64][]GPUPoint `json:"tiers"`
}

// Run samples until the context ends. It returns at once on a host without
// readable GPU counters.
func (g *GPU) Run(ctx context.Context) {
	counters, err := openGPUCounters()
	if err != nil {
		g.Logger.Info("no GPU load history", "reason", err)
		return
	}
	defer counters.Close()
	g.load()
	g.mu.Lock()
	g.available = true
	g.mu.Unlock()

	last, err := counters.Read()
	if err != nil {
		g.Logger.Warn("could not read the GPU counters", "error", err)
		return
	}
	lastTime := time.Now()
	// Sample on the minute, so every sample falls in one RRD interval.
	timer := time.NewTimer(time.Until(lastTime.Truncate(time.Minute).Add(time.Minute)))
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		}
		now := time.Now()
		timer.Reset(time.Until(now.Truncate(time.Minute).Add(time.Minute)))

		busy, err := counters.Read()
		if err != nil {
			g.Logger.Warn("could not read the GPU counters", "error", err)
			continue
		}
		elapsed := now.Sub(lastTime)
		render, video := busy.loadSince(last, elapsed)
		last, lastTime = busy, now
		// A sample covers the minute before it, so it counts for that minute.
		if g.add(now.Add(-elapsed/2).Unix(), render, video) {
			g.save()
		}
	}
}

// add counts a sample in every tier, and says when a 30 minute interval closed.
func (g *GPU) add(at int64, render, video float64) (closed bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	for _, tier := range g.tiers {
		if tier.add(at, render, video) && tier.step == 1800 {
			closed = true
		}
	}
	return closed
}

// History returns the load over a timeframe that ends now.
func (g *GPU) History(timeframe string, now time.Time) (GPUHistory, error) {
	frame, ok := gpuTimeframes[timeframe]
	if !ok {
		return GPUHistory{}, errors.New("unknown timeframe")
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if !g.available {
		return GPUHistory{Points: []GPUPoint{}}, nil
	}
	for _, tier := range g.tiers {
		if tier.step == frame.step {
			return GPUHistory{Available: true, Points: tier.since(now.Unix() - frame.span)}, nil
		}
	}
	return GPUHistory{}, errors.New("unknown timeframe")
}

// save writes the closed averages of the longer tiers. It runs once every 30
// minutes, when one closes.
func (g *GPU) save() {
	g.mu.Lock()
	file := gpuFile{Tiers: map[int64][]GPUPoint{}}
	for _, tier := range g.tiers[1:] {
		file.Tiers[tier.step] = append([]GPUPoint(nil), tier.points...)
	}
	g.mu.Unlock()

	data, err := json.Marshal(file)
	if err == nil {
		err = writeFileAtomic(g.File, data)
	}
	if err != nil {
		g.Logger.Warn("could not save the GPU load history", "error", err)
	}
}

func (g *GPU) load() {
	data, err := os.ReadFile(g.File)
	if err != nil {
		return
	}
	var file gpuFile
	if err := json.Unmarshal(data, &file); err != nil {
		g.Logger.Warn("ignoring the saved GPU load history", "error", err)
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	for _, tier := range g.tiers[1:] {
		points := file.Tiers[tier.step]
		tier.points = points[max(0, len(points)-tier.size):]
	}
}

func writeFileAtomic(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	temp := path + ".tmp"
	if err := os.WriteFile(temp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(temp, path)
}

// gpuBusy is the busy time of the engines in nanoseconds, since the counters
// were opened.
type gpuBusy struct {
	render uint64
	video  []uint64
}

// loadSince turns the busy time since an earlier read into shares of the
// elapsed time. Counters can run a little ahead of the clock, so a share
// stops at 1.
func (b gpuBusy) loadSince(earlier gpuBusy, elapsed time.Duration) (render, video float64) {
	if elapsed <= 0 {
		return 0, 0
	}
	share := func(now, then uint64) float64 {
		if now < then {
			return 0
		}
		return min(1, float64(now-then)/float64(elapsed.Nanoseconds()))
	}
	render = share(b.render, earlier.render)
	for i := range min(len(b.video), len(earlier.video)) {
		video = max(video, share(b.video[i], earlier.video[i]))
	}
	return render, video
}

// RegisterGPU serves the history at GET /v1/gpu/history?timeframe=hour.
func RegisterGPU(mux *http.ServeMux, gpu *GPU) {
	mux.HandleFunc("GET /v1/gpu/history", func(w http.ResponseWriter, r *http.Request) {
		history, err := gpu.History(r.URL.Query().Get("timeframe"), time.Now())
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(history)
	})
}

// GPUHistory reads the load of the host's GPU over a timeframe.
func (c *Client) GPUHistory(ctx context.Context, timeframe string) (GPUHistory, error) {
	var history GPUHistory
	err := c.do(ctx, http.MethodGet, "/v1/gpu/history?"+url.Values{"timeframe": {timeframe}}.Encode(), nil, &history)
	return history, err
}
