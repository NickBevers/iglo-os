package shrink

import (
	"io/fs"
	"syscall"
	"time"
)

// CIFS 3.0 on kernel 7.0.14 with Umbrel Samba, checked 2026-11-08:
// rename can shift mtime by about 0.5 ms while size and inode stay unchanged.
// Keep a two-second window, and reject a different inode when both are known.
// A Sonarr or Radarr upgrade replaces the file rather than editing it in place.
const identityTimeWindow = 2 * time.Second

// The same CIFS/Samba host, checked 2026-11-08, also delays the write time
// until close and can move it again later. Fresh results use inode and size
// when both inodes are known. Otherwise allow 30 seconds from a post-close stat.
const resultTimeWindow = 30 * time.Second

type fileIdentity struct {
	Bytes    int64     `json:"bytes"`
	Inode    uint64    `json:"inode,omitempty"`
	Modified time.Time `json:"modified"`
}

func fileInode(info fs.FileInfo) uint64 {
	if info != nil {
		if stat, ok := info.Sys().(*syscall.Stat_t); ok && stat != nil {
			return uint64(stat.Ino)
		}
	}
	return 0
}

func inodeChanged(before, after uint64) bool {
	return before != 0 && after != 0 && before != after
}

func identifyFile(info fs.FileInfo) fileIdentity {
	if info == nil {
		return fileIdentity{}
	}
	return fileIdentity{Bytes: info.Size(), Inode: fileInode(info), Modified: info.ModTime()}
}

func (id fileIdentity) matches(info fs.FileInfo) bool {
	if info == nil || info.Size() != id.Bytes || inodeChanged(id.Inode, fileInode(info)) {
		return false
	}
	delta := info.ModTime().Sub(id.Modified)
	return delta >= -identityTimeWindow && delta <= identityTimeWindow
}

func (s *Service) originalMatches(info fs.FileInfo, j Job) bool {
	id, ok := s.state.Sources[j.ID]
	if !ok {
		// Existing queue files have no inode record. Preserve their recovery path.
		id = fileIdentity{Bytes: j.SourceBytes, Modified: j.Modified}
	}
	if intent, pending := s.state.Swaps[j.ID]; pending && intent.Original.Bytes > 0 {
		id = intent.Original
	}
	return id.matches(info)
}

func resultMatches(info fs.FileInfo, intent swapIntent) bool {
	return (fileIdentity{Bytes: intent.Result.Bytes, Inode: intent.Result.Inode, Modified: intent.Result.Modified}).matchesResult(info)
}

func (id fileIdentity) matchesResult(info fs.FileInfo) bool {
	if info == nil || info.Size() != id.Bytes {
		return false
	}
	inode := fileInode(info)
	if id.Inode != 0 && inode != 0 {
		return id.Inode == inode
	}
	delta := info.ModTime().Sub(id.Modified)
	return delta >= -resultTimeWindow && delta <= resultTimeWindow
}
