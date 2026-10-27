package agent

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// The agent streams what changes on the host to the manager, with
// Server-Sent Events over its socket. An event only names a topic; the
// manager asks for the details the normal way.

const (
	// eventsKeepAlive is how often an idle stream sends a comment.
	eventsKeepAlive = 25 * time.Second
	// installCheck is how often the stream looks at the files of an install.
	// It only stats them, which costs nothing.
	installCheck = time.Second
	// guestStreamLimit ends a relayed stream from a container after this long,
	// and the manager opens a new one. A curl that lost its pct exec then
	// can't stay behind in the container for long.
	guestStreamLimit = 10 * time.Minute
)

// fingerprint changes when the status, log or exit code of an install does.
func (i *AppInstaller) fingerprint() string {
	var parts []string
	for _, path := range []string{i.statusPath(), i.logPath(), i.exitPath()} {
		if info, err := os.Stat(path); err == nil {
			parts = append(parts, fmt.Sprint(info.Size(), info.ModTime().UnixNano()))
		} else {
			parts = append(parts, "-")
		}
	}
	return strings.Join(parts, "/")
}

// eventStream starts an SSE answer and returns a function that writes one
// event, or a keep-alive comment for an empty topic. It reports false once
// the manager is gone.
func eventStream(w http.ResponseWriter) func(topic string) bool {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	controller := http.NewResponseController(w)
	return func(topic string) bool {
		if topic == "" {
			fmt.Fprint(w, ": keep-alive\n\n")
		} else {
			fmt.Fprintf(w, "data: %s\n\n", topic)
		}
		return controller.Flush() == nil
	}
}

// RegisterEvents adds the event stream of the host: "apps" when an install,
// update or removal moves on.
func RegisterEvents(mux *http.ServeMux, installer *AppInstaller) {
	mux.HandleFunc("GET /v1/events", func(w http.ResponseWriter, r *http.Request) {
		// The first look comes before the stream opens, so a change right
		// after the opening comment is never missed.
		last := installer.fingerprint()
		send := eventStream(w)
		if !send("") {
			return
		}
		check := time.NewTicker(installCheck)
		defer check.Stop()
		keepAlive := time.NewTicker(eventsKeepAlive)
		defer keepAlive.Stop()
		for {
			select {
			case <-r.Context().Done():
				return
			case <-keepAlive.C:
				if !send("") {
					return
				}
			case <-check.C:
				if current := installer.fingerprint(); current != last {
					last = current
					if !send("apps") {
						return
					}
				}
			}
		}
	})
}

// relayGuestStream runs a streaming curl in a container, and passes each
// event that pick names on as topic. It never passes the data of an event.
func relayGuestStream(w http.ResponseWriter, r *http.Request, vmid int, curl []string, topic string, pick func(name string) bool) {
	ctx, cancel := context.WithTimeout(r.Context(), guestStreamLimit)
	defer cancel()
	args := append([]string{"exec", strconv.Itoa(vmid), "--", "curl", "--silent", "--no-buffer", "--max-time", strconv.Itoa(int(guestStreamLimit.Seconds()))}, curl...)
	cmd := exec.CommandContext(ctx, "pct", args...)
	output, err := cmd.StdoutPipe()
	if err != nil {
		http.Error(w, "could not follow the container", http.StatusBadGateway)
		return
	}
	if err := cmd.Start(); err != nil {
		http.Error(w, "could not follow the container", http.StatusBadGateway)
		return
	}
	defer cmd.Wait()
	defer cancel()

	send := eventStream(w)
	if !send("") {
		return
	}
	events := make(chan struct{}, 1)
	go func() {
		defer close(events)
		lines := bufio.NewScanner(output)
		// Wolf puts a whole session on one data line.
		lines.Buffer(make([]byte, 64<<10), 1<<20)
		for lines.Scan() {
			name, ok := strings.CutPrefix(lines.Text(), "event: ")
			if !ok || !pick(strings.TrimSpace(name)) {
				continue
			}
			select {
			case events <- struct{}{}:
			default:
			}
		}
	}()
	keepAlive := time.NewTicker(eventsKeepAlive)
	defer keepAlive.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-keepAlive.C:
			if !send("") {
				return
			}
		case _, open := <-events:
			if !open || !send(topic) {
				return
			}
		}
	}
}

// wolfSessionEvents are the events of Wolf that change its session list. Wolf
// names an event by its C++ type, like wolf::core::events::StopStreamEvent.
var wolfSessionEvents = map[string]bool{"StreamSession": true, "StopStreamEvent": true, "PauseStreamEvent": true, "ResumeStreamEvent": true}

func wolfSessionEvent(name string) bool {
	return wolfSessionEvents[name[strings.LastIndex(name, ":")+1:]]
}

// follow reads an event stream of the agent and calls onEvent for each
// topic, until ctx ends or the stream drops. It calls onEvent with an empty
// topic once the stream is open.
func (c *Client) follow(ctx context.Context, path string, onEvent func(topic string)) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://agent"+path, nil)
	if err != nil {
		return err
	}
	response, err := c.stream.Do(request)
	if err != nil {
		return fmt.Errorf("host agent: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		message, _ := io.ReadAll(io.LimitReader(response.Body, 512))
		return fmt.Errorf("host agent %s: %s %s", path, response.Status, strings.TrimSpace(string(message)))
	}
	// An empty topic tells the caller that the stream is open.
	onEvent("")
	lines := bufio.NewScanner(response.Body)
	for lines.Scan() {
		if topic, ok := strings.CutPrefix(lines.Text(), "data: "); ok && topic != "" {
			onEvent(topic)
		}
	}
	if err := lines.Err(); err != nil {
		return err
	}
	return errors.New("the host agent closed the event stream")
}

// Events follows the event stream of the host.
func (c *Client) Events(ctx context.Context, onEvent func(topic string)) error {
	return c.follow(ctx, "/v1/events", onEvent)
}

// GamingEvents follows the session events of Wolf in the gaming container.
func (c *Client) GamingEvents(ctx context.Context, vmid int, onEvent func(topic string)) error {
	return c.follow(ctx, "/v1/gaming/"+strconv.Itoa(vmid)+"/events", onEvent)
}

// MusicEvents follows the job events of the Music worker.
func (c *Client) MusicEvents(ctx context.Context, vmid int, onEvent func(topic string)) error {
	return c.follow(ctx, "/v1/music/"+strconv.Itoa(vmid)+"/events", onEvent)
}
