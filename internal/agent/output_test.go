package agent

import (
	"bytes"
	"strings"
	"testing"
)

func TestCommandOutputKeepsResultsBeforeEvictingLogs(t *testing.T) {
	var output boundedCommandOutput
	for _, part := range []string{"IG", "LO package first 1 2\n", strings.Repeat("x", maxLogBytes*3), "\nIGLO image last repo:tag\nIGLO unsupported no apt-get"} {
		if _, err := output.Write([]byte(part)); err != nil {
			t.Fatal(err)
		}
		if len(output.log) > maxLogBytes || cap(output.line) > 32<<10 {
			t.Fatal("unbounded capture")
		}
	}
	log, result, err := output.finish()
	if err != nil || len(log) > maxLogBytes+len("…") || !strings.HasPrefix(log, "…") {
		t.Fatal(len(log), err)
	}
	if len(result.packages) != 1 || result.packages[0].Name != "first" || len(result.images) != 1 || !result.unsupported {
		t.Fatal(result)
	}
}

func TestRunnerParsesResultsOutsideLogTail(t *testing.T) {
	runner := newTestRunner(`echo 'IGLO package first 1 2'; head -c 1048576 /dev/zero | tr '\000' x; echo; echo 'IGLO done'`)
	started, err := runner.Start(JobRequest{Kind: HostCheck})
	if err != nil {
		t.Fatal(err)
	}
	job := waitForJob(t, runner, started.ID)
	if job.Status != JobSucceeded || len(job.Packages) != 1 || len(job.Log) > maxLogBytes+len("…") {
		t.Fatalf("bad bounded job: status %s, packages %v, log %d", job.Status, job.Packages, len(job.Log))
	}
}

func BenchmarkBoundedCommandOutput(b *testing.B) {
	chunk := bytes.Repeat([]byte("normal command output\n"), 1000)
	b.ReportAllocs()
	b.SetBytes(int64(len(chunk)))
	var output boundedCommandOutput
	for b.Loop() {
		output.Write(chunk)
	}
	output.finish()
}
