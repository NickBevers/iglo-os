package jellyfin

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/coder/websocket"
)

const (
	// watchRetry is how long Watch waits before it connects again.
	watchRetry = 15 * time.Second
	// sessionsPeriod is how often Jellyfin sends the sessions unasked. It also
	// sends them at once when playback starts or stops.
	sessionsPeriod = "0,60000"
)

// socketMessage is a message on the WebSocket of Jellyfin.
type socketMessage struct {
	MessageType string          `json:"MessageType"`
	Data        json.RawMessage `json:"Data,omitempty"`
}

// Watch keeps the WebSocket of Jellyfin open, and calls changed when
// playback starts or stops, or the library changes. Then the dashboard asks
// at once instead of on its next poll. It connects again after a drop, and
// when the settings change.
func (s *Service) Watch(ctx context.Context, changed func()) {
	for ctx.Err() == nil {
		s.mu.Lock()
		settings := s.settings
		s.mu.Unlock()
		if settings.URL != "" && settings.APIKey != "" {
			s.watchOnce(ctx, settings, changed)
		}
		select {
		case <-ctx.Done():
			return
		case <-s.reconnect:
		case <-time.After(watchRetry):
		}
	}
}

func (s *Service) watchOnce(ctx context.Context, settings Settings, changed func()) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	// A new URL or key ends this connection.
	go func() {
		select {
		case <-ctx.Done():
		case <-s.reconnect:
			cancel()
			// Leave the signal for the loop, so it connects at once.
			select {
			case s.reconnect <- struct{}{}:
			default:
			}
		}
	}()

	address := "ws" + strings.TrimPrefix(strings.TrimRight(settings.URL, "/"), "http") + "/socket"
	conn, _, err := websocket.Dial(ctx, address, &websocket.DialOptions{
		HTTPHeader: http.Header{"X-Emby-Token": {settings.APIKey}},
	})
	if err != nil {
		return
	}
	defer conn.CloseNow()
	// The sessions of a busy server are larger than the default limit.
	conn.SetReadLimit(4 << 20)
	send := func(message socketMessage) error {
		data, _ := json.Marshal(message)
		return conn.Write(ctx, websocket.MessageText, data)
	}
	start, _ := json.Marshal(sessionsPeriod)
	if send(socketMessage{MessageType: "SessionsStart", Data: start}) != nil {
		return
	}
	// The dashboard may have missed something while the socket was closed.
	changed()

	keepAlive := make(chan time.Duration, 1)
	go func() {
		var ticker *time.Ticker
		tick := make(<-chan time.Time)
		for {
			select {
			case <-ctx.Done():
				if ticker != nil {
					ticker.Stop()
				}
				return
			case every := <-keepAlive:
				if ticker != nil {
					ticker.Stop()
				}
				ticker = time.NewTicker(every)
				tick = ticker.C
			case <-tick:
				if send(socketMessage{MessageType: "KeepAlive"}) != nil {
					cancel()
				}
			}
		}
	}()

	for {
		_, data, err := conn.Read(ctx)
		if err != nil {
			return
		}
		var message socketMessage
		if json.Unmarshal(data, &message) != nil {
			continue
		}
		switch message.MessageType {
		case "Sessions":
			changed()
		case "LibraryChanged":
			s.mu.Lock()
			s.libraryAt = time.Time{}
			s.mu.Unlock()
			changed()
		case "ForceKeepAlive":
			// Jellyfin closes a socket that stays quiet for this many seconds.
			var seconds int
			if json.Unmarshal(message.Data, &seconds) == nil && seconds > 1 {
				select {
				case keepAlive <- time.Duration(seconds) * time.Second / 2:
				default:
				}
			}
		}
	}
}
