package apps

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"iglo/internal/agent"
	"iglo/internal/proxmox"
	"iglo/internal/shrink"
	"iglo/internal/stacks/arr"
)

type shrinkFake struct {
	statusErr     error
	statusWait    <-chan struct{}
	statusStarted chan struct{}
	checkCalls    []agent.MediaLibraryRequest
	failTitle     string
	*fakeAgent
	lock                            sync.Mutex
	checks                          []arr.ShrinkCheckResult
	checkErr, errorRescan, errorAck error
	submitted                       []shrink.Input
	rescans                         []agent.MediaLibraryRequest
	acks                            []string
	view                            shrink.View
	reads                           int
	stream                          chan string
	opened                          chan struct{}
}

func (f *shrinkFake) Library(context.Context, int) (shrink.Library, error) {
	return shrink.Library{}, nil
}
func (f *shrinkFake) ScanLibrary(context.Context, int) error          { return nil }
func (f *shrinkFake) ShrinkSettings(context.Context, int, bool) error { return nil }
func (f *shrinkFake) CancelShrink(context.Context, int, string) error { return nil }
func (f *shrinkFake) ShrinkJobs(ctx context.Context, _ int) (shrink.View, error) {
	f.lock.Lock()
	f.reads++
	view := copyShrinkView(f.view)
	err := f.statusErr
	wait, started := f.statusWait, f.statusStarted
	f.lock.Unlock()
	if started != nil {
		select {
		case started <- struct{}{}:
		default:
		}
	}
	if wait != nil {
		select {
		case <-ctx.Done():
			return shrink.View{}, ctx.Err()
		case <-wait:
		}
	}
	return view, err
}
func (f *shrinkFake) SubmitShrink(_ context.Context, _ int, in shrink.Input) ([]shrink.Job, error) {
	f.submitted = append(f.submitted, in)
	return []shrink.Job{{Path: in.Paths[0]}}, nil
}
func (f *shrinkFake) ShrinkCheck(_ context.Context, in agent.MediaLibraryRequest) ([]arr.ShrinkCheckResult, error) {
	f.checkCalls = append(f.checkCalls, in)
	return f.checks, f.checkErr
}
func (f *shrinkFake) RescanLibrary(_ context.Context, in agent.MediaLibraryRequest) ([]arr.RescanResult, error) {
	f.lock.Lock()
	defer f.lock.Unlock()
	f.rescans = append(f.rescans, in)
	if f.failTitle != "" && strings.HasPrefix(in.Paths[0], f.failTitle+"/") {
		return nil, errors.New("title unavailable")
	}
	return nil, f.errorRescan
}
func (f *shrinkFake) ShrinkRescanned(_ context.Context, _ int, id string) error {
	f.lock.Lock()
	defer f.lock.Unlock()
	if f.errorAck != nil {
		return f.errorAck
	}
	f.acks = append(f.acks, id)
	for i := range f.view.Jobs {
		if f.view.Jobs[i].ID == id {
			f.view.Jobs[i].Rescanned = true
		}
	}
	return nil
}
func (f *shrinkFake) InboxEvents(ctx context.Context, _ int, on func(string)) error {
	on("")
	if f.opened != nil {
		close(f.opened)
	}
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case topic := <-f.stream:
			on(topic)
		}
	}
}
func shrinkService(media bool) (*Service, *shrinkFake) {
	f := &shrinkFake{fakeAgent: &fakeAgent{}, stream: make(chan string, 32), opened: make(chan struct{})}
	resources := []proxmox.Resource{{Type: "lxc", VMID: 104, Tags: "iglo;inbox", Status: "running"}}
	if media {
		resources = append(resources, proxmox.Resource{Type: "lxc", VMID: 130, Tags: "iglo;media", Status: "running"})
	}
	s := New(Options{Agent: f, Proxmox: &fakeProxmox{resources: resources}})
	s.SetLibraryRefresh(func(context.Context) error { return nil })
	return s, f
}
func TestShrinkSubmissionGuard(t *testing.T) {
	for _, name := range []string{"refused", "all refused", "failure", "missing", "unknown", "absent", "stopped"} {
		t.Run(name, func(t *testing.T) {
			s, f := shrinkService(name != "absent")
			paths := []string{"anime/Safe/Season 01/one.mkv", "movies/Unsafe/movie.mkv"}
			f.checks = []arr.ShrinkCheckResult{{Paths: paths[:1], State: "ok"}, {Path: "movies/Unsafe", Paths: paths[1:], Title: "Unsafe", State: "refused", Reason: "size format"}}
			switch name {
			case "all refused":
				for i := range f.checks {
					f.checks[i].State = "refused"
				}
			case "failure":
				f.checkErr = errors.New("offline")
			case "missing":
				f.checks = f.checks[:1]
			case "unknown":
				f.checks[0].State = "maybe"
			case "stopped":
				s.Proxmox.(*fakeProxmox).resources[1].Status = "stopped"
			}
			out, err := s.SubmitShrink(t.Context(), shrink.Input{Paths: paths, Profile: "standard", Confirm: "replace-originals"})
			if name == "absent" || name == "stopped" {
				if len(f.checkCalls) != 0 {
					t.Fatal("called unavailable media", f.checkCalls)
				}
			} else if len(f.checkCalls) != 1 {
				t.Fatal("missing guard", f.checkCalls)
			}
			if name == "refused" {
				if err != nil || len(out.Refused) != 1 || len(f.submitted) != 1 || !reflect.DeepEqual(f.submitted[0].Paths, paths[:1]) {
					t.Fatalf("%+v %v %+v", out, err, f.submitted)
				}
			} else if name == "all refused" {
				if err != nil || len(out.Jobs) != 0 || len(out.Refused) != 2 || len(f.submitted) != 0 {
					t.Fatal(out, err, f.submitted)
				}
			} else if name == "absent" {
				if err != nil || len(f.submitted) != 1 || !reflect.DeepEqual(f.submitted[0].Paths, paths) {
					t.Fatal(out, err)
				}
			} else if err == nil || len(f.submitted) != 0 {
				t.Fatalf("unchecked submission: %v %+v", err, f.submitted)
			}
		})
	}
	s, f := shrinkService(true)
	for _, in := range []shrink.Input{{Paths: []string{"anime/Safe/file.mkv"}, Profile: "standard"}, {Paths: []string{"../escape"}, Profile: "standard", Confirm: "replace-originals"}} {
		if _, err := s.SubmitShrink(t.Context(), in); !errors.Is(err, shrink.ErrInvalid) {
			t.Fatal(err)
		}
	}
	if len(f.submitted) != 0 {
		t.Fatal(f.submitted)
	}
}
func doneJobs() []shrink.Job {
	return []shrink.Job{{ID: "one", Path: "anime/Title/Season 01/one.mkv", State: "done"}, {ID: "two", Path: "anime/Title/Season 02/two.mkv", State: "done"}, {ID: "three", Path: "movies/Film/movie.mkv", State: "done"}, {ID: "old", Path: "movies/Old/movie.mkv", State: "done", Rescanned: true}, {ID: "failed", Path: "movies/Failed/movie.mkv", State: "failed"}}
}
func TestShrinkRescanRound(t *testing.T) {
	for _, media := range []bool{true, false} {
		s, f := shrinkService(media)
		f.view.Jobs = doneJobs()
		refreshes := 0
		s.SetLibraryRefresh(func(context.Context) error {
			if len(f.acks) != 0 {
				t.Fatal("acked before refresh")
			}
			refreshes++
			return nil
		})
		pending, err := s.rescanShrink(t.Context(), f, 104, copyShrinkView(f.view))
		if err != nil || pending || len(f.acks) != 3 || refreshes != 1 {
			t.Fatalf("pending %v err %v acks %v refreshes %d", pending, err, f.acks, refreshes)
		}
		if media {
			if len(f.rescans) != 2 {
				t.Fatal(f.rescans)
			}
			for _, r := range f.rescans {
				if r.VMID != 130 {
					t.Fatal(r)
				}
				if r.Paths[0][:5] == "anime" && len(r.Paths) != 2 {
					t.Fatal(r)
				}
			}
		} else if len(f.rescans) != 0 {
			t.Fatal(f.rescans)
		}
		s.SetLibraryRefresh(func(context.Context) error { refreshes++; return nil })
		s.rescanShrink(t.Context(), f, 104, copyShrinkView(f.view))
		if refreshes != 1 {
			t.Fatal("refreshed idle round")
		}
	}
}
func TestShrinkRescanRetries(t *testing.T) {
	for _, failure := range []string{"rescan", "refresh", "ack"} {
		t.Run(failure, func(t *testing.T) {
			s, f := shrinkService(true)
			f.view.Jobs = doneJobs()[:1]
			bad := errors.New("offline")
			refreshes := 0
			s.SetLibraryRefresh(func(context.Context) error {
				refreshes++
				if failure == "refresh" {
					return bad
				}
				return nil
			})
			if failure == "rescan" {
				f.errorRescan = bad
			}
			if failure == "ack" {
				f.errorAck = bad
			}
			pending, err := s.rescanShrink(t.Context(), f, 104, copyShrinkView(f.view))
			if !pending || err == nil || len(f.acks) != 0 {
				t.Fatal(pending, err, f.acks)
			}
			if failure == "rescan" && refreshes != 0 {
				t.Fatal("refreshed failed rescan")
			}
			f.errorRescan = nil
			f.errorAck = nil
			s.SetLibraryRefresh(func(context.Context) error { return nil })
			pending, err = s.rescanShrink(t.Context(), f, 104, copyShrinkView(f.view))
			if pending || err != nil || len(f.acks) != 1 {
				t.Fatal(pending, err, f.acks)
			}
		})
	}
}
func TestShrinkWatcherEventsAndIdlePoll(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s, f := shrinkService(false)
		topics := map[string]int{}
		var mu sync.Mutex
		s.Changed = func(topic string) { mu.Lock(); topics[topic]++; mu.Unlock() }
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		go s.WatchInbox(ctx)
		<-f.opened
		synctest.Wait()
		count := func() int { f.lock.Lock(); defer f.lock.Unlock(); return f.reads }
		initial := count()
		time.Sleep(3 * shrinkPoll)
		synctest.Wait()
		if count() != initial {
			t.Fatal("polled idle queue")
		}
		f.stream <- "library"
		f.stream <- "inbox"
		synctest.Wait()
		if count() != initial {
			t.Fatal("unrelated event polled shrink")
		}
		f.lock.Lock()
		f.view.Jobs = []shrink.Job{{State: "queued"}}
		f.lock.Unlock()
		f.stream <- "shrink"
		synctest.Wait()
		active := count()
		time.Sleep(shrinkPoll - time.Nanosecond)
		synctest.Wait()
		if count() != active {
			t.Fatal("polled early")
		}
		time.Sleep(time.Nanosecond)
		synctest.Wait()
		if count() != active+1 {
			t.Fatal("fallback did not poll")
		}
		f.lock.Lock()
		f.view.Jobs = nil
		f.lock.Unlock()
		s.wakeShrink()
		synctest.Wait()
		idle := count()
		time.Sleep(2 * shrinkPoll)
		synctest.Wait()
		if count() != idle {
			t.Fatal("idle timer stayed active")
		}
		mu.Lock()
		defer mu.Unlock()
		for _, topic := range []string{"inbox", "library", "shrink"} {
			if topics[topic] == 0 {
				t.Fatalf("missing %s", topic)
			}
		}
	})
}

