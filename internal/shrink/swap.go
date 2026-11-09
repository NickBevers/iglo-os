package shrink

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"golang.org/x/sys/unix"
)

// A durable intent is the commit boundary. Once recorded, cancellation cannot
// change the job. Recovery checks the files themselves, so a crash between a
// rename and the next state write does not require guessing which call ran.
type swapIntent struct {
	Original fileIdentity `json:"original,omitzero"`
	Result   cached       `json:"result"`
}

var errSwapPending = errors.New("replacement needs recovery")

func (s *Service) checkpointAt(point string) {
	if s.checkpoint != nil {
		s.checkpoint(point)
	}
}
func (s *Service) renameAt(from *os.File, old string, to *os.File, name string) error {
	return s.renameWithMode(from, old, to, name, false)
}
func (s *Service) renameNoReplaceAt(from *os.File, old string, to *os.File, name string) error {
	return s.renameWithMode(from, old, to, name, true)
}
func (s *Service) renameWithMode(from *os.File, old string, to *os.File, name string, noReplace bool) error {
	if s.rename != nil {
		return s.rename(int(from.Fd()), old, int(to.Fd()), name, noReplace)
	}
	return renameFile(int(from.Fd()), old, int(to.Fd()), name, noReplace)
}
func renameFile(from int, old string, to int, name string, noReplace bool) error {
	if noReplace {
		return renameNoReplace(from, old, to, name)
	}
	return unix.Renameat(from, old, to, name)
}
func (s *Service) removeAt(dir *os.File, name string) error {
	if s.remove != nil {
		return s.remove(int(dir.Fd()), name)
	}
	return unix.Unlinkat(int(dir.Fd()), name, 0)
}
func tempName(id string) string   { return "iglo-" + id + ".mkv" }
func backupName(id string) string { return "backup-" + id }

func (s *Service) stageDirectory(kind string) (*os.File, error) {
	dir, err := s.openDirectory(kind)
	if err != nil {
		return nil, err
	}
	defer dir.Close()
	if err = unix.Mkdirat(int(dir.Fd()), ".iglo-shrink", 0775); err != nil && !os.IsExist(err) {
		return nil, err
	}
	fd, err := unix.Openat(int(dir.Fd()), ".iglo-shrink", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW, 0)
	if err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(fd), kind+"/.iglo-shrink"), nil
}

// openParent also works when recovery needs to restore a missing original.
func (s *Service) openParent(p string) (*os.File, error) {
	if ValidatePath(p) != nil {
		return nil, ErrInvalid
	}
	parts := strings.Split(p, "/")
	if len(parts) < 2 {
		return nil, ErrInvalid
	}
	root, err := s.openKindRoot(parts[0])
	if err != nil {
		return nil, err
	}
	defer root.Close()
	for i := 1; i < len(parts)-1; i++ {
		info, err := root.Lstat(strings.Join(parts[1:i+1], "/"))
		if err != nil {
			return nil, err
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return nil, ErrInvalid
		}
	}
	return root.Open(filepath.Dir(strings.Join(parts[1:], "/")))
}

// statAt reads attributes by path without opening a media file. In particular,
// fresh CIFS output must be statted after its writing and probing handles close.
func statAt(dir *os.File, name string) (fs.FileInfo, error) {
	root, err := os.OpenRoot(fmt.Sprintf("/dev/fd/%d", dir.Fd()))
	if err != nil {
		return nil, err
	}
	defer root.Close()
	info, err := root.Lstat(name)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, ErrInvalid
	}
	return info, nil
}

