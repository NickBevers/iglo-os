// Package worker shares durable state, admission and safe publication between
// native media workers. Inputs and job policies remain with each worker.
package worker

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

func Save(dir string, state any) error {
	data, err := json.Marshal(state)
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, ".queue-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if err = f.Chmod(0600); err == nil {
		_, err = f.Write(data)
	}
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Rename(f.Name(), filepath.Join(dir, "queue.json"))
}

func Admitted(dir string) bool {
	data, err := os.ReadFile(filepath.Join(dir, "admit-until"))
	if err != nil {
		return false
	}
	deadline, err := strconv.ParseInt(strings.TrimSpace(string(data)), 10, 64)
	return err == nil && (deadline == -1 || time.Now().Unix() < deadline)
}

type contextReader struct {
	ctx context.Context
	io.Reader
}

func (r contextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.Reader.Read(p)
}

// Publish writes under a hidden unique name, flushes and then renames. It
// never accepts a symlink output, and a missing NAS mount cannot receive data.
func Publish(ctx context.Context, source, root, folder, base string, limit int64, mountOK func() bool) (string, error) {
	info, err := os.Lstat(source)
	if err != nil || !info.Mode().IsRegular() || info.Size() == 0 || info.Size() > limit {
		return "", errors.New("processing produced no valid bounded media file")
	}
	if !mountOK() {
		return "", errors.New("output share is unavailable; nothing was published")
	}
	dir := filepath.Join(root, folder)
	if err := os.MkdirAll(dir, 0775); err != nil {
		return "", err
	}
	resolved, err := filepath.EvalSymlinks(dir)
	if err != nil || resolved != dir {
		return "", errors.New("output must not be a symbolic link")
	}
	input, err := os.Open(source)
	if err != nil {
		return "", err
	}
	defer input.Close()
	dest := filepath.Join(dir, base)
	temp := filepath.Join(dir, ".iglo-incoming-"+base)
	out, err := os.OpenFile(temp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0664)
	if err != nil {
		return "", err
	}
	defer os.Remove(temp)
	written, err := io.Copy(out, contextReader{ctx, io.LimitReader(input, limit+1)})
	if err == nil && written != info.Size() {
		err = errors.New("processed file changed during publication")
	}
	if err == nil {
		err = out.Sync()
	}
	closeErr := out.Close()
	if err != nil {
		return "", err
	}
	if closeErr != nil {
		return "", closeErr
	}
	if !mountOK() {
		return "", errors.New("output share disappeared before publication")
	}
	if _, err := os.Lstat(dest); !os.IsNotExist(err) {
		return "", errors.New("destination already exists")
	}
	if err := os.Rename(temp, dest); err != nil {
		return "", err
	}
	return filepath.Join(folder, base), nil
}

func Copy(ctx context.Context, dst io.Writer, src io.Reader) (int64, error) {
	return io.Copy(dst, contextReader{ctx, src})
}
