package music

import (
	"bufio"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func fixture(t *testing.T) *Service {
	t.Helper()
	root := t.TempDir()
	library := filepath.Join(root, "library")
	os.Mkdir(library, 0700)
	library, _ = filepath.EvalSymlinks(library)
	s, err := New(filepath.Join(root, "state"), library)
	if err != nil {
		t.Fatal(err)
	}
	s.MountOK = func() bool { return true }
	return s
}
func TestURLAllowlist(t *testing.T) {
	for _, raw := range []string{"http://youtube.com/watch?v=12345678901", "https://youtube.com.evil/watch?v=12345678901", "https://youtube.com:8443/watch?v=12345678901", "https://user@youtube.com/watch?v=12345678901", "https://127.0.0.1/a", "https://youtu.be/../../etc/passwd", "https://www.youtube.com/playlist?list=123"} {
		if _, err := Canonical(raw); err == nil {
			t.Fatalf("accepted %s", raw)
		}
	}
	url, err := Canonical("https://music.youtube.com/watch?v=12345678901&list=ignored")
	if err != nil || url != "https://www.youtube.com/watch?v=12345678901" {
		t.Fatal(url, err)
	}
}
func TestEmptyQueueSkipsShareCheck(t *testing.T) {
	s := fixture(t)
	checks := 0
	s.MountOK = func() bool { checks++; return true }
	os.WriteFile(filepath.Join(s.Dir, "admit-until"), []byte("-1"), 0644)
	if s.step(context.Background()) || checks != 0 {
		t.Fatalf("checked the share %d times with nothing queued", checks)
	}
}
func TestAdmissionPauseAndDurablePublication(t *testing.T) {
	s := fixture(t)
	count := 0
	s.Execute = func(_ context.Context, _ Input, stage string) error {
		count++
		return os.WriteFile(filepath.Join(stage, "clip.m4a"), []byte("test media"), 0600)
	}
	j, err := s.Submit(Input{URL: "https://youtu.be/12345678901"})
	if err != nil {
		t.Fatal(err)
	}
	if s.step(context.Background()) || count != 0 {
		t.Fatal("ran without admission")
	}
	os.WriteFile(filepath.Join(s.Dir, "admit-until"), []byte(strconv.FormatInt(time.Now().Add(-time.Minute).Unix(), 10)), 0644)
	if s.step(context.Background()) {
		t.Fatal("ran with expired lease")
	}
	os.WriteFile(filepath.Join(s.Dir, "admit-until"), []byte("-1"), 0644)
	s.Pause(true)
	if s.step(context.Background()) {
		t.Fatal("ignored manual pause")
	}
	s.Pause(false)
	if !s.step(context.Background()) || count != 1 {
		t.Fatal("did not process")
	}
	view := s.Status()
	if view.Jobs[0].State != "succeeded" || view.Jobs[0].ID != j.ID {
		t.Fatalf("%+v", view)
	}
	file := filepath.Join(s.Library, view.Jobs[0].File)
	if data, err := os.ReadFile(file); err != nil || string(data) != "test media" {
		t.Fatal("publication failed", err)
	}
	reloaded, err := New(s.Dir, s.Library)
	if err != nil || reloaded.Status().Jobs[0].State != "succeeded" {
		t.Fatal("queue not durable", err)
	}
	if _, err := s.Submit(Input{URL: j.URL}); err == nil {
		t.Fatal("queued duplicate")
	}
}
func TestFailureAndMissingShareNeverPublish(t *testing.T) {
	for _, lost := range []bool{false, true} {
		t.Run(strconv.FormatBool(lost), func(t *testing.T) {
			s := fixture(t)
			os.WriteFile(filepath.Join(s.Dir, "admit-until"), []byte("-1"), 0644)
			s.Execute = func(_ context.Context, _ Input, stage string) error {
				os.WriteFile(filepath.Join(stage, "clip.m4a"), []byte("partial"), 0600)
				if lost {
					s.MountOK = func() bool { return false }
					return nil
				}
				return errors.New("synthetic download failure")
			}
			s.Submit(Input{URL: "https://youtu.be/12345678901"})
			s.step(context.Background())
			if s.Status().Jobs[0].State != "failed" {
				t.Fatal("did not fail")
			}
			files, _ := os.ReadDir(s.Library)
			if len(files) != 0 {
				t.Fatal("published failed input")
			}
		})
	}
}
func TestSymbolicLinkOutputIsRefused(t *testing.T) {
	s := fixture(t)
	os.WriteFile(filepath.Join(s.Dir, "admit-until"), []byte("-1"), 0644)
	outside := t.TempDir()
	os.Symlink(outside, filepath.Join(s.Library, "audio"))
	s.Execute = func(_ context.Context, _ Input, stage string) error {
		return os.WriteFile(filepath.Join(stage, "clip.m4a"), []byte("test"), 0600)
	}
	s.Submit(Input{URL: "https://youtu.be/12345678901"})
	s.step(context.Background())
	if !strings.Contains(s.Status().Jobs[0].Error, "symbolic link") {
		t.Fatal(s.Status())
	}
	files, _ := os.ReadDir(outside)
	if len(files) > 0 {
		t.Fatal("wrote outside library")
	}
}

func TestEventsSayWhenTheQueueChanges(t *testing.T) {
	s := fixture(t)
	server := httptest.NewServer(s.Handler())
	defer server.Close()
	response, err := http.Get(server.URL + "/events")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	lines := bufio.NewReader(response.Body)
	if line, _ := lines.ReadString('\n'); line != ": open\n" {
		t.Fatalf("first line = %q", line)
	}
	lines.ReadString('\n')

	if _, err := s.Submit(Input{URL: "https://www.youtube.com/watch?v=12345678901"}); err != nil {
		t.Fatal(err)
	}
	if line, _ := lines.ReadString('\n'); line != "event: jobs\n" {
		t.Fatalf("line = %q", line)
	}
}
