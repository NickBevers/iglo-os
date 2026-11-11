package server

import (
	"context"
	"net/http"
	"slices"
	"strconv"
	"sync"

	"iglo/internal/agent"
	"iglo/internal/proxmox"
)

// usagePoint is a Proxmox point, with the load of the host's GPU from the
// agent. Proxmox doesn't record the GPU.
type usagePoint struct {
	proxmox.UsagePoint
	// GPURender and GPUVideo are shares from 0 to 1, nil without a GPU.
	GPURender *float64 `json:"gpuRender"`
	GPUVideo  *float64 `json:"gpuVideo"`
}

// gpuHistory is the part of the agent client that reads the GPU load.
type gpuHistory interface {
	GPUHistory(ctx context.Context, timeframe string) (agent.GPUHistory, error)
}

func (s *server) nodeUsage(w http.ResponseWriter, r *http.Request) {
	node := r.PathValue("node")
	timeframe := proxmox.Timeframe(r.URL.Query().Get("timeframe"))
	if !nodeName.MatchString(node) || !slices.Contains(proxmox.Timeframes, timeframe) {
		writeError(w, http.StatusBadRequest, "invalid node or timeframe")
		return
	}

	var (
		points []proxmox.UsagePoint
		err    error
		gpu    agent.GPUHistory
	)
	var wait sync.WaitGroup
	wait.Go(func() { points, err = s.Proxmox.NodeUsage(r.Context(), node, timeframe) })
	if source, ok := s.Agent.(gpuHistory); ok {
		wait.Go(func() {
			// Without the agent the charts show no GPU, like on a host without one.
			history, gpuErr := source.GPUHistory(r.Context(), string(timeframe))
			if gpuErr != nil {
				s.Logger.Debug("gpu history", "error", gpuErr)
				return
			}
			gpu = history
		})
	}
	wait.Wait()
	s.writeUsage(w, withGPU(points, gpu), err)
}

// withGPU puts each GPU point on the Proxmox point of the same time. Both
// start their intervals on the same multiples of the step.
func withGPU(points []proxmox.UsagePoint, gpu agent.GPUHistory) []usagePoint {
	byTime := make(map[int64]agent.GPUPoint, len(gpu.Points))
	for _, point := range gpu.Points {
		byTime[point.Time] = point
	}
	result := make([]usagePoint, len(points))
	for i, point := range points {
		result[i] = usagePoint{UsagePoint: point}
		if load, ok := byTime[point.Time]; ok && gpu.Available {
			result[i].GPURender, result[i].GPUVideo = &load.Render, &load.Video
		}
	}
	return result
}

func (s *server) guestUsage(w http.ResponseWriter, r *http.Request) {
	node := r.PathValue("node")
	guestType := proxmox.GuestType(r.PathValue("type"))
	vmid, err := strconv.Atoi(r.PathValue("vmid"))
	timeframe := proxmox.Timeframe(r.URL.Query().Get("timeframe"))

	valid := err == nil && vmid > 0 &&
		nodeName.MatchString(node) &&
		slices.Contains([]proxmox.GuestType{proxmox.LXC, proxmox.QEMU}, guestType) &&
		slices.Contains(proxmox.Timeframes, timeframe)
	if !valid {
		writeError(w, http.StatusBadRequest, "invalid guest or timeframe")
		return
	}

	points, err := s.Proxmox.GuestUsage(r.Context(), node, guestType, vmid, timeframe)
	s.writeUsage(w, withGPU(points, agent.GPUHistory{}), err)
}

func (s *server) writeUsage(w http.ResponseWriter, points []usagePoint, err error) {
	if err != nil {
		s.Logger.Error("proxmox usage", "error", err)
		writeError(w, http.StatusBadGateway, "could not read the usage history from Proxmox")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"points": downsample(points, maxUsagePoints)})
}

// maxUsagePoints keeps the charts readable: Proxmox returns 1440 points for a month or a year.
const maxUsagePoints = 240

// downsample averages neighbouring points until there are at most limit points.
func downsample(points []usagePoint, limit int) []usagePoint {
	size := (len(points) + limit - 1) / limit
	if size <= 1 {
		return points
	}

	result := make([]usagePoint, 0, limit)
	for start := 0; start < len(points); start += size {
		bucket := points[start:min(start+size, len(points))]
		result = append(result, usagePoint{
			UsagePoint: proxmox.UsagePoint{
				Time:   bucket[0].Time,
				CPU:    average(bucket, func(point usagePoint) *float64 { return point.CPU }),
				Mem:    average(bucket, func(point usagePoint) *float64 { return point.Mem }),
				MaxMem: average(bucket, func(point usagePoint) *float64 { return point.MaxMem }),
				NetIn:  average(bucket, func(point usagePoint) *float64 { return point.NetIn }),
				NetOut: average(bucket, func(point usagePoint) *float64 { return point.NetOut }),
			},
			GPURender: average(bucket, func(point usagePoint) *float64 { return point.GPURender }),
			GPUVideo:  average(bucket, func(point usagePoint) *float64 { return point.GPUVideo }),
		})
	}

	return result
}

// average skips missing values, and is nil when the bucket has none.
func average(bucket []usagePoint, value func(usagePoint) *float64) *float64 {
	sum, count := 0.0, 0
	for _, point := range bucket {
		if v := value(point); v != nil {
			sum += *v
			count++
		}
	}
	if count == 0 {
		return nil
	}

	result := sum / float64(count)
	return &result
}
