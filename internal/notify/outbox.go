package notify

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

const (
	maxPending = 128
	// Stay below ntfy's default 4 KiB limit, including metadata.
	messageBytes = 3000
)

// Destination identifies a route without persisting its credentials.
type Destination struct {
	ID   string
	Send func(context.Context, string) error
}

func DestinationID(parts ...string) string {
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return hex.EncodeToString(sum[:])
}

type pendingMessage struct {
	Text     string    `json:"text"`
	Targets  []string  `json:"targets"`
	Next     time.Time `json:"next"`
	Attempts int       `json:"attempts"`
}

// Outbox keeps failed deliveries across restarts. Successful destinations are
// removed independently, so a failed provider does not repeat another's alerts.
// Messages contain the same short, non-secret summaries sent to the providers.
type Outbox struct {
	mu      sync.Mutex
	path    string
	pending []pendingMessage
}

func NewOutbox(path string) (*Outbox, error) {
	q := &Outbox{path: path}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return q, nil
	}
	if err != nil {
		return nil, err
	}
	if len(data) > 2<<20 {
		return nil, errors.New("notification outbox is too large")
	}
	if err := json.Unmarshal(data, &q.pending); err != nil {
		return nil, err
	}
	if len(q.pending) > maxPending {
		return nil, errors.New("too many pending notifications")
	}
	for _, item := range q.pending {
		if len(item.Text) > messageBytes || len(item.Targets) > 2 {
			return nil, errors.New("invalid pending notification")
		}
	}
	return q, nil
}

func (q *Outbox) save() error {
	data, err := json.Marshal(q.pending)
	if err != nil {
		return err
	}
	temp := filepath.Join(filepath.Dir(q.path), "."+filepath.Base(q.path)+".tmp")
	if err := os.WriteFile(temp, data, 0600); err != nil {
		return err
	}
	return os.Rename(temp, q.path)
}

func (q *Outbox) Enqueue(text string, destinations []Destination, now time.Time) error {
	if len(destinations) == 0 || text == "" {
		return nil
	}
	var chunks []string
	for len(text) > messageBytes {
		end := messageBytes
		for !utf8.RuneStart(text[end]) {
			end--
		}
		chunks = append(chunks, text[:end])
		text = text[end:]
	}
	if text != "" {
		chunks = append(chunks, text)
	}
	targets := make([]string, 0, len(destinations))
	for _, target := range destinations {
		targets = append(targets, target.ID)
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	if len(q.pending)+len(chunks) > maxPending {
		return errors.New("notification outbox is full; delivery needs attention")
	}
	previous := len(q.pending)
	for _, chunk := range chunks {
		q.pending = append(q.pending, pendingMessage{Text: chunk, Targets: slices.Clone(targets), Next: now})
	}
	if err := q.save(); err != nil {
		q.pending = q.pending[:previous]
		return err
	}
	return nil
}

func (q *Outbox) Flush(ctx context.Context, destinations []Destination, now time.Time) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	active := map[string]Destination{}
	for _, destination := range destinations {
		active[destination.ID] = destination
	}
	var errs []error
	changed := false
	for index := range q.pending {
		item := &q.pending[index]
		// Never redirect queued messages to a replacement server or topic.
		kept := slices.DeleteFunc(item.Targets, func(id string) bool { _, ok := active[id]; return !ok })
		changed = changed || len(kept) != len(item.Targets)
		item.Targets = kept
		if now.Before(item.Next) || ctx.Err() != nil {
			continue
		}
		var failed []string
		for _, id := range item.Targets {
			if err := active[id].Send(ctx, item.Text); err != nil {
				errs = append(errs, err)
				failed = append(failed, id)
			}
		}
		if len(item.Targets) == 0 {
			continue
		}
		item.Targets = failed
		item.Attempts++
		item.Next = now.Add(min(30*time.Second*time.Duration(1<<min(item.Attempts-1, 6)), 30*time.Minute))
		changed = true
	}
	q.pending = slices.DeleteFunc(q.pending, func(item pendingMessage) bool { return len(item.Targets) == 0 })
	if changed {
		if err := q.save(); err != nil {
			errs = append(errs, fmt.Errorf("save notification deliveries: %w", err))
		}
	}
	return errors.Join(errs...)
}
