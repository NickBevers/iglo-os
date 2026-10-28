package setup

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
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
	service := New(Options{})
	if _, err := service.immichKey(context.Background(), server.URL, Admin{}, false); err != errImmichHasAdmin || calls != 1 {
		t.Fatal(err, calls)
	}
}

func TestImmichErrorsNeverEchoCredentialedBodies(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Error(w, "sensitive-upstream-value", 400) }))
	defer server.Close()
	service := New(Options{})
	err := service.immichDefaults(context.Background(), server.URL, "admin-secret", false)
	if err == nil || strings.Contains(err.Error(), "sensitive-upstream-value") || strings.Contains(err.Error(), "admin-secret") {
		t.Fatal(err)
	}
}