func TestShrinkRescanPartialFailure(t *testing.T) {
	s, f := shrinkService(true)
	f.view.Jobs = doneJobs()[:3]
	f.failTitle = "movies/Film"
	refreshes := 0
	s.SetLibraryRefresh(func(context.Context) error { refreshes++; return nil })
	pending, err := s.rescanShrink(t.Context(), f, 104, copyShrinkView(f.view))
	if !pending || err == nil || len(f.acks) != 2 || refreshes != 1 {
		t.Fatal(pending, err, f.acks, refreshes)
	}
	for _, id := range f.acks {
		if id == "three" {
			t.Fatal("acked failed title")
		}
	}
	f.failTitle = ""
	pending, err = s.rescanShrink(t.Context(), f, 104, copyShrinkView(f.view))
	if pending || err != nil || len(f.acks) != 3 || len(f.rescans) != 3 || refreshes != 2 {
		t.Fatal(pending, err, f.acks, refreshes)
	}
}
func TestShrinkWatcherRetriesFailedRescan(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s, f := shrinkService(true)
		f.view.Jobs = doneJobs()[:1]
		f.errorRescan = errors.New("offline")
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		go s.WatchInbox(ctx)
		<-f.opened
		synctest.Wait()
		snapshot := func() (int, int) { f.lock.Lock(); defer f.lock.Unlock(); return len(f.acks), len(f.rescans) }
		acks, attempts := snapshot()
		if acks != 0 || attempts == 0 {
			t.Fatal(acks, attempts)
		}
		f.lock.Lock()
		f.errorRescan = nil
		f.lock.Unlock()
		time.Sleep(shrinkPoll)
		synctest.Wait()
		acks, after := snapshot()
		if acks != 1 || after != attempts+1 {
			t.Fatal(acks, after, attempts)
		}
		time.Sleep(2 * shrinkPoll)
		synctest.Wait()
		_, after = snapshot()
		if after != attempts+1 {
			t.Fatal("retried acknowledged job")
		}
	})
}
