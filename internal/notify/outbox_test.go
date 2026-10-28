package notify

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

func TestOutboxRetriesOnlyFailedDestinationsAcrossRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "notifications.json")
	q, err := NewOutbox(path)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	good, bad := 0, 0
	fail := true
	targets := []Destination{
		{ID: "one", Send: func(context.Context, string) error { good++; return nil }},
		{ID: "two", Send: func(context.Context, string) error {
			bad++
			if fail {
				return errors.New("offline")
			}
			return nil
		}},
	}
	if err := q.Enqueue("disk failed", targets, now); err != nil {
		t.Fatal(err)
	}
	if err := q.Flush(context.Background(), targets, now); err == nil {
		t.Fatal("failure lost")
	}
	if good != 1 || bad != 1 {
		t.Fatal(good, bad)
	}
	q, err = NewOutbox(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := q.Flush(context.Background(), targets, now.Add(29*time.Second)); err != nil {
		t.Fatal(err)
	}
	if good != 1 || bad != 1 {
		t.Fatal("retried before backoff")
	}
	fail = false
	if err := q.Flush(context.Background(), targets, now.Add(30*time.Second)); err != nil {
		t.Fatal(err)
	}
	if good != 1 || bad != 2 || len(q.pending) != 0 {
		t.Fatal(good, bad, q.pending)
	}
	info, _ := os.Stat(path)
	if info.Mode().Perm() != 0600 {
		t.Fatal(info.Mode())
	}
	before, _ := os.Stat(path)
	q.Flush(context.Background(), targets, now.Add(time.Hour))
	after, _ := os.Stat(path)
	if !before.ModTime().Equal(after.ModTime()) {
		t.Fatal("empty outbox wrote state")
	}
}

func TestOutboxSplitsUTF8AndRejectsOverflow(t *testing.T) {
	q, _ := NewOutbox(filepath.Join(t.TempDir(), "notifications.json"))
	var received strings.Builder
	targets := []Destination{{ID: "one", Send: func(_ context.Context, text string) error {
		if len(text) > messageBytes || !utf8.ValidString(text) {
			t.Fatal("invalid chunk")
		}
		received.WriteString(text)
		return nil
	}}}
	text := strings.Repeat("🧊", 2000)
	if err := q.Enqueue(text, targets, time.Time{}); err != nil {
		t.Fatal(err)
	}
	if err := q.Flush(context.Background(), targets, time.Time{}); err != nil {
		t.Fatal(err)
	}
	if received.String() != text {
		t.Fatal("message changed")
	}
	if err := q.Enqueue(strings.Repeat("x", messageBytes*(maxPending+1)), targets, time.Time{}); err == nil {
		t.Fatal("unbounded queue")
	}
	if len(q.pending) != 0 {
		t.Fatal("partially queued oversized alert")
	}
}

func TestOutboxNeverReroutesOldAlerts(t *testing.T) {
	q, _ := NewOutbox(filepath.Join(t.TempDir(), "notifications.json"))
	q.Enqueue("private", []Destination{{ID: "old"}}, time.Time{})
	calls := 0
	q.Flush(context.Background(), []Destination{{ID: "new", Send: func(context.Context, string) error { calls++; return nil }}}, time.Time{})
	if calls != 0 || len(q.pending) != 0 {
		t.Fatal("old alert rerouted")
	}
}
