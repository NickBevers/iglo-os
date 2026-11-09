package agent

import (
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInboxShrinkEventsStripData(t *testing.T) {
	bin := t.TempDir()
	script := "#!/bin/sh\nprintf 'event: jobs\\ndata: SECRET\\n\\nevent: library\\ndata: SECRET\\n\\nevent: shrink\\ndata: SECRET\\n\\nevent: unknown\\ndata: SECRET\\n\\n'\n"
	if err := os.WriteFile(filepath.Join(bin, "pct"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/events", nil)
	relayGuestStream(w, r, 104, []string{"http://127.0.0.1:8789/events"}, "inbox", func(name string) bool { return name == "jobs" || name == "library" || name == "shrink" }, inboxEventTopic)
	body := w.Body.String()
	for _, name := range []string{"inbox", "library", "shrink"} {
		if !strings.Contains(body, "data: "+name+"\n") {
			t.Fatal(body)
		}
	}
	if strings.Contains(body, "SECRET") || strings.Contains(body, "unknown") {
		t.Fatal("event data leaked", body)
	}
}
