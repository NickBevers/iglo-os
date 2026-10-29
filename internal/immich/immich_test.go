package immich

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func defaultPhotoConfig(t *testing.T) map[string]any {
	t.Helper()
	var config map[string]any
	if err := json.Unmarshal([]byte(`{"job":{"videoConversion":{"concurrency":2},"faceDetection":{"concurrency":2},"smartSearch":{"concurrency":2},"ocr":{"concurrency":1}},"ffmpeg":{"threads":0,"transcode":"optimal","targetVideoCodec":"h264"},"machineLearning":{"facialRecognition":{"enabled":true,"modelName":"buffalo_l"},"clip":{"enabled":true}},"unrelated":{"keep":true}}`), &config); err != nil {
		t.Fatal(err)
	}
	return config
}

func TestPhotoDefaultsPreserveChoicesOutsideManagedDefaults(t *testing.T) {
	for _, allowed := range []bool{false, true} {
		config := defaultPhotoConfig(t)
		if err := photoDefaults(config, allowed); err != nil {
			t.Fatal(err)
		}
		for _, name := range []string{"videoConversion", "faceDetection", "smartSearch", "ocr"} {
			if config["job"].(map[string]any)[name].(map[string]any)["concurrency"] != 1 {
				t.Fatal(name)
			}
		}
		encoding := config["ffmpeg"].(map[string]any)
		if encoding["threads"] != 2 || encoding["transcode"] != "required" || encoding["targetVideoCodec"] != "h264" {
			t.Fatal(encoding)
		}
		learning := config["machineLearning"].(map[string]any)
		faces := learning["facialRecognition"].(map[string]any)
		if faces["enabled"] != allowed || faces["modelName"] != "buffalo_l" || learning["clip"].(map[string]any)["enabled"] != true {
			t.Fatal(learning)
		}
		if config["unrelated"].(map[string]any)["keep"] != true {
			t.Fatal("unrelated setting changed")
		}
	}
}

func TestPhotoDefaultsRefuseUnknownConfiguration(t *testing.T) {
	config := defaultPhotoConfig(t)
	delete(config, "ffmpeg")
	if err := photoDefaults(config, false); err == nil {
		t.Fatal("guessed unknown config")
	}
}

func TestExistingImmichAdminIsNeverReconfigured(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls++; w.Write([]byte(`{"isInitialized":true}`)) }))
	defer server.Close()
	client := Client{HTTP: server.Client(), Server: server.URL}
	if _, err := client.CreateAdmin(context.Background(), Admin{}); err != ErrHasAdmin || calls != 1 {
		t.Fatal(err, calls)
	}
}

func TestImmichErrorsNeverEchoCredentialedBodies(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Error(w, "sensitive-upstream-value", 400) }))
	defer server.Close()
	client := Client{HTTP: server.Client(), Server: server.URL}
	err := client.applyDefaults(context.Background(), "admin-secret", false)
	if err == nil || strings.Contains(err.Error(), "sensitive-upstream-value") || strings.Contains(err.Error(), "admin-secret") {
		t.Fatal(err)
	}
}

// A new Immich gets its admin and a key that may only read and pause the
// queues, after it starts answering.
func TestNewImmichGetsAdminAndQueueKey(t *testing.T) {
	var calls []string
	starting := 2
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, r.Method+" "+r.URL.Path)
		switch r.Method + " " + r.URL.Path {
		case "GET /api/server/config":
			if starting > 0 {
				starting--
				http.Error(w, "starting", http.StatusBadGateway)
				return
			}
			w.Write([]byte(`{"isInitialized":false}`))
		case "POST /api/auth/admin-sign-up":
			var body map[string]string
			json.NewDecoder(r.Body).Decode(&body)
			if body["email"] != "nick@example.org" || body["name"] != "Nick" || body["password"] != "a long password" {
				t.Errorf("sign-up = %v", body)
			}
			w.WriteHeader(http.StatusCreated)
		case "POST /api/auth/login":
			w.Write([]byte(`{"accessToken":"session"}`))
		case "GET /api/admin/config", "PUT /api/admin/config":
			if r.Header.Get("Authorization") != "Bearer session" {
				t.Error("config without the admin session")
			}
			w.Write([]byte(`{"job":{"videoConversion":{},"faceDetection":{},"smartSearch":{},"ocr":{}},"ffmpeg":{},"machineLearning":{"facialRecognition":{"enabled":true}}}`))
		case "POST /api/api-keys":
			var body struct {
				Permissions []string `json:"permissions"`
			}
			json.NewDecoder(r.Body).Decode(&body)
			if strings.Join(body.Permissions, ",") != "queue.read,queue.update" || r.Header.Get("Authorization") != "Bearer session" {
				t.Errorf("key request = %v", body)
			}
			w.Write([]byte(`{"secret":"queue-key"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	client := Client{HTTP: server.Client(), Server: server.URL, Wait: time.Second, Poll: time.Millisecond}
	admin := Admin{Email: "nick@example.org", Name: "Nick", Password: "a long password"}
	token, err := client.CreateAdmin(context.Background(), admin)
	if err != nil {
		t.Fatal(err)
	}
	key, err := client.QueueKey(context.Background(), token)
	if err != nil || key != "queue-key" {
		t.Fatal(key, err)
	}
	if want := "GET /api/server/config GET /api/server/config GET /api/server/config POST /api/auth/admin-sign-up POST /api/auth/login GET /api/admin/config PUT /api/admin/config POST /api/api-keys"; strings.Join(calls, " ") != want {
		t.Fatalf("calls = %v", calls)
	}
}

func TestAdminValidation(t *testing.T) {
	good := Admin{Email: "nick@example.org", Name: "Nick", Password: "twelve chars"}
	if err := good.Validate(); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []Admin{
		{Email: "nick", Name: "Nick", Password: "twelve chars"},
		{Email: "nick@example.org", Name: "", Password: "twelve chars"},
		{Email: "nick@example.org", Name: "Nick\n", Password: "twelve chars"},
		{Email: "nick@example.org", Name: "Nick", Password: "too short"},
		{Email: "nick@example.org", Name: "Nick", Password: "twelve chars\n"},
	} {
		if err := bad.Validate(); !errors.Is(err, ErrInvalidAdmin) {
			t.Errorf("%+v: %v", bad, err)
		}
	}
}
