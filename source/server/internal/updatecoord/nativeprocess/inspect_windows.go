//go:build windows

package nativeprocess

import (
	"errors"
	"fmt"
	"path/filepath"

	"golang.org/x/sys/windows"
)

// Read-only observation. PROCESS_QUERY_LIMITED_INFORMATION grants image name,
// times and exit status; it grants no right to signal, terminate, or inject
// into the observed process, and no token or privilege is opened or enabled.
const queryLimited = windows.PROCESS_QUERY_LIMITED_INFORMATION

// Windows reports this exit code while a process is still running. Any other
// exit code is kernel-provided proof of termination. Equality here is treated
// as "exit not proven" — never as proof of life, since a process can exit
// with code 259 itself.
const stillActiveExitCode = 259

// Upper bound for image-name buffers, in UTF-16 code units. Long
// "\\?\"-prefixed paths can exceed MAX_PATH, so the buffer grows on
// ERROR_INSUFFICIENT_BUFFER and inspection fails closed beyond this cap.
const maxImageNameChars = 32768

var (
	// errProcessExited is returned only on kernel-provided proof of
	// termination (an exit status other than the still-active sentinel).
	errProcessExited = errors.New("process has exited")
	// errObservedChange mirrors the other platforms: two independent
	// observations of the same PID disagreed, so the PID may have been
	// reused and no identity is reported.
	errObservedChange = errors.New("process identity changed during inspection")
)

// Inspect observes one explicitly known PID. Nothing is enumerated.
//
// Every observation reads the same pinned process handle, so a PID reuse
// between calls cannot be mistaken for a stable identity. A process that has
// provably exited, an observation error, or a disagreement between the two
// passes all fail closed with an error; access denied on a protected process
// is uncertainty, not evidence of exit.
func Inspect(pid int) (Identity, error) {
	if pid <= 0 || uint64(pid) > maxPID32 {
		return Identity{}, fmt.Errorf("invalid PID %d", pid)
	}
	first, err := observeWindows(pid)
	if err != nil {
		return Identity{}, err
	}
	second, err := observeWindows(pid)
	if err != nil {
		return Identity{}, err
	}
	if !first.SameProcess(second) {
		return Identity{}, errObservedChange
	}
	return first, nil
}

// maxPID32 bounds the uint32 PID argument of OpenProcess.
const maxPID32 = uint64(^uint32(0))

func observeWindows(pid int) (Identity, error) {
	// Pinned handle: image name, creation time, token SID and exit status are
	// all read from this one kernel object. False is shared-handles-inherit,
	// not the inherit flag.
	handle, err := windows.OpenProcess(queryLimited, false, uint32(pid))
	if err != nil {
		return Identity{}, fmt.Errorf("inspect PID %d: open process: %w", pid, err)
	}
	defer windows.CloseHandle(handle)

	var exitCode uint32
	if err := windows.GetExitCodeProcess(handle, &exitCode); err != nil {
		return Identity{}, fmt.Errorf("inspect PID %d: exit status: %w", pid, err)
	}
	if exitCode != stillActiveExitCode {
		return Identity{}, fmt.Errorf("inspect PID %d: %w (exit code %d)", pid, errProcessExited, exitCode)
	}

	image, err := processImageName(handle, pid)
	if err != nil {
		return Identity{}, err
	}
	creation, err := processCreationTime(handle, pid)
	if err != nil {
		return Identity{}, err
	}
	sid, err := processUserSID(handle, pid)
	if err != nil {
		return Identity{}, err
	}
	return Identity{
		PID:                  pid,
		Executable:           image,
		WindowsSID:           sid,
		WindowsStartFileTime: creation,
	}, nil
}

func processImageName(handle windows.Handle, pid int) (string, error) {
	const initialChars = 1024
	for size := uint32(initialChars); size <= maxImageNameChars; {
		buf := make([]uint16, size)
		n := size
		err := windows.QueryFullProcessImageName(handle, 0, &buf[0], &n)
		if err == nil {
			if n == 0 || int(n) >= len(buf) {
				return "", fmt.Errorf("inspect PID %d: malformed kernel image-name length", pid)
			}
			image := filepath.Clean(windows.UTF16ToString(buf[:n]))
			if !filepath.IsAbs(image) {
				return "", fmt.Errorf("inspect PID %d: kernel reported non-absolute image path", pid)
			}
			return image, nil
		}
		if errors.Is(err, windows.ERROR_INSUFFICIENT_BUFFER) {
			if n > size && uint32(n) <= maxImageNameChars {
				size = n
				continue
			}
			size *= 2
			continue
		}
		return "", fmt.Errorf("inspect PID %d: image name: %w", pid, err)
	}
	return "", fmt.Errorf("inspect PID %d: image name exceeds %d characters", pid, maxImageNameChars)
}

func processCreationTime(handle windows.Handle, pid int) (int64, error) {
	var creation, exit, kernel, user windows.Filetime
	if err := windows.GetProcessTimes(handle, &creation, &exit, &kernel, &user); err != nil {
		return 0, fmt.Errorf("inspect PID %d: process times: %w", pid, err)
	}
	if creation.LowDateTime == 0 && creation.HighDateTime == 0 {
		return 0, fmt.Errorf("inspect PID %d: kernel reported no creation time", pid)
	}
	// Raw FILETIME: 100-nanosecond intervals since 1601-01-01 UTC.
	return int64(creation.Nanoseconds()), nil
}

func processUserSID(handle windows.Handle, pid int) (string, error) {
	var token windows.Token
	if err := windows.OpenProcessToken(handle, windows.TOKEN_QUERY, &token); err != nil {
		return "", fmt.Errorf("inspect PID %d: process token: %w", pid, err)
	}
	defer token.Close()
	user, err := token.GetTokenUser()
	if err != nil {
		return "", fmt.Errorf("inspect PID %d: token user: %w", pid, err)
	}
	if user.User.Sid == nil {
		return "", fmt.Errorf("inspect PID %d: token user SID missing", pid)
	}
	sid := user.User.Sid.String()
	if sid == "" {
		return "", fmt.Errorf("inspect PID %d: token user SID is empty", pid)
	}
	return sid, nil
}
