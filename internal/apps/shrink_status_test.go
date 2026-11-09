package apps

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"iglo/internal/shrink"
)

type shrinkLogs struct {
	mu     sync.Mutex
	levels []slog.Level
}

func (*shrinkLogs) Enabled(context.Context, slog.Level) bool { return true }
func (h *shrinkLogs) Handle(_ context.Context, r slog.Record) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.levels = append(h.levels, r.Level)
	return nil
}
func (h *shrinkLogs) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h *shrinkLogs) WithGroup(string) slog.Handler      { return h }
func (h *shrinkLogs) count(level slog.Level) int {
	h.mu.Lock()
	defer h.mu.Unlock()
	n := 0
	for _, l := range h.levels {
		if l == level {
			n++
		}
	}
	return n
}
func shrinkReads(f *shrinkFake) int { f.lock.Lock(); defer f.lock.Unlock(); return f.reads }
func shrinkCalls(f *shrinkFake) (int, int) {
	f.lock.Lock()
	defer f.lock.Unlock()
	return len(f.rescans), len(f.acks)
}

func TestShrinkStatusSharesInFlightAndCachedReads(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s, f := shrinkService(true)
		f.view = shrink.View{Jobs: doneJobs()[:1]}
		wait := make(chan struct{})
		f.statusWait = wait
		f.statusStarted = make(chan struct{}, 1)
		var wg sync.WaitGroup
		for range 12 {
			wg.Go(func() {
				v, err := s.ShrinkJobs(t.Context())
				if err != nil || len(v.Jobs) != 1 {
					t.Errorf("snapshot: %+v %v", v, err)
				}
				v.Jobs[0].Path = "local change"
			})
		}
		<-f.statusStarted
		synctest.Wait()
		if shrinkReads(f) != 1 {
			t.Fatal("duplicate in-flight reads", shrinkReads(f))
		}
		close(wait)
		wg.Wait()
		v, err := s.ShrinkJobs(t.Context())
		if err != nil || v.Jobs[0].Path == "local change" || shrinkReads(f) != 1 {
			t.Fatal(v, err, shrinkReads(f))
		}
		time.Sleep(shrinkStatusTTL)
		s.ShrinkJobs(t.Context())
		if shrinkReads(f) != 2 {
			t.Fatal("snapshot did not expire")
		}
		s.wakeShrink()
		s.ShrinkJobs(t.Context())
		if shrinkReads(f) != 3 {
			t.Fatal("user action did not forget snapshot")
		}
	})
}
func TestShrinkStatusFailureBackoffAndLogging(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s, f := shrinkService(true)
		logs := &shrinkLogs{}
		s.Logger = slog.New(logs)
		f.statusErr = errors.New("offline")
		if _, err := s.ShrinkJobs(t.Context()); err == nil {
			t.Fatal("missing error")
		}
		expected := 1
		for _, delay := range []time.Duration{time.Minute, 2 * time.Minute, 4 * time.Minute, 8 * time.Minute, 16 * time.Minute, 30 * time.Minute, 30 * time.Minute} {
			time.Sleep(delay - time.Nanosecond)
			for range 10 {
				if _, err := s.ShrinkJobs(t.Context()); err == nil {
					t.Fatal("missing cached error")
				}
			}
			if shrinkReads(f) != expected {
				t.Fatal("retried before backoff", shrinkReads(f), expected)
			}
			time.Sleep(time.Nanosecond)
			s.ShrinkJobs(t.Context())
			expected++
			if shrinkReads(f) != expected {
				t.Fatal("missing retry", shrinkReads(f), expected)
			}
		}
		if logs.count(slog.LevelWarn) != 1 {
			t.Fatal("repeated failure logs", logs.count(slog.LevelWarn))
		}
		f.lock.Lock()
		f.statusErr = errors.New("different failure")
		f.lock.Unlock()
		time.Sleep(shrinkMaxRetry)
		s.ShrinkJobs(t.Context())
		if logs.count(slog.LevelWarn) != 2 {
			t.Fatal("missing error transition")
		}
		f.lock.Lock()
		f.statusErr = nil
		f.lock.Unlock()
		time.Sleep(shrinkMaxRetry)
		if _, err := s.ShrinkJobs(t.Context()); err != nil {
			t.Fatal(err)
		}
		if logs.count(slog.LevelInfo) != 1 {
			t.Fatal("missing recovery transition")
		}
	})
}
func TestShrinkWatcherProgressSharesReadsAndDoesNoRescan(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s, f := shrinkService(true)
		var published atomic.Int64
		s.Changed = func(topic string) {
			if topic == "shrink" {
				published.Add(1)
			}
		}
		f.view.Jobs = []shrink.Job{{ID: "one", Path: "anime/Title/one.mkv", State: "running"}}
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		go s.WatchInbox(ctx)
		<-f.opened
		synctest.Wait()
		initial := shrinkReads(f)
		initialPublished := published.Load()
		for second := 1; second <= 59; second++ {
			time.Sleep(time.Second)
			f.lock.Lock()
			f.view.Jobs[0].Progress = float64(second) / 60
			f.lock.Unlock()
			f.stream <- "shrink"
			synctest.Wait()
			for range 3 {
				s.ShrinkJobs(t.Context())
			}
		}
		if reads := shrinkReads(f); reads > initial+2 {
			t.Fatal("progress or tabs caused extra worker reads", reads, initial)
		}
		if published.Load() > initialPublished+2 {
			t.Fatal("progress caused repeated browser invalidations", published.Load(), initialPublished)
		}
		if rescans, acks := shrinkCalls(f); rescans != 0 || acks != 0 {
			t.Fatal("progress started a rescan", rescans, acks)
		}
		f.lock.Lock()
		f.view.Jobs[0].State = "done"
		f.lock.Unlock()
		f.stream <- "shrink"
		synctest.Wait()
		time.Sleep(shrinkStatusTTL)
		synctest.Wait()
		if rescans, acks := shrinkCalls(f); rescans != 1 || acks != 1 {
			t.Fatal("final state was not processed", rescans, acks)
		}
	})
}
func TestShrinkWatcherUnreachableBacksOffDespiteEvents(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s, f := shrinkService(true)
		logs := &shrinkLogs{}
		s.Logger = slog.New(logs)
		f.statusErr = errors.New("offline")
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		go s.WatchInbox(ctx)
		<-f.opened
		synctest.Wait()
		for _, delay := range []time.Duration{time.Minute, 2 * time.Minute, 4 * time.Minute, 8 * time.Minute, 16 * time.Minute, 30 * time.Minute} {
			initial := shrinkReads(f)
			time.Sleep(delay - time.Second)
			f.stream <- "shrink"
			synctest.Wait()
			if shrinkReads(f) != initial {
				t.Fatal("event bypassed backoff")
			}
			time.Sleep(time.Second)
			synctest.Wait()
			if shrinkReads(f) != initial+1 {
				t.Fatal("retry deadline missed", delay, shrinkReads(f), initial)
			}
		}
		if logs.count(slog.LevelWarn) != 1 {
			t.Fatal("unreachable queue logged repeatedly")
		}
	})
}
func TestShrinkBatchRefreshWaitsForQueueToDrain(t *testing.T) {
	for _, lastState := range []string{"done", "failed", "not-smaller", "cancelled"} {
		t.Run(lastState, func(t *testing.T) {
			s, f := shrinkService(true)
			f.view.Jobs = doneJobs()[:2]
			f.view.Jobs[1].State = "running"
			refreshes := 0
			s.SetLibraryRefresh(func(context.Context) error { refreshes++; return nil })
			pending, err := s.rescanShrink(t.Context(), f, 104, copyShrinkView(f.view))
			if err != nil || !pending || len(f.rescans) != 1 || len(f.acks) != 0 || refreshes != 0 {
				t.Fatal(pending, err, f.rescans, f.acks, refreshes)
			}
			f.view.Jobs[1].Progress = 0.9
			if s.shrinkNeedsRescan(104, f.view) {
				t.Fatal("progress woke the rescan round")
			}
			f.view.Jobs[1].State = lastState
			if !s.shrinkNeedsRescan(104, f.view) {
				t.Fatal("final state did not release deferred refresh")
			}
			pending, err = s.rescanShrink(t.Context(), f, 104, copyShrinkView(f.view))
			expected := 1
			if lastState == "done" {
				expected = 2
			}
			if err != nil || pending || refreshes != 1 || len(f.acks) != expected || len(f.rescans) != expected {
				t.Fatal(pending, err, refreshes, f.acks, f.rescans)
			}
			if s.shrinkNeedsRescan(104, f.view) {
				t.Fatal("idle queue repeated a refresh")
			}
		})
	}
}
func TestShrinkAckFailureDoesNotRefreshAgain(t *testing.T) {
	s, f := shrinkService(true)
	f.view.Jobs = doneJobs()[:1]
	f.errorAck = errors.New("offline")
	refreshes := 0
	s.SetLibraryRefresh(func(context.Context) error { refreshes++; return nil })
	if pending, err := s.rescanShrink(t.Context(), f, 104, copyShrinkView(f.view)); !pending || err == nil {
		t.Fatal(pending, err)
	}
	f.errorAck = nil
	if pending, err := s.rescanShrink(t.Context(), f, 104, copyShrinkView(f.view)); pending || err != nil {
		t.Fatal(pending, err)
	}
	if refreshes != 1 || len(f.rescans) != 1 {
		t.Fatal("ack retry repeated external work", refreshes, f.rescans)
	}
}
func TestShrinkManagerRefusesFoldersAndOversizedInput(t *testing.T) {
	s, f := shrinkService(true)
	for _, paths := range [][]string{{"anime/Title"}, {"anime/Title/Season 01"}, {"anime/Title/video.mp4"}, make([]string, 101)} {
		for i := range paths {
			if paths[i] == "" {
				paths[i] = "anime/Title/one.mkv"
			}
		}
		if _, err := s.SubmitShrink(t.Context(), shrink.Input{Paths: paths, Profile: "standard", Confirm: "replace-originals"}); !errors.Is(err, shrink.ErrInvalid) {
			t.Fatal(paths, err)
		}
	}
	if len(f.checkCalls) != 0 || len(f.submitted) != 0 {
		t.Fatal("invalid file list reached the agent")
	}
}