// cleanUncommitted removes only this job's output, and only after confirming
// that the original is still intact. Unknown files are retained for inspection.
func (s *Service) cleanUncommitted(j Job, stage *os.File) {
	parent, err := s.openParent(j.Path)
	if err != nil {
		s.logf("shrink %s: preserving temporary output: %v", j.ID, err)
		return
	}
	defer parent.Close()
	info, err := statAt(parent, filepath.Base(j.Path))
	if err != nil || !s.originalMatches(info, j) {
		s.logf("shrink %s: preserving temporary output because the original changed or is missing", j.ID)
		return
	}
	if err = s.removeAt(stage, tempName(j.ID)); err == nil {
		s.logf("shrink %s: removed interrupted output; original is intact", j.ID)
	} else if !os.IsNotExist(err) {
		s.logf("shrink %s: could not remove interrupted output: %v", j.ID, err)
	}
}
func (s *Service) recoveryErrorLocked(j Job, err error) error {
	s.state.Paused = true
	i := slices.IndexFunc(s.state.Jobs, func(v Job) bool { return v.ID == j.ID })
	if i >= 0 {
		if s.state.Jobs[i].State != "done" {
			s.state.Jobs[i].State = "failed"
			s.state.Jobs[i].FinishedAt = time.Now()
		}
		s.state.Jobs[i].Error = fmt.Sprintf("%v: %v", errSwapPending, err)
	}
	s.logf("shrink %s (%s): recovery required; files retained: %v", j.ID, j.Path, err)
	if saveErr := s.saveDurable(); saveErr != nil {
		s.logf("shrink %s: could not save recovery status: %v", j.ID, saveErr)
	}
	return fmt.Errorf("%w: %v", errSwapPending, err)
}

// abortSwapLocked is only used after confirming that the original was restored.
// Save the terminal state before deleting the temp so cancellation cannot race
// the result, and a restart can still identify an interrupted cleanup.
func (s *Service) abortSwapLocked(j Job, stage *os.File, cause error) error {
	intent := s.state.Swaps[j.ID]
	s.state.Paused = true
	delete(s.state.Swaps, j.ID)
	i := slices.IndexFunc(s.state.Jobs, func(v Job) bool { return v.ID == j.ID })
	s.state.Jobs[i].State = "failed"
	s.state.Jobs[i].Error = cause.Error()
	s.state.Jobs[i].FinishedAt = time.Now()
	if err := s.saveDurable(); err != nil {
		s.state.Swaps[j.ID] = intent
		return s.recoveryErrorLocked(j, err)
	}
	s.checkpointAt("swap-aborted")
	s.cleanUncommitted(j, stage)
	return cause
}
func (s *Service) completeSwapLocked(j Job, parent, stage *os.File, intent swapIntent) error {
	name := filepath.Base(j.Path)
	info, err := statAt(parent, name)
	if err != nil || !resultMatches(info, intent) {
		return s.recoveryErrorLocked(j, errors.New("installed result does not match the verified output"))
	}
	s.checkpointAt("result-confirmed")
	i := slices.IndexFunc(s.state.Jobs, func(v Job) bool { return v.ID == j.ID })
	job := s.state.Jobs[i]
	job.State = "done"
	job.Progress = 1
	job.Error = ""
	job.ResultBytes = info.Size()
	job.SavedBytes = job.SourceBytes - job.ResultBytes
	if job.FinishedAt.IsZero() {
		job.FinishedAt = time.Now()
	}
	s.state.Jobs[i] = job
	done := intent.Result
	// Record the installed file's actual attributes after a CIFS rename rather
	// than carrying the staging timestamp into the completed-file cache.
	done.Modified = info.ModTime()
	done.Inode = fileInode(info)
	done.Done = true
	s.state.Cache[j.Path] = done
	intent.Result = done
	s.state.Swaps[j.ID] = intent
	if err = s.saveDurable(); err != nil {
		return s.recoveryErrorLocked(j, err)
	}
	s.checkpointAt("done-recorded")
	// Recheck before deleting the last original copy. A changed result is kept
	// alongside its backup for inspection rather than being silently accepted.
	info, err = statAt(parent, name)
	if err != nil || !resultMatches(info, intent) {
		return s.recoveryErrorLocked(j, errors.New("installed result changed before backup cleanup"))
	}
	backup, err := statAt(stage, backupName(j.ID))
	if err == nil {
		if !s.originalMatches(backup, j) {
			return s.recoveryErrorLocked(j, errors.New("backup differs from the original"))
		}
		if err = s.removeAt(stage, backupName(j.ID)); err != nil {
			return s.recoveryErrorLocked(j, err)
		}
		s.logf("shrink %s: verified replacement and removed backup", j.ID)
	} else if !os.IsNotExist(err) {
		return s.recoveryErrorLocked(j, err)
	}
	s.checkpointAt("backup-removed")
	delete(s.state.Swaps, j.ID)
	if err = s.saveDurable(); err != nil {
		s.state.Swaps[j.ID] = intent
		return s.recoveryErrorLocked(j, err)
	}
	s.logf("shrink %s (%s): completed recorded replacement", j.ID, j.Path)
	s.checkpointAt("swap-cleared")
	return nil
}
func (s *Service) rollbackSwapLocked(j Job, parent, stage *os.File, intent swapIntent, cause error) error {
	original, originalErr := statAt(parent, filepath.Base(j.Path))
	_, tempErr := statAt(stage, tempName(j.ID))
	// A failed call may have completed on the NAS before its response was lost.
	if originalErr == nil && resultMatches(original, intent) && os.IsNotExist(tempErr) {
		return s.completeSwapLocked(j, parent, stage, intent)
	}
	backup, backupErr := statAt(stage, backupName(j.ID))
	if os.IsNotExist(originalErr) && backupErr == nil && s.originalMatches(backup, j) {
		if err := s.renameNoReplaceAt(stage, backupName(j.ID), parent, filepath.Base(j.Path)); err != nil {
			return s.recoveryErrorLocked(j, fmt.Errorf("restore original: %w", err))
		}
		s.checkpointAt("backup-restored")
		s.logf("shrink %s: restored original after a failed rename", j.ID)
		original, originalErr = statAt(parent, filepath.Base(j.Path))
		_, backupErr = statAt(stage, backupName(j.ID))
	}
	if originalErr == nil && s.originalMatches(original, j) && os.IsNotExist(backupErr) {
		return s.abortSwapLocked(j, stage, cause)
	}
	return s.recoveryErrorLocked(j, cause)
}

