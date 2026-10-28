package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestInstallLogReadsOnlyNewLines(t *testing.T) {
	path := filepath.Join(t.TempDir(), "install.log")
	os.WriteFile(path, nil, 0o600)
	appendLog := func(text string) {
		file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
		if err != nil {
			t.Fatal(err)
		}
		file.WriteString(text)
		file.Close()
	}
	run := time.Now()
	var l installLog

	appendLog("\x1b[1;32m==>\x1b[0m Creating container 130\nIGLO jellyfin-key ab")
	log, key, result, ok := l.read(path, run)
	if !ok || log != "==> Creating container 130\n" || key != "" || result != nil {
		t.Fatalf("first read: %q %q %v", log, key, result)
	}

	// The key line is whole now. The first key counts, and no key line shows.
	appendLog("cd12\nIGLO jellyfin-key later99\nIGLO app media 130 192.168.1.50\nIGLO app media 131 192.168.1.51\n")
	log, key, result, _ = l.read(path, run)
	if strings.Contains(log, "jellyfin-key") || key != "abcd12" || result == nil || result[2] != "130" {
		t.Fatalf("second read: %q %q %v", log, key, result)
	}
	offset := l.offset

	// Nothing new: nothing more is read.
	l.read(path, run)
	if l.offset != offset {
		t.Fatalf("offset moved from %d to %d", offset, l.offset)
	}

	// A long log keeps only its end, marked as cut.
	appendLog(strings.Repeat("x", 3*maxAppLog) + "\nlast line\n")
	log, _, _, _ = l.read(path, run)
	if !strings.HasPrefix(log, "…") || !strings.HasSuffix(log, "last line\n") || len(log) > maxAppLog+len("…") {
		t.Fatalf("long log: %d bytes, starts %q", len(log), log[:10])
	}

	// A new run truncates the file and starts over.
	os.WriteFile(path, []byte("==> Updating\n"), 0o600)
	log, key, result, _ = l.read(path, run.Add(time.Minute))
	if log != "==> Updating\n" || key != "" || result != nil {
		t.Fatalf("new run: %q %q %v", log, key, result)
	}
}
