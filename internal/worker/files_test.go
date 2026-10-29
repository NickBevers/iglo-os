package worker

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

func TestAdmissionTellsLeaseFromNativeControl(t *testing.T) {
	dir := t.TempDir()
	if ReadAdmission(dir) != Held {
		t.Fatal("a missing lease admitted work")
	}
	for _, c := range []struct {
		content string
		want    Admission
	}{
		{"-1\n", Native},
		{"0\n", Held},
		{strconv.FormatInt(time.Now().Add(-time.Second).Unix(), 10), Held},
		{strconv.FormatInt(time.Now().Add(time.Minute).Unix(), 10), Leased},
		{"soon", Held},
	} {
		os.WriteFile(filepath.Join(dir, "admit-until"), []byte(c.content), 0600)
		if got := ReadAdmission(dir); got != c.want {
			t.Fatal(c.content, got)
		}
		if Admitted(dir) != (c.want != Held) {
			t.Fatal("Admitted disagrees for", c.content)
		}
	}
}

func TestPublicationRefusesCollisionCancellationAndLinks(t *testing.T) {
	root := t.TempDir()
	root, _ = filepath.EvalSymlinks(root)
	source := filepath.Join(t.TempDir(), "source")
	os.WriteFile(source, []byte("media"), 0600)
	ready := func() bool { return true }
	if _, err := Publish(context.Background(), source, root, "", "output.mkv", 10, ready); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(source, []byte("other"), 0600)
	if _, err := Publish(context.Background(), source, root, "", "output.mkv", 10, ready); err == nil {
		t.Fatal("overwrote output")
	}
	data, _ := os.ReadFile(filepath.Join(root, "output.mkv"))
	if string(data) != "media" {
		t.Fatal("output changed")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Publish(ctx, source, root, "", "cancel.mkv", 10, ready); err == nil {
		t.Fatal("ignored cancellation")
	}
	target := filepath.Join(root, "linked")
	os.Symlink(t.TempDir(), target)
	if _, err := Publish(context.Background(), source, target, "", "escape.mkv", 10, ready); err == nil {
		t.Fatal("followed link")
	}
	files, _ := os.ReadDir(root)
	for _, f := range files {
		if len(f.Name()) > 0 && f.Name()[0] == '.' {
			t.Fatal("partial output remains")
		}
	}
}