// finishSwapLocked is idempotent and is used after encoding and during recovery.
// Recovery restores a missing original from its backup before resuming a
// verified swap. Ambiguous files are never removed or overwritten.
func (s *Service) finishSwapLocked(j Job, parent, stage *os.File, intent swapIntent) error {
	original, originalErr := statAt(parent, filepath.Base(j.Path))
	backup, backupErr := statAt(stage, backupName(j.ID))
	temp, tempErr := statAt(stage, tempName(j.ID))
	if originalErr == nil && resultMatches(original, intent) && os.IsNotExist(tempErr) {
		return s.completeSwapLocked(j, parent, stage, intent)
	}
	if os.IsNotExist(originalErr) && backupErr == nil && s.originalMatches(backup, j) {
		if err := s.renameNoReplaceAt(stage, backupName(j.ID), parent, filepath.Base(j.Path)); err != nil {
			return s.recoveryErrorLocked(j, fmt.Errorf("restore original: %w", err))
		}
		s.checkpointAt("backup-restored")
		s.logf("shrink %s: restored missing original from backup", j.ID)
		original, originalErr = statAt(parent, filepath.Base(j.Path))
		_, backupErr = statAt(stage, backupName(j.ID))
	}
	if originalErr != nil || !s.originalMatches(original, j) {
		return s.recoveryErrorLocked(j, errors.New("original is missing or changed"))
	}
	if !os.IsNotExist(backupErr) {
		return s.recoveryErrorLocked(j, errors.New("backup already exists or cannot be inspected"))
	}
	if tempErr != nil || !resultMatches(temp, intent) {
		return s.abortSwapLocked(j, stage, errors.New("verified output is missing or changed; original preserved"))
	}
	if err := s.renameAt(parent, filepath.Base(j.Path), stage, backupName(j.ID)); err != nil {
		return s.rollbackSwapLocked(j, parent, stage, intent, err)
	}
	s.checkpointAt("original-backed-up")
	backup, backupErr = statAt(stage, backupName(j.ID))
	if backupErr != nil || !s.originalMatches(backup, j) {
		return s.recoveryErrorLocked(j, errors.New("could not verify the original backup"))
	}
	if err := s.renameNoReplaceAt(stage, tempName(j.ID), parent, filepath.Base(j.Path)); err != nil {
		if errors.Is(err, unix.EEXIST) {
			return s.recoveryErrorLocked(j, fmt.Errorf("install result: library name already exists: %w", err))
		}
		return s.rollbackSwapLocked(j, parent, stage, intent, err)
	}
	s.checkpointAt("result-installed")
	return s.completeSwapLocked(j, parent, stage, intent)
}

