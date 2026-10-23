//go:build !unix

package modelhost

import "syscall"

// detachSysProcAttr: POSIX session detachment (setsid) has no equivalent
// here; the spawned host is independent of the caller's console by default.
func detachSysProcAttr() *syscall.SysProcAttr {
	return nil
}
