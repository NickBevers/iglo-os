package jellyfin

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/coder/websocket"
)

func TestWatchFollowsTheSocket(t *testing.T) {
	received := make(chan socketMessage, 10)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/socket" || r.Header.Get("X-Emby-Token") != "key" {
			http.Error(w, "no", http.StatusUnauthorized)
			return
		}
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer conn.CloseNow()
		ctx := r.Context()
		write := func(message string) { conn.Write(ctx, websocket.MessageText, []byte(message)) }
		write(`{"MessageType":"ForceKeepAlive","Data":2}`)
		write(`{"MessageType":"Sessions","Data":[]}`)
		for {
			_, data, err := conn.Read(ctx)
			if err != nil {
				return
			}
			var message socketMessage
			json.Unmarshal(data, &message)
			received <- message
		}
	}))
	defer server.Close()

	service, err := NewService(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	service.settings = Settings{URL: server.URL, APIKey: "key"}
	changes := make(chan struct{}, 10)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go service.Watch(ctx, func() { changes <- struct{}{} })

	expect := func(what string, ok func() bool) {
		t.Helper()
		deadline := time.After(5 * time.Second)
		for !ok() {
			select {
			case <-deadline:
				t.Fatalf("no %s", what)
			case <-time.After(10 * time.Millisecond):
			}
		}
	}
	var first socketMessage
	expect("SessionsStart", func() bool {
		select {
		case first = <-received:
			return true
		default:
			return false
		}
	})
	if first.MessageType != "SessionsStart" || string(first.Data) != `"0,60000"` {
		t.Fatalf("first message = %+v", first)
	}
	// One change when connected, one for the sessions.
	count := 0
	expect("two changes", func() bool {
		for {
			select {
			case <-changes:
				count++
			default:
				return count >= 2
			}
		}
	})
	// ForceKeepAlive 2 asks for a keep-alive every second.
	expect("KeepAlive", func() bool {
		select {
		case message := <-received:
			return message.MessageType == "KeepAlive"
		default:
			return false
		}
	})
}
