package agent

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
)

// Syslog levels, as journald uses them.
const (
	LevelError   = 3
	LevelWarning = 4
	LevelInfo    = 6
	LevelDebug   = 7
)

const (
	maxJournalLines = 2000
	maxDockerLines  = 1000
	maxMessage      = 4096
	logsTimeout     = 30 * time.Second
)

var ErrInvalidLogQuery = errors.New("invalid log query")

var dockerName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,127}$`)

type LogEntry struct {
	Time    time.Time `json:"time"`
	Level   int       `json:"level"`
	Source  string    `json:"source"`
	Message string    `json:"message"`
}

type JournalQuery struct {
	// VMID 0 reads the host journal.
	VMID  int
	Lines int
	// Priority shows this level and more important ones, like journalctl -p.
	Priority int
}

func (q JournalQuery) validate() error {
	if q.VMID != 0 && q.VMID < 100 {
		return fmt.Errorf("%w: invalid guest", ErrInvalidLogQuery)
	}
	if q.Lines < 1 || q.Lines > maxJournalLines {
		return fmt.Errorf("%w: lines must be 1 to %d", ErrInvalidLogQuery, maxJournalLines)
	}
	if q.Priority < 0 || q.Priority > LevelDebug {
		return fmt.Errorf("%w: invalid level", ErrInvalidLogQuery)
	}

	return nil
}

type DockerContainer struct {
	Name  string `json:"name"`
	State string `json:"state"`
	Image string `json:"image"`
}

type DockerLogs struct {
	// Installed is false when the container has no Docker, like the manager.
	Installed  bool              `json:"installed"`
	Containers []DockerContainer `json:"containers"`
	Entries    []LogEntry        `json:"entries"`
}

// guestError is a container that is missing or not running.
type guestError string

func (e guestError) Error() string { return string(e) }

// Logs reads journals and Docker logs with fixed, read-only commands.
type Logs struct {
	// run executes a command and returns its output. Tests replace it.
	run func(ctx context.Context, name string, args ...string) ([]byte, error)
}

func NewLogs() *Logs {
	return &Logs{run: func(ctx context.Context, name string, args ...string) ([]byte, error) {
		return exec.CommandContext(ctx, name, args...).Output()
	}}
}

// inGuest runs the command on the host, or in the container when vmid is set.
// pct exec fails on a container that is missing or stopped, so only a failure
// costs a pct status, to say which. Each pct call starts a Perl program.
func (l *Logs) inGuest(ctx context.Context, vmid int, args ...string) ([]byte, error) {
	if vmid == 0 {
		return l.run(ctx, args[0], args[1:]...)
	}

	id := strconv.Itoa(vmid)
	output, err := l.run(ctx, "pct", append([]string{"exec", id, "--"}, args...)...)
	if err == nil || len(output) > 0 {
		return output, err
	}

	status, statusErr := l.run(ctx, "pct", "status", id)
	if statusErr != nil {
		return nil, guestError(fmt.Sprintf("container %d not found", vmid))
	}
	if strings.TrimSpace(string(status)) != "status: running" {
		return nil, guestError(fmt.Sprintf("container %d is not running", vmid))
	}

	return output, err
}

func (l *Logs) Journal(ctx context.Context, query JournalQuery) ([]LogEntry, error) {
	if err := query.validate(); err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(ctx, logsTimeout)
	defer cancel()

	output, err := l.inGuest(ctx, query.VMID, "journalctl", "--no-pager", "--output=json",
		"--lines="+strconv.Itoa(query.Lines), "--priority="+strconv.Itoa(query.Priority))
	if err != nil && len(output) == 0 {
		return nil, fmt.Errorf("could not read the journal: %w", err)
	}

	return parseJournal(output), nil
}

type journalLine struct {
	Message    json.RawMessage `json:"MESSAGE"`
	Priority   string          `json:"PRIORITY"`
	Identifier string          `json:"SYSLOG_IDENTIFIER"`
	Unit       string          `json:"_SYSTEMD_UNIT"`
	Command    string          `json:"_COMM"`
	Realtime   string          `json:"__REALTIME_TIMESTAMP"`
}

func parseJournal(output []byte) []LogEntry {
	entries := []LogEntry{}

	scanner := bufio.NewScanner(bytes.NewReader(output))
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	for scanner.Scan() {
		var line journalLine
		if json.Unmarshal(scanner.Bytes(), &line) != nil {
			continue
		}

		level, err := strconv.Atoi(line.Priority)
		if err != nil {
			level = LevelInfo
		}
		micros, _ := strconv.ParseInt(line.Realtime, 10, 64)

		entries = append(entries, LogEntry{
			Time:    time.UnixMicro(micros).UTC(),
			Level:   level,
			Source:  firstNonEmpty(line.Identifier, strings.TrimSuffix(line.Unit, ".service"), line.Command, "journal"),
			Message: truncate(journalMessage(line.Message)),
		})
	}

	return entries
}

// journalMessage decodes MESSAGE, which journald writes as a string, as a
// list of bytes when it isn't UTF-8, or as null when it is very large.
func journalMessage(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	if string(raw) == "null" {
		return "(message too large to show)"
	}

	var text string
	if json.Unmarshal(raw, &text) == nil {
		return text
	}

	var data []byte
	var numbers []int
	if json.Unmarshal(raw, &numbers) == nil {
		for _, number := range numbers {
			data = append(data, byte(number))
		}
		return strings.ToValidUTF8(string(data), "�")
	}

	return "(message too large to show)"
}

// dockerScript lists the Docker containers and prints their recent logs, all
// in one pct exec. A line that starts with the group separator (\035) opens a
// section: the list, then the log of each container. Docker starts each log
// line with a timestamp, so a log can't open a section. Without Docker the
// script prints nothing.
const dockerScript = `command -v docker >/dev/null 2>&1 || exit 0
names=$(docker ps --all --format '{{.Names}}	{{.State}}	{{.Image}}') || exit 3
printf '\035list\n%s\n' "$names"
printf '%s\n' "$names" | cut -f1 | while IFS= read -r name; do
	[ -n "$name" ] || continue
	printf '\035log %s\n' "$name"
	# Docker writes the app's stderr to our stderr, so combine both.
	docker logs --timestamps --tail "$1" "$name" 2>&1