func TestShrinkStatusDeadlineBacksOffButCancelledRequestDoesNot(t *testing.T) {
	for _, cancelled := range []bool{false, true} {
		t.Run(map[bool]string{false: "deadline", true: "cancelled"}[cancelled], func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				s, f := shrinkService(true)
				f.statusWait = make(chan struct{})
				logs := &shrinkLogs{}
				s.Logger = slog.New(logs)
				ctx, cancel := context.WithTimeout(t.Context(), time.Second)
				defer cancel()
				if cancelled {
					cancel()
				}
				if _, err := s.ShrinkJobs(ctx); err == nil {
					t.Fatal("missing context failure")
				}
				f.lock.Lock()
				f.statusWait = nil
				f.lock.Unlock()
				_, err := s.ShrinkJobs(t.Context())
				if cancelled {
					if err != nil || shrinkReads(f) != 2 || logs.count(slog.LevelWarn) != 0 {
						t.Fatal("cancelled request poisoned cache", err, shrinkReads(f))
					}
				} else {
					if !errors.Is(err, context.DeadlineExceeded) || shrinkReads(f) != 1 || logs.count(slog.LevelWarn) != 1 {
						t.Fatal("deadline bypassed backoff", err, shrinkReads(f))
					}
					time.Sleep(shrinkPoll)
					if _, err := s.ShrinkJobs(t.Context()); err != nil {
						t.Fatal(err)
					}
				}
			})
		})
	}
}