const (
	recoveryRetryEvery      = 10 * time.Minute
	recoveryMountCheckEvery = 30 * time.Second
)

// Run calls this between files. Only pending intents trigger a local findmnt,
// at most twice a minute. A mount return retries immediately at that check;
// other retries are ten minutes apart. No host call, probe or scan is needed.
// Return only completed titles so Run refreshes them once after recovery.
func (s *Service) retryRecoveries(now time.Time) []string {
	s.mu.Lock()
	if len(s.state.Swaps) == 0 || now.Before(s.nextRecoveryCheck) {
		s.mu.Unlock()
		return nil
	}
	s.nextRecoveryCheck = now.Add(recoveryMountCheckEvery)
	s.mu.Unlock()
	mounted := s.MountOK()
	s.mu.Lock()
	defer s.mu.Unlock()
	returned := s.recoveryMountKnown && !s.recoveryMounted && mounted
	s.recoveryMountKnown = true
	s.recoveryMounted = mounted
	if len(s.state.Swaps) == 0 || !mounted {
		return nil
	}
	if !returned && !s.lastRecoveryAttempt.IsZero() && now.Sub(s.lastRecoveryAttempt) < recoveryRetryEvery {
		return nil
	}
	s.lastRecoveryAttempt = now
	var titles []string
	for i, j := range s.state.Jobs {
		if _, pending := s.state.Swaps[j.ID]; pending {
			if err := s.recoverJob(i); err != nil {
				s.logf("shrink %s: could not retry recovery: %v", j.ID, err)
			}
			if _, pending := s.state.Swaps[j.ID]; !pending && s.state.Jobs[i].State == "done" {
				title := titlePath(j.Path)
				if !slices.Contains(titles, title) {
					titles = append(titles, title)
				}
			}
		}
	}
	s.emit("shrink")
	return titles
}

func (s *Service) recoverFiles() error {
	for _, kind := range LibraryFolders {
		stage, err := s.stageDirectory(kind)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return err
		}
		stage.Close()
	}
	for i := range s.state.Jobs {
		if err := s.recoverJob(i); err != nil {
			return err
		}
	}

	// Older releases used a share-level staging directory. It may contain the
	// only remaining copy after a failed CIFS replacement. Never clean it blindly.
	legacyPath := filepath.Join(s.Root, ".iglo-shrink")
	info, err := os.Lstat(legacyPath)
	if err == nil {
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			s.logf("shrink: preserving invalid legacy staging path without following it")
		} else if entries, err := os.ReadDir(legacyPath); err == nil && len(entries) > 0 {
			s.logf("shrink: preserving legacy .iglo-shrink files; inspect them before removal")
		}
	}
	return nil
}

func (s *Service) recoverJob(i int) error {
	j := s.state.Jobs[i]
	stage, err := s.stageDirectory(strings.Split(j.Path, "/")[0])
	if err != nil {
		if _, pending := s.state.Swaps[j.ID]; pending {
			s.recoveryErrorLocked(j, err)
		} else if j.State == "running" {
			s.state.Jobs[i].State = "failed"
			s.state.Jobs[i].Error = "library folder is unavailable; no files removed"
			s.state.Jobs[i].FinishedAt = time.Now()
			return s.saveDurable()
		}
		return nil
	}
	defer stage.Close()
	if intent, pending := s.state.Swaps[j.ID]; pending {
		parent, err := s.openParent(j.Path)
		if err != nil {
			s.recoveryErrorLocked(j, err)
			return nil
		}
		defer parent.Close()
		s.logf("shrink %s (%s): recovering recorded replacement", j.ID, j.Path)
		s.finishSwapLocked(j, parent, stage, intent)
	} else {
		if _, err := statAt(stage, tempName(j.ID)); err == nil {
			s.cleanUncommitted(j, stage)
		}
		if s.state.Jobs[i].State == "running" {
			s.state.Jobs[i].State = "queued"
			s.state.Jobs[i].Progress = 0
			return s.saveDurable()
		}
	}
	return nil
}
