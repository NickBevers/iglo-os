package agent

import (
	"bufio"
	"io"
	"os"
	"strings"
	"time"
)

// installLog keeps what Status read of install.log, so each call reads only
// what the installer wrote since. The log has no size cap, and Status runs
// about twice a second during an install.
type installLog struct {
	// run is the start of the operation that wrote the log. A new run
	// truncates the file, so its log starts over.
	run  time.Time
	info os.FileInfo
	// head is the start of the file. A file that was written again from the
	// start, not only added to, has another head.
	head   string
	offset int64
	// shown is the cleaned log of the lines read so far, cut to just over
	// maxAppLog, so tail still marks it as cut.
	shown  string
	apiKey string
	result []string
}

// read returns the cleaned tail of the log, the Jellyfin key and the first
// app result line. The line that is still being written shows, but only a
// whole line counts for the key and the result.
func (l *installLog) read(path string, run time.Time) (log, apiKey string, result []string, ok bool) {
	file, err := os.Open(path)
	if err != nil {
		*l = installLog{}
		return "", "", nil, false
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil {
		*l = installLog{}
		return "", "", nil, false
	}
	buffer := make([]byte, 64)
	count, _ := file.ReadAt(buffer, 0)
	head := string(buffer[:count])
	if !l.run.Equal(run) || l.info == nil || !os.SameFile(l.info, info) || info.Size() < l.offset || !strings.HasPrefix(head, l.head) {
		*l = installLog{run: run}
	}
	l.info, l.head = info, head

	if _, err := file.Seek(l.offset, io.SeekStart); err != nil {
		*l = installLog{}
		return "", "", nil, false
	}
	partial := ""
	reader := bufio.NewReader(file)
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			partial = line
			break
		}
		l.offset += int64(len(line))
		l.add(line)
	}

	partial = jellyfinKeyPattern.ReplaceAllString(ansiPattern.ReplaceAllString(partial, ""), "")
	return tail(l.shown+partial, maxAppLog), l.apiKey, l.result, true
}

func (l *installLog) add(line string) {
	line = ansiPattern.ReplaceAllString(line, "")
	// The browser never sees the key, so its line is not shown.
	if match := jellyfinKeyPattern.FindStringSubmatch(line); match != nil {
		l.apiKey = match[1]
		return
	}
	if l.result == nil {
		l.result = appResultPattern.FindStringSubmatch(line)
	}
	l.shown += line
	if len(l.shown) > 2*maxAppLog {
		l.shown = l.shown[len(l.shown)-maxAppLog-1:]
	}
}
