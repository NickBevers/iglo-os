package agent

import (
	"strings"
	"testing"
)

func TestRedactSecrets(t *testing.T) {
	cases := []struct{ in, want string }{
		// A failed indexer call, as Sonarr v4.0.20.3014 writes it to the console.
		{
			"[Warn] HttpClient: HTTP Error - Res: HTTP/1.1 [GET] http://gluetun:9696/9/api?t=tvsearch&cat=5000,5070&extended=1&apikey=0123456789abcdef0123456789abcdef&offset=0&limit=100: 429.TooManyRequests",
			"[Warn] HttpClient: HTTP Error - Res: HTTP/1.1 [GET] http://gluetun:9696/9/api?t=tvsearch&cat=5000,5070&extended=1&apikey=[redacted]&offset=0&limit=100: 429.TooManyRequests",
		},
		{"http://gluetun:9696/2/download?apikey=abc123&link=xyz&file=Show", "http://gluetun:9696/2/download?apikey=[redacted]&link=xyz&file=Show"},
		{"https://tracker.example/announce?passkey=deadbeef", "https://tracker.example/announce?passkey=[redacted]"},
		{"GET /Items?api_key=feedface HTTP/1.1", "GET /Items?api_key=[redacted] HTTP/1.1"},
		{"X-Api-Key: 0123abcd", "X-Api-Key: [redacted]"},
		{`header "X-Emby-Token"="0123abcd"`, `header "X-Emby-Token"="[redacted]"`},
		{"Authorization: Bearer eyJhbGciOi.payload.sig", "Authorization: Bearer [redacted]"},
		{`{"apiKey":"0123abcd","name":"Sonarr"}`, `{"apiKey":"[redacted]","name":"Sonarr"}`},
		{`Token="0123abcd", Client="Kodi"`, `Token="[redacted]", Client="Kodi"`},
		{"password=hunter2 user=nick", "password=[redacted] user=nick"},
		{"PROWLARR_API_KEY=0123abcd", "PROWLARR_API_KEY=[redacted]"},
		{"connecting to smb://nick:hunter2@192.168.0.101/Videos", "connecting to smb://[redacted]@192.168.0.101/Videos"},
	}
	for _, c := range cases {
		if got := redactSecrets(c.in); got != c.want {
			t.Errorf("redactSecrets(%q)\n got %q\nwant %q", c.in, got, c.want)
		}
		if again := redactSecrets(c.want); again != c.want {
			t.Errorf("not stable: %q became %q", c.want, again)
		}
	}
}

func TestRedactSecretsKeepsOrdinaryLines(t *testing.T) {
	for _, line := range []string{
		"[Info] DownloadService: Report sent to qBittorrent. Indexer Anime Tosho (Prowlarr).",
		"http://gluetun:9696/5/api?t=search&cat=5070&q=Death%20March&monkey=1",
		"authorization failed for the session",
		"Token refresh succeeded",
		"Pressed key: Enter",
		"https://example.org/path?page=2",
	} {
		if got := redactSecrets(line); got != line {
			t.Errorf("changed %q to %q", line, got)
		}
	}
}

func TestDockerLogsAreRedacted(t *testing.T) {
	output := "\x1dlist\nsonarr\trunning\tsonarr:4\n\x1dlog sonarr\n2026-11-13T15:00:00Z [Warn] GET http://gluetun:9696/9/api?apikey=0123abcd&limit=100: 429\n"
	result := parseDockerOutput([]byte(output))
	if len(result.Entries) != 1 || strings.Contains(result.Entries[0].Message, "0123abcd") || !strings.Contains(result.Entries[0].Message, "apikey=[redacted]") {
		t.Fatalf("entries = %+v", result.Entries)
	}
}

func TestJournalIsRedacted(t *testing.T) {
	output := `{"MESSAGE":"login https://user:hunter2@host/ ok","PRIORITY":"6","SYSLOG_IDENTIFIER":"app","__REALTIME_TIMESTAMP":"1790690400000000"}` + "\n"
	entries := parseJournal([]byte(output))
	if len(entries) != 1 || strings.Contains(entries[0].Message, "hunter2") {
		t.Fatalf("entries = %+v", entries)
	}
}

func TestRedactSecretsPrefixedNames(t *testing.T) {
	for in, want := range map[string]string{
		`{"sonarrApiKey": "0123abcd"}`: `{"sonarrApiKey": "[redacted]"}`,
		"JELLYFIN_API_KEY: 0123abcd":   "JELLYFIN_API_KEY: [redacted]",
		"admin_password=hunter2":       "admin_password=[redacted]",
	} {
		if got := redactSecrets(in); got != want {
			t.Errorf("redactSecrets(%q) = %q, want %q", in, got, want)
		}
	}
}
