package agent

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

func TestInboxBridgeRejectsInvalidInputsBeforeGuestCall(t *testing.T) {
	mux := http.NewServeMux()
	RegisterInbox(mux)
	for _, req := range []struct{ method, path, body string }{{"POST", "/v1/inbox/180", `{"path":"../secret.mp4","profile":"preserve"}`}, {"PUT", "/v1/inbox/180/settings", `{"profile":"preserve"}`}, {"POST", "/v1/inbox/180/retry/invalid", ""}} {
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest(req.method, req.path, strings.NewReader(req.body)))
		if w.Code != 400 {
			t.Fatal(w.Code, w.Body.String())
		}
	}
}
func TestOperationCompletionInvalidatesContainerConfigOnce(t *testing.T) {
	i := &AppInstaller{Dir: t.TempDir()}
	state := AppInstallStatus{App: InboxApp, Action: ActionUpdate, State: UpgradeSucceeded, StartedAt: time.Now()}
	data, _ := json.Marshal(state)
	if err := os.WriteFile(i.statusPath(), data, 0600); err != nil {
		t.Fatal(err)
	}
	configs.Lock()
	configs.entries = map[int]cachedConfig{180: {text: "old", at: time.Now()}}
	configs.Unlock()
	t.Cleanup(func() { configs.Lock(); configs.entries = nil; configs.Unlock() })
	i.Status()
	configs.Lock()
	if len(configs.entries) != 0 {
		t.Error("stale config survived the update")
	}
	configs.entries = map[int]cachedConfig{180: {text: "new", at: time.Now()}}
	configs.Unlock()
	i.Status()
	configs.Lock()
	defer configs.Unlock()
	if len(configs.entries) != 1 {
		t.Fatal("unchanged completed operation cleared cache again")
	}
}
