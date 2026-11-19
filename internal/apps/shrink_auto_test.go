package apps

import (
	"slices"
	"testing"
	"time"

	"iglo/internal/shrink"
	"iglo/internal/stacks/arr"
)

func TestAutoShrinkCandidates(t *testing.T) {
	now := time.Now()
	file := func(p string, bytes, high int64, status string) shrink.File {
		return shrink.File{Path: p, Bytes: bytes, Modified: now, Status: status, Estimates: shrink.Estimates{Standard: high * 3 / 4, High: high}}
	}
	tried := file("anime/Tried/S1/e.mkv", 3000, 400, "ready")
	tried.AutoTried = true
	lib := shrink.Library{Kinds: []shrink.Folder{
		{Path: "anime", Folders: []shrink.Folder{{Path: "anime/Show", Folders: []shrink.Folder{{Path: "anime/Show/Season 01", Files: []shrink.File{
			file("anime/Show/Season 01/big.mkv", 3000, 400, "ready"),
			file("anime/Show/Season 01/edge.mkv", 1000, 400, "ready"),
			file("anime/Show/Season 01/little.mkv", 1000, 401, "ready"),
			file("anime/Show/Season 01/done.mkv", 3000, 400, "done"),
			file("anime/Show/Season 01/fresh.mkv", 3000, 400, "skipped"),
		}}}}, {Path: "anime/Tried", Files: []shrink.File{tried}}, {Path: "anime/Refused", Files: []shrink.File{file("anime/Refused/e.mkv", 3000, 400, "ready")}}}},
		{Path: "series", Folders: []shrink.Folder{{Path: "series/Show", Files: []shrink.File{file("series/Show/e.mkv", 3000, 400, "ready")}}}},
	}}
	refused := map[string]autoIdentity{"anime/Refused/e.mkv": {bytes: 3000, modified: now}}
	got := autoShrinkCandidates(lib, "high", refused)
	want := []string{"anime/Show/Season 01/big.mkv", "anime/Show/Season 01/edge.mkv"}
	if !slices.Equal(got, want) {
		t.Fatalf("high candidates = %v, want %v", got, want)
	}
	// The standard estimate is smaller, so the file that High leaves out joins.
	if got := autoShrinkCandidates(lib, "standard", refused); !slices.Contains(got, "anime/Show/Season 01/little.mkv") {
		t.Fatalf("standard candidates = %v", got)
	}
	// A refused file that changed is checked again.
	refused["anime/Refused/e.mkv"] = autoIdentity{bytes: 2000, modified: now}
	if got := autoShrinkCandidates(lib, "high", refused); !slices.Contains(got, "anime/Refused/e.mkv") {
		t.Fatalf("changed refused file left out: %v", got)
	}
}

func TestAutoShrinkPass(t *testing.T) {
	s, f := shrinkService(true)
	now := time.Now()
	big := shrink.File{Path: "anime/Show/Season 01/big.mkv", Bytes: 3000, Modified: now, Status: "ready", Estimates: shrink.Estimates{Standard: 300, High: 400}}
	refusedFile := shrink.File{Path: "anime/Scored/Season 01/e.mkv", Bytes: 3000, Modified: now, Status: "ready", Estimates: shrink.Estimates{Standard: 300, High: 400}}
	f.lib = shrink.Library{Kinds: []shrink.Folder{{Path: "anime", Files: []shrink.File{big, refusedFile}}}}
	f.checks = []arr.ShrinkCheckResult{
		{Path: "anime/Show", Paths: []string{big.Path}, State: "ok"},
		{Path: "anime/Scored", Paths: []string{refusedFile.Path}, State: "refused", Reason: "size format"},
	}
	pass := func(refused map[string]autoIdentity) {
		t.Helper()
		s.shrinkStatus.forget(true)
		select {
		case s.autoLibrary <- struct{}{}:
		default:
		}
		if err := s.autoShrinkPass(t.Context(), refused); err != nil {
			t.Fatal(err)
		}
	}
	refused := map[string]autoIdentity{}

	pass(refused)
	if len(f.submitted) != 0 || f.scans != 0 {
		t.Fatalf("a pass ran while the setting was off: %+v, %d scans", f.submitted, f.scans)
	}

	f.view = shrink.View{AutoAnime: "high", Jobs: []shrink.Job{{ID: "a", Path: "anime/Other/e.mkv", State: "running"}}}
	pass(refused)
	if len(f.submitted) != 0 || f.scans != 0 {
		t.Fatalf("a pass ran during an encode: %+v, %d scans", f.submitted, f.scans)
	}

	f.view.Jobs = nil
	pass(refused)
	if f.scans != 1 || len(f.submitted) != 1 {
		t.Fatalf("scans %d, submitted %+v", f.scans, f.submitted)
	}
	in := f.submitted[0]
	if !in.Auto || in.Profile != "high" || in.Confirm != "replace-originals" || !slices.Equal(in.Paths, []string{big.Path}) {
		t.Fatalf("submission %+v", in)
	}
	if _, ok := refused[refusedFile.Path]; !ok {
		t.Fatal("refused file not remembered")
	}

	// The worker now marks the big file as tried, and the refused one is skipped.
	f.lib.Kinds[0].Files[0].AutoTried = true
	f.checkCalls = nil
	pass(refused)
	if len(f.checkCalls) != 0 || len(f.submitted) != 1 {
		t.Fatalf("second pass submitted again: %+v", f.submitted)
	}
}
