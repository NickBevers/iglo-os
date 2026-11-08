//go:build !linux && !darwin

package shrink

import "golang.org/x/sys/unix"

// An unsupported platform must preserve files rather than risk overwriting one.
func renameNoReplace(int, string, int, string) error { return unix.ENOTSUP }