done
exit 0`

const dockerSection = "\x1d"

// Docker reads the recent logs of every Docker container in a container.
func (l *Logs) Docker(ctx context.Context, vmid, lines int) (DockerLogs, error) {
	if vmid < 100 || lines < 1 || lines > maxDockerLines {
		return DockerLogs{}, fmt.Errorf("%w: invalid guest or lines", ErrInvalidLogQuery)
	}

	ctx, cancel := context.WithTimeout(ctx, logsTimeout)
	defer cancel()

	output, err := l.inGuest(ctx, vmid, "sh", "-c", dockerScript, "sh", strconv.Itoa(lines))
	if err != nil {
		if _, ok := errors.AsType[guestError](err); ok {
			return DockerLogs{}, err
		}
		return DockerLogs{}, fmt.Errorf("could not list Docker containers: %w", err)
	}

	return parseDockerOutput(output), nil
}

// parseDockerOutput reads the sections of dockerScript. It keeps only the logs
// of containers with a valid name in the list.
func parseDockerOutput(output []byte) DockerLogs {
	result := DockerLogs{Containers: []DockerContainer{}, Entries: []LogEntry{}}
	accepted := map[string]bool{}

	section, body := "", strings.Builder{}
	flush := func() {
		switch name, isLog := strings.CutPrefix(section, "log "); {
		case section == "list":
			result.Installed = true
			for line := range strings.Lines(body.String()) {
				fields := strings.Split(strings.TrimSpace(line), "\t")
				if len(fields) != 3 || !dockerName.MatchString(fields[0]) {
					continue
				}
				accepted[fields[0]] = true
				result.Containers = append(result.Containers, DockerContainer{Name: fields[0], State: fields[1], Image: fields[2]})
			}
		case isLog && accepted[name]:
			result.Entries = append(result.Entries, parseDockerLogs(name, []byte(body.String()))...)
		}
		body.Reset()
	}
	for line := range strings.Lines(string(output)) {
		if header, ok := strings.CutPrefix(line, dockerSection); ok {
			flush()
			section = strings.TrimRight(header, "\r\n")
			continue
		}
		body.WriteString(line)
	}
	flush()

	slices.SortStableFunc(result.Entries, func(a, b LogEntry) int { return a.Time.Compare(b.Time) })
	return result
}

var (
	errorWords   = regexp.MustCompile(`(?i)\b(fatal|panic|crit|critical|err|error|exception)\b|\(C\)`)
	warningWords = regexp.MustCompile(`(?i)\b(warn|warning)\b|\(W\)`)
	debugWords   = regexp.MustCompile(`(?i)\b(debug|trace|dbg|verbose)\b`)
)

// parseDockerLogs reads "<RFC 3339 time> <message>" lines. Docker has no log
// levels, so the level is guessed from words like ERROR or [Warn].
func parseDockerLogs(source string, output []byte) []LogEntry {
	entries := []LogEntry{}

	for line := range strings.Lines(string(output)) {
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			continue
		}

		stamp, message, _ := strings.Cut(line, " ")
		timestamp, err := time.Parse(time.RFC3339Nano, stamp)
		if err != nil {
			message = line
		}

		entries = append(entries, LogEntry{
			Time:    timestamp.UTC(),
			Level:   guessLevel(message),
			Source:  source,
			Message: truncate(message),
		})
	}

	return entries
}

func guessLevel(message string) int {
	switch {
	case errorWords.MatchString(message):
		return LevelError
	case warningWords.MatchString(message):
		return LevelWarning
	case debugWords.MatchString(message):
		return LevelDebug
	default:
		return LevelInfo
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func truncate(message string) string {
	if len(message) <= maxMessage {
		return message
	}
	return strings.ToValidUTF8(message[:maxMessage], "") + "…"
}
