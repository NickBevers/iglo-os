package agent

import "testing"

func TestParseHostMemory(t *testing.T) {
	meminfo := "MemTotal:       32768000 kB\nMemFree:         1000000 kB\nMemAvailable:   20480000 kB\nBuffers:          100 kB\n"
	pressure := "some avg10=12.50 avg60=3.00 avg300=1.00 total=123\nfull avg10=4.00 avg60=1.00 avg300=0.50 total=45\n"
	memory, err := parseHostMemory(meminfo, pressure)
	if err != nil || memory.TotalBytes != 32768000<<10 || memory.AvailableBytes != 20480000<<10 || memory.Pressure != 12.5 {
		t.Fatalf("memory = %+v, %v", memory, err)
	}
	if memory, _ := parseHostMemory(meminfo, ""); memory.Pressure != -1 {
		t.Fatalf("without PSI: %+v", memory)
	}
	for _, broken := range []string{"", "MemTotal: 10 kB\n", "MemTotal: 10 kB\nMemAvailable: 20 kB\n"} {
		if _, err := parseHostMemory(broken, pressure); err == nil {
			t.Errorf("accepted %q", broken)
		}
	}
}
