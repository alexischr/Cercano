//go:build darwin || linux

package tuirestart

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

func Supported() bool { return true }

// Target resolves the same installed executable location, not an arbitrary PATH
// program. A future versioned installer must supply its verified selected target.
func Target() (string, error) {
	p, e := os.Executable()
	if e != nil {
		return "", e
	}
	p = strings.TrimSuffix(p, " (deleted)")
	return p, nil
}
func CheckTarget(path string, expectedVersion ...string) error {
	if !filepath.IsAbs(path) {
		return errors.New("restart target must be absolute")
	}
	info, e := os.Stat(path)
	if e != nil {
		return e
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0111 == 0 {
		return errors.New("restart target is not executable")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, path, "--version", "tui-handoff-protocol")
	cmd.WaitDelay = 100 * time.Millisecond
	out := &boundedOutput{}
	cmd.Stdout = out
	cmd.Stderr = &boundedOutput{}
	if e = cmd.Run(); e != nil || out.text != Protocol+"\n" || out.exceeded {
		return errors.New("installed TUI does not support safe session handoff; restart manually")
	}
	if len(expectedVersion) > 0 && expectedVersion[0] != "" {
		versionCmd := exec.CommandContext(ctx, path, "--version")
		versionCmd.WaitDelay = 100 * time.Millisecond
		value := &boundedOutput{}
		versionCmd.Stdout = value
		versionCmd.Stderr = &boundedOutput{}
		if e = versionCmd.Run(); e != nil || value.exceeded || strings.TrimSpace(value.text) != "cercano-cli "+expectedVersion[0] {
			return errors.New("installed TUI path does not match the announced update; restart manually using the updated installation")
		}
	}

	return nil
}

type boundedOutput struct {
	text     string
	exceeded bool
}

func (b *boundedOutput) Write(p []byte) (int, error) {
	n := len(p)
	if len(b.text)+n > 4096 {
		b.exceeded = true
	} else {
		b.text += string(p)
	}
	return n, nil
}
func Replace(path string, s State, expectedVersion ...string) error {
	data, e := Encode(s)
	if e != nil {
		return e
	}
	if e = CheckTarget(path, expectedVersion...); e != nil {
		return e
	}
	f, e := os.CreateTemp("", "cercano-tui-handoff-")
	if e != nil {
		return e
	}
	defer f.Close()
	// Unlink before transferring sensitive draft bytes. No persistent recovery
	// file and no draft/conversation content in process arguments or environment.
	if e = os.Remove(f.Name()); e != nil {
		return e
	}
	if _, e = f.Write(data); e != nil {
		return e
	}
	if _, e = f.Seek(0, 0); e != nil {
		return e
	}
	flags, e := unix.FcntlInt(f.Fd(), unix.F_GETFD, 0)
	if e != nil {
		return e
	}
	if _, e = unix.FcntlInt(f.Fd(), unix.F_SETFD, flags & ^unix.FD_CLOEXEC); e != nil {
		return e
	}
	return syscall.Exec(path, []string{path, "--internal-tui-handoff-fd", strconv.Itoa(int(f.Fd()))}, os.Environ())
}
func ReadInherited(fd int) (State, error) {
	if fd < 3 {
		return State{}, ErrState
	}
	f := os.NewFile(uintptr(fd), "tui-handoff")
	if f == nil {
		return State{}, ErrState
	}
	defer f.Close()
	syscall.CloseOnExec(fd)
	st, e := f.Stat()
	if e != nil || !st.Mode().IsRegular() || st.Size() > MaxStateBytes {
		return State{}, ErrState
	}
	raw, ok := st.Sys().(*syscall.Stat_t)
	if !ok || raw.Uid != uint32(os.Geteuid()) || raw.Nlink != 0 || st.Mode().Perm() != 0600 {
		return State{}, ErrState
	}
	if _, e = f.Seek(0, 0); e != nil {
		return State{}, ErrState
	}
	return Decode(f)
}
