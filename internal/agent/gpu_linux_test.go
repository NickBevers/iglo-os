package agent

import (
	"os"
	"testing"
	"time"
)

// TestGPUCountersOnAHostWithI915 runs only where the i915 counters exist and
// can be opened, which needs root or CAP_PERFMON.
func TestGPUCountersOnAHostWithI915(t *testing.T) {
	if _, err := os.Stat(i915PMU); err != nil {
		t.Skip("no i915 GPU")
	}
	counters, err := openGPUCounters()
	if err != nil {
		t.Skipf("can't open the GPU counters: %v", err)
	}
	defer counters.Close()

	first, err := counters.Read()
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(time.Second)
	second, err := counters.Read()
	if err != nil {
		t.Fatal(err)
	}
	if len(second.video) == 0 {
		t.Error("expected at least one video engine")
	}
	render, video := second.loadSince(first, time.Second)
	t.Logf("3D %.1f%%, video %.1f%%, %d video engines", render*100, video*100, len(second.video))
}
