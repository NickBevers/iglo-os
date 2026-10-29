// Package immich makes the first admin of a new Immich and a key for
// Background work, for the guided setup and for a Photos install from the
// Apps page.
//
// Checked against Immich v3.3.5 on 29 October 2026: server.controller.ts
// (GET server/config with isInitialized), auth.controller.ts and auth.dto.ts
// (POST auth/admin-sign-up with email, password and name, POST auth/login
// with email and password, answering accessToken), api-key.controller.ts and
// api-key.dto.ts (POST api-keys with name and permissions, answering secret),
// enum.ts (queue.read and queue.update) and queue.controller.ts (GET queues
// needs queue.read, PUT queues/{name} needs queue.update, both admin only).
package immich

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"
	"unicode"
)

// ErrHasAdmin means Immich was set up before, so its admin is someone else's
// to give.
var ErrHasAdmin = errors.New("Immich already has an admin")

// ErrInvalidAdmin means the answers for the first admin can't be used.
var ErrInvalidAdmin = errors.New("invalid Immich admin")

// Admin is the first admin that Iglo-OS makes in a new Immich.
type Admin struct {
	Email    string `json:"email"`
	Name     string `json:"name"`
	Password string `json:"password"`
	// FaceModels turns face grouping on. Its buffalo_l weights come with
	// non-commercial research terms, so it is off unless Nick opts in.
	FaceModels bool `json:"faceModels"`
}

var (
	emailPattern = regexp.MustCompile(`^[^@\s]+@[^@\s]+\.[^@\s]+$`)
	namePattern  = regexp.MustCompile(`^[A-Za-z0-9._ -]{1,32}$`)
)

// Validate checks the admin like the setup does: Immich logs in by email,
// and the password is as long as the other admin passwords of Iglo-OS.
func (a Admin) Validate() error {
	invalid := func(message string) error { return fmt.Errorf("%w: %s", ErrInvalidAdmin, message) }
	switch {
	case len(a.Email) > 254 || !emailPattern.MatchString(a.Email):
		return invalid("enter the email address to log in to Immich with")
	case !namePattern.MatchString(a.Name):
		return invalid("the name can have up to 32 letters, digits, spaces, dots, dashes and underscores")
	case len([]rune(a.Password)) < 12 || len(a.Password) > 128 || strings.ContainsFunc(a.Password, unicode.IsControl):
		return invalid("the password needs 12 to 128 characters, without line breaks")
	}
	return nil
}

// Client calls the API of one Immich, like http://192.168.1.170:2283.
type Client struct {
	HTTP   *http.Client
	Server string
	// Wait is how long CreateAdmin waits for a new Immich to answer, and Poll
	// how often it asks. Immich takes a while to start after its install.
	Wait, Poll time.Duration
}

func (c Client) call(ctx context.Context, method, path, token string, body, out any) error {
	var reader io.Reader
	if body != nil {
		data, _ := json.Marshal(body)
		reader = bytes.NewReader(data)
	}
	request, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(c.Server, "/")+"/api"+path, reader)
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/json")
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	response, err := c.HTTP.Do(request)
	if err != nil {
		return fmt.Errorf("Immich: %w", err)
	}
	defer response.Body.Close()
	// The body is never shown: an error of Immich can repeat what it was sent.
	if response.StatusCode >= 300 {
		return fmt.Errorf("Immich %s: %s", path, response.Status)
	}
	if out != nil {
		return json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(out)
	}
	return nil
}

// CreateAdmin makes the first admin of a new Immich, sets the photo defaults
// and returns a session token of that admin. The token stays in memory.
// An Immich that has an admin already is left as it is: ErrHasAdmin.
func (c Client) CreateAdmin(ctx context.Context, admin Admin) (string, error) {
	wait, poll := c.Wait, c.Poll
	if wait == 0 {
		wait = 5 * time.Minute
	}
	if poll == 0 {
		poll = 5 * time.Second
	}
	deadline := time.Now().Add(wait)
	var config struct {
		IsInitialized bool `json:"isInitialized"`
	}
	for {
		err := c.call(ctx, http.MethodGet, "/server/config", "", nil, &config)
		if err == nil {
			break
		}
		if time.Now().After(deadline) {
			return "", err
		}
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(poll):
		}
	}
	if config.IsInitialized {
		return "", ErrHasAdmin
	}

	signUp := map[string]string{"email": admin.Email, "password": admin.Password, "name": admin.Name}
	if err := c.call(ctx, http.MethodPost, "/auth/admin-sign-up", "", signUp, nil); err != nil {
		return "", err
	}
	var login struct {
		AccessToken string `json:"accessToken"`
	}
	credentials := map[string]string{"email": admin.Email, "password": admin.Password}
	if err := c.call(ctx, http.MethodPost, "/auth/login", "", credentials, &login); err != nil {
		return "", err
	}
	if err := c.applyDefaults(ctx, login.AccessToken, admin.FaceModels); err != nil {
		return "", err
	}
	return login.AccessToken, nil
}

// QueueKey makes an API key for Background work, which may only read and
// pause the queues.
func (c Client) QueueKey(ctx context.Context, token string) (string, error) {
	var key struct {
		Secret string `json:"secret"`
	}
	request := map[string]any{"name": "Iglo-OS background work", "permissions": []string{"queue.read", "queue.update"}}
	if err := c.call(ctx, http.MethodPost, "/api-keys", token, request, &key); err != nil {
		return "", err
	}
	if key.Secret == "" {
		return "", errors.New("Immich made no API key")
	}
	return key.Secret, nil
}

// applyDefaults only runs while making the first admin. Existing libraries
// keep their choices. Checked against Immich v3.3.5 config-admin.controller.ts,
// config.dto.ts and enum.ts on 28 October 2026. The admin bearer token stays
// in memory; the long-lived key still gets queue.read and queue.update only.
func (c Client) applyDefaults(ctx context.Context, token string, faces bool) error {
	var config map[string]any
	if err := c.call(ctx, http.MethodGet, "/admin/config", token, nil, &config); err != nil {
		return err
	}
	if err := photoDefaults(config, faces); err != nil {
		return err
	}
	return c.call(ctx, http.MethodPut, "/admin/config", token, config, nil)
}

// photoDefaults limits video conversion and the analysis jobs to one at a
// time, converts only the videos that need it, and turns face grouping on
// only when faces is set. It changes nothing else.
func photoDefaults(config map[string]any, faces bool) error {
	section := func(object map[string]any, key string) (map[string]any, error) {
		value, ok := object[key].(map[string]any)
		if !ok {
			return nil, fmt.Errorf("Immich configuration has no %s section", key)
		}
		return value, nil
	}
	jobs, err := section(config, "job")
	if err != nil {
		return err
	}
	for _, name := range []string{"videoConversion", "faceDetection", "smartSearch", "ocr"} {
		job, err := section(jobs, name)
		if err != nil {
			return err
		}
		job["concurrency"] = 1
	}
	encoding, err := section(config, "ffmpeg")
	if err != nil {
		return err
	}
	encoding["threads"], encoding["transcode"] = 2, "required"
	learning, err := section(config, "machineLearning")
	if err != nil {
		return err
	}
	recognition, err := section(learning, "facialRecognition")
	if err != nil {
		return err
	}
	recognition["enabled"] = faces
	return nil
}
