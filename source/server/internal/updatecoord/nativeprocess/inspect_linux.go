//go:build linux

package nativeprocess

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

const maxProcRecord = 64 << 10

// Inspect reads an anchored proc directory and repeats identity observations.
// A vanished, inaccessible, deleted-image or changing process is refused. This
// does not authorize signalling the PID, nor prove listener ownership.
func Inspect(pid int) (Identity, error) {
	if pid <= 0 {
		return Identity{}, fmt.Errorf("invalid pid %d", pid)
	}
	root, e := os.OpenRoot(filepath.Join("/proc", strconv.Itoa(pid)))
	if e != nil {
		return Identity{}, procError(e)
	}
	defer root.Close()
	read := func(name string) ([]byte, error) {
		f, e := root.Open(name)
		if e != nil {
			return nil, e
		}
		defer f.Close()
		data, e := io.ReadAll(io.LimitReader(f, maxProcRecord+1))
		if e != nil {
			return nil, e
		}
		if len(data) > maxProcRecord {
			return nil, errProcRecord
		}
		return data, nil
	}
	observe := func() (Identity, error) {
		stat, e := read("stat")
		if e != nil {
			return Identity{}, e
		}
		ticks, e := parseProcStart(stat, pid)
		if e != nil {
			return Identity{}, e
		}
		status, e := read("status")
		if e != nil {
			return Identity{}, e
		}
		uid, e := parseProcUID(status)
		if e != nil {
			return Identity{}, e
		}
		executable, e := root.Readlink("exe")
		if e != nil {
			return Identity{}, e
		}
		if !filepath.IsAbs(executable) || filepath.Clean(executable) != executable || strings.HasSuffix(executable, " (deleted)") {
			return Identity{}, errProcRecord
		}
		return Identity{PID: pid, UID: uid, Executable: executable, StartTicks: ticks}, nil
	}
	first, e := observe()
	if e != nil {
		return Identity{}, procError(e)
	}
	second, e := observe()
	if e != nil {
		return Identity{}, procError(e)
	}
	if !first.SameProcess(second) {
		return Identity{}, syscall.EAGAIN
	}
	return first, nil
}

func procError(e error) error {
	// Missing proc data does not prove exit: executable data is also unavailable
	// for zombies and kernel threads. Preserve uncertainty instead of ESRCH.
	if errors.Is(e, os.ErrNotExist) {
		return fmt.Errorf("process observation unavailable: %w", e)
	}
	return e
}
