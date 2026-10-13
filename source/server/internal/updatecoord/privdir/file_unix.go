//go:build darwin || linux

package privdir

import (
	"golang.org/x/sys/unix"
	"os"
	"path/filepath"
	"syscall"
)

// CreatePrivateFile creates only a new file in an already verified private
// parent. Existing files are never repaired or overwritten.
func CreatePrivateFile(path string) (*os.File, error) {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return nil, ErrInvalidPath
	}
	if e := VerifyExisting(filepath.Dir(path)); e != nil {
		return nil, e
	}
	return os.OpenFile(path, os.O_RDWR|os.O_CREATE|os.O_EXCL|unix.O_NOFOLLOW, 0600)
}

// OpenPrivateFile checks ownership and access on the opened object, not a later
// pathname lookup. Its caller must also verify parent/entry identity bindings.
func OpenPrivateFile(path string) (*os.File, error) {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return nil, ErrInvalidPath
	}
	f, e := os.OpenFile(path, os.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	if e != nil {
		return nil, e
	}
	info, e := f.Stat()
	if e != nil {
		f.Close()
		return nil, e
	}
	raw, ok := info.Sys().(*syscall.Stat_t)
	if !info.Mode().IsRegular() || !ok || raw.Uid != uint32(os.Getuid()) || info.Mode().Perm() != 0600 || raw.Nlink != 1 {
		f.Close()
		return nil, ErrUnsafeACL
	}
	return f, nil
}

// OpenPrivateStateFile also verifies the protected database parent.
func OpenPrivateStateFile(path string) (*os.File, error) {
	if e := VerifyExisting(filepath.Dir(path)); e != nil {
		return nil, e
	}
	return OpenPrivateFile(path)
}
