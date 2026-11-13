package agent

import "regexp"

const redacted = "[redacted]"

// secretPatterns find keys, tokens and passwords in log lines, keeping the
// name so a reader still sees what was removed. Apps log them more often than
// expected: Sonarr v4.0.20.3014 cleanses its log files but not its console
// (NzbDroneLogger.cs, checked 2026-11-13), so docker logs shows Prowlarr's
// API key in every failed indexer URL.
var secretPatterns = []struct {
	pattern     *regexp.Regexp
	replacement string
}{
	// Query and form parameters: ?apikey=..., &passkey=...
	{regexp.MustCompile(`(?i)([?&;](?:api[_-]?key|api[_-]?token|access[_-]?token|refresh[_-]?token|token|passkey|auth[_-]?key|auth|password|passwd|pass|secret|key|sig|signature|x-plex-token)=)[^&\s"'<>]+`), "${1}" + redacted},
	// Headers: X-Api-Key: ..., Authorization: Bearer ...
	{regexp.MustCompile(`(?i)\b(x-api-key|x-emby-token|x-mediabrowser-token|x-plex-token|authorization)(["']?\s*[:=]\s*["']?)((?:bearer|basic|token)\s+)?[^\s"',]+`), "${1}${2}${3}" + redacted},
	// Quoted values in JSON or settings: "sonarrApiKey": "...", Token="..."
	{regexp.MustCompile(`(?i)(\b[a-z0-9_]*?(?:api[_-]?key|access[_-]?token|refresh[_-]?token|token|password|passwd|passkey|secret|private[_-]?key)["']?\s*[:=]\s*["'])[^"']+`), "${1}" + redacted},
	// Unquoted values: password=..., PROWLARR_API_KEY=...
	{regexp.MustCompile(`(?i)(\b[a-z0-9_]*?(?:api[_-]?key|access[_-]?token|refresh[_-]?token|password|passwd|passkey|private[_-]?key)\s*[:=]\s*)[^\s"',&]+`), "${1}" + redacted},
	// Logins in URLs: https://user:password@host
	{regexp.MustCompile(`(?i)\b([a-z][a-z0-9+.-]*://)[^/\s:@]+:[^/\s@]+@`), "${1}" + redacted + "@"},
}

// redactSecrets removes keys, tokens and passwords from a log line before it
// leaves the host. A log can't be trusted to be clean, and the logs page sends
// lines to the browser.
func redactSecrets(message string) string {
	for _, secret := range secretPatterns {
		message = secret.pattern.ReplaceAllString(message, secret.replacement)
	}
	return message
}
