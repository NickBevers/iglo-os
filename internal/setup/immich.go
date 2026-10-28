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
		return fmt.Errorf("Immich %s: %s", path, response.Status)
	}
	if out != nil {
		return json.NewDecoder(response.Body).Decode(out)
	}
	return nil
}

// immichKey creates the first admin of a new Immich and returns an API key
// for Background work, which may only read and pause the queues.
func (s *Service) immichKey(ctx context.Context, server string, admin Admin, allowRestrictedFaces bool) (string, error) {
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
	if err := s.immichDefaults(ctx, server, login.AccessToken, allowRestrictedFaces); err != nil {
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

// immichDefaults only runs while making the first admin. Existing libraries
// keep their choices. Checked against Immich v3.3.5 config-admin.controller.ts,
// config.dto.ts and enum.ts on 28 October 2026. The admin bearer token stays
// in memory; the long-lived key still gets queue.read and queue.update only.
func (s *Service) immichDefaults(ctx context.Context, server, token string, allowRestrictedFaces bool) error {
	var config map[string]any
	if err := s.immichCall(ctx, server, http.MethodGet, "/admin/config", token, nil, &config); err != nil {
		return err
	}
	if err := photoDefaults(config, allowRestrictedFaces); err != nil {
		return err
	}
	return s.immichCall(ctx, server, http.MethodPut, "/admin/config", token, config, nil)
}

func photoDefaults(config map[string]any, allowRestrictedFaces bool) error {
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
	faces, err := section(learning, "facialRecognition")
	if err != nil {
		return err
	}
	faces["enabled"] = allowRestrictedFaces
	return nil
}
