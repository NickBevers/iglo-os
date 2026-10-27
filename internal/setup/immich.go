package setup

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// errImmichHasAdmin means Immich was set up before, so its admin is someone
// else's to give.
var errImmichHasAdmin = errors.New("Immich already has an admin")

// immichCall calls the API of Immich at server, like http://192.168.1.170:2283.
func (s *Service) immichCall(ctx context.Context, server, method, path, token string, body, out any) error {
	var reader io.Reader
	if body != nil {
		data, _ := json.Marshal(body)
		reader = bytes.NewReader(data)
	}
	request, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(server, "/")+"/api"+path, reader)
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/json")
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	response, err := s.http().Do(request)
	if err != nil {
		return fmt.Errorf("Immich: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode >= 300 {
		message, _ := io.ReadAll(io.LimitReader(response.Body, 512))
		return fmt.Errorf("Immich %s: %s %s", path, response.Status, strings.TrimSpace(string(message)))
	}
	if out != nil {
		return json.NewDecoder(response.Body).Decode(out)
	}
	return nil
}

// immichKey creates the first admin of a new Immich and returns an API key
// for Background work, which may only read and pause the queues.
func (s *Service) immichKey(ctx context.Context, server string, admin Admin) (string, error) {
	// Immich takes a while to start after its install.
	deadline := time.Now().Add(5 * time.Minute)
	var config struct {
		IsInitialized bool `json:"isInitialized"`
	}
	for {
		err := s.immichCall(ctx, server, http.MethodGet, "/server/config", "", nil, &config)
		if err == nil {
			break
		}
		if time.Now().After(deadline) {
			return "", err
		}
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(5 * time.Second):
		}
	}
	if config.IsInitialized {
		return "", errImmichHasAdmin
	}

	credentials := map[string]string{"email": admin.Email, "password": admin.Password}
	signUp := map[string]string{"email": admin.Email, "password": admin.Password, "name": admin.Username}
	if err := s.immichCall(ctx, server, http.MethodPost, "/auth/admin-sign-up", "", signUp, nil); err != nil {
		return "", err
	}
	var login struct {
		AccessToken string `json:"accessToken"`
	}
	if err := s.immichCall(ctx, server, http.MethodPost, "/auth/login", "", credentials, &login); err != nil {
		return "", err
	}
	var key struct {
		Secret string `json:"secret"`
	}
	request := map[string]any{"name": "Iglo-OS background work", "permissions": []string{"queue.read", "queue.update"}}
	if err := s.immichCall(ctx, server, http.MethodPost, "/api-keys", login.AccessToken, request, &key); err != nil {
		return "", err
	}
	if key.Secret == "" {
		return "", errors.New("Immich made no API key")
	}
	return key.Secret, nil
}
