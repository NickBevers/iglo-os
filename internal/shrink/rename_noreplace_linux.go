package shrink

import "golang.org/x/sys/unix"

// Linux 7.0.14 with CIFS 3.0 to Umbrel Samba, checked 2026-11-08 on the host:
// RENAME_NOREPLACE returns EEXIST for an occupied name and succeeds for a free
// name. Install and restore must never replace an upgrade imported by Arr.
func renameNoReplace(from int, old string, to int, name string) error {
	return unix.Renameat2(from, old, to, name, unix.RENAME_NOREPLACE)
}
