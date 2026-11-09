package shrink

import "golang.org/x/sys/unix"

// x/sys v0.48.0, checked 2026-11-08: Darwin exposes RENAME_EXCL through
// RenameatxNp. Keep local tests atomic too; never emulate exclusion with a stat.
func renameNoReplace(from int, old string, to int, name string) error {
	return unix.RenameatxNp(from, old, to, name, unix.RENAME_EXCL)
}
