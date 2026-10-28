package notify

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Ntfy publishes to a topic of an ntfy server, like ntfy.sh or your own. The
// phone app subscribes to the topic. Checked against ntfy v2.34.0 on 28
// October 2026: a POST to /<topic> with the text as body, and an access token
// as "Authorization: Bearer tk_...".
type Ntfy struct {
	// Server is the base URL, without a slash at the end.
	Server string
	Topic  string
	// Token is an access token of the server. Empty on a server without logins.
	Token string
	HTTP  *http.Client
}

func (n Ntfy) Configured() bool {
	return n.Server != "" && n.Topic != ""
}

func (n Ntfy) Send(ctx context.Context, text string) error {
	if !n.Configured() {
		return nil
	}
	client := &http.Client{Timeout: 15 * time.Second}
	if n.HTTP != nil {
		*client = *n.HTTP
	}
	// Credentials belong only to this endpoint, including on same-host redirects.
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }

	request, err := http.NewRequestWithContext(ctx, http.MethodPost, n.Server+"/"+n.Topic, strings.NewReader(text))
	if err != nil {
		return err
	}
	request.Header.Set("Title", "Iglo-OS")
	if n.Token != "" {
		request.Header.Set("Authorization", "Bearer "+n.Token)
	}

	response, err := client.Do(request)
	if err != nil {
		return fmt.Errorf("ntfy: could not reach %s", n.Server)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		// ntfy answers {"code":40301,"http":403,"error":"forbidden",...}.
		data, _ := io.ReadAll(io.LimitReader(response.Body, 2048))
		return fmt.Errorf("ntfy: %s: %s", response.Status, strings.TrimSpace(string(data)))
	}

	return nil
}

// Sender is one way to reach the user.
type Sender interface {
	Configured() bool
	Send(ctx context.Context, text string) error
}

// All sends to every sender that is set up, and returns all errors.
type All []Sender

func (a All) Configured() bool {
	for _, sender := range a {
		if sender.Configured() {
			return true
		}
	}
	return false
}

func (a All) Send(ctx context.Context, text string) error {
	var errs []error
	for _, sender := range a {
		if sender.Configured() {
			errs = append(errs, sender.Send(ctx, text))
		}
	}
	return errors.Join(errs...)
}
