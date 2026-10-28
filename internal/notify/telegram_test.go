package notify

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSendPostsMessageToChat(t *testing.T) {
	var got map[string]string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/botTOKEN/sendMessage" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		json.NewDecoder(r.Body).Decode(&got)
		w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()

	telegram := Telegram{BotToken: "TOKEN", ChatID: "42", BaseURL: server.URL}
	if err := telegram.Send(context.Background(), "hello"); err != nil {
		t.Fatal(err)
	}

	if got["chat_id"] != "42" || got["text"] != "hello" {
		t.Fatalf("unexpected body: %v", got)
	}
}

func TestSendReturnsTelegramErrorWithoutToken(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"ok":false,"description":"Bad Request: chat not found"}`))
	}))
	defer server.Close()

	err := Telegram{BotToken: "SECRET", ChatID: "1", BaseURL: server.URL}.Send(context.Background(), "hi")
	if err == nil || !strings.Contains(err.Error(), "chat not found") {
		t.Fatalf("expected Telegram error, got %v", err)
	}
	if strings.Contains(err.Error(), "SECRET") {
		t.Fatal("error leaks the bot token")
	}
}

func TestSendSkipsWhenNotConfigured(t *testing.T) {
	if err := (Telegram{}).Send(context.Background(), "hi"); err != nil {
		t.Fatal(err)
	}
}

func TestNtfyPublishesToTopic(t *testing.T) {
	var path, body, auth, title string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data, _ := io.ReadAll(r.Body)
		path, body, auth, title = r.URL.Path, string(data), r.Header.Get("Authorization"), r.Header.Get("Title")
		w.Write([]byte(`{"id":"x"}`))
	}))
	defer server.Close()

	ntfy := Ntfy{Server: server.URL, Topic: "iglo-abc", Token: "tk_AgQdq7mVBoFD37zQVN29RhuMzNIz2"}
	if err := ntfy.Send(context.Background(), "✅ jellyfin: updated"); err != nil {
		t.Fatal(err)
	}
	if path != "/iglo-abc" || body != "✅ jellyfin: updated" || auth != "Bearer tk_AgQdq7mVBoFD37zQVN29RhuMzNIz2" || title != "Iglo-OS" {
		t.Fatalf("path %q, body %q, auth %q, title %q", path, body, auth, title)
	}
}

func TestAllSendsToEverySender(t *testing.T) {
	forbidden := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte(`{"code":40301,"http":403,"error":"forbidden"}`))
	}))
	defer forbidden.Close()
	telegramCalls := 0
	telegram := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		telegramCalls++
		w.Write([]byte(`{"ok":true}`))
	}))
	defer telegram.Close()

	all := All{
		Telegram{BotToken: "T", ChatID: "1", BaseURL: telegram.URL},
		Ntfy{Server: forbidden.URL, Topic: "iglo"},
		Ntfy{},
	}
	err := all.Send(context.Background(), "hi")
	if err == nil || !strings.Contains(err.Error(), "forbidden") || telegramCalls != 1 {
		t.Fatalf("err = %v, telegram calls = %d", err, telegramCalls)
	}
	if (All{Ntfy{}, Telegram{}}).Configured() {
		t.Fatal("nothing is set up")
	}
}
