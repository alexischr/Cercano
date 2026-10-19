//go:build windows

package nativeprocess

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

const inspectHelperEnv = "CERCANO_NATIVEPROCESS_INSPECT_HELPER"

// TestWindowsInspectCurrentProcess verifies Inspect against the test process
// itself: PID, absolute kernel-reported image path, non-empty user SID,
// non-zero creation FILETIME, stability across two full inspections, and that
// the Unix UID and start-time fields are NOT overloaded on Windows.
func TestWindowsInspectCurrentProcess(t *testing.T) {
	self := os.Getpid()
	id, err := Inspect(self)
	if err != nil {
		t.Fatalf("Inspect(%d): %v", self, err)
	}
	if id.PID != self {
		t.Fatalf("PID = %d, want %d", id.PID, self)
	}
	if id.UID != 0 || id.StartTicks != 0 || id.StartSeconds != 0 || id.StartMicroseconds != 0 {
		t.Fatalf("Windows identity must not overload Unix fields: %+v", id)
	}
	if id.WindowsSID == "" {
		t.Fatal("WindowsSID is empty")
	}
	if !strings.HasPrefix(id.WindowsSID, "S-1-") {
		t.Fatalf("WindowsSID %q is not a SDDL SID string", id.WindowsSID)
	}
	if id.WindowsStartFileTime <= 0 {
		t.Fatalf("WindowsStartFileTime = %d, want positive raw FILETIME", id.WindowsStartFileTime)
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	requireKernelImageIsExecutable(t, id.Executable, exe)
	again, err := Inspect(self)
	if err != nil {
		t.Fatalf("second Inspect(%d): %v", self, err)
	}
	if !id.SameProcess(again) {
		t.Fatalf("own process identity unstable: %+v vs %+v", id, again)
	}
}

// TestWindowsInspectRefusesInvalidOrAbsentPIDs covers pid validation and
// clearly absent PIDs. Only failure is asserted: an error for an absent PID
// must not claim the process exited cleanly, and access-denied-style errors
// are uncertainty, not absence.
func TestWindowsInspectRefusesInvalidOrAbsentPIDs(t *testing.T) {
	for _, pid := range []int{0, -1, 2147483647, 4294967295, 4294967296} {
		id, err := Inspect(pid)
		if err == nil {
			t.Fatalf("Inspect(%d) accepted invalid or absent PID", pid)
		}
		if id != (Identity{}) {
			t.Fatalf("Inspect(%d) returned identity %+v alongside error %v", pid, id, err)
		}
		if errors.Is(err, errProcessExited) {
			t.Fatalf("Inspect(%d) claimed kernel exit proof for a pid it could not open: %v", pid, err)
		}
	}
}

// TestWindowsInspectOwnedHelperProcess spawns a test-owned helper process,
// inspects it while it is provably alive, then terminates and reaps it and
// verifies that Inspect fails closed afterwards.
func TestWindowsInspectOwnedHelperProcess(t *testing.T) {
	if os.Getenv(inspectHelperEnv) == "1" {
		t.Skip("helper process for TestWindowsInspectOwnedHelperProcess")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	executable, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable: %v", err)
	}
	cmd := exec.CommandContext(ctx, executable, "-test.run=TestWindowsInspectHelperProcess")
	cmd.Env = append(os.Environ(), inspectHelperEnv+"=1")
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatalf("start helper: %v", err)
	}
	defer func() {
		if cmd.Process != nil {
			cmd.Process.Kill()
		}
	}()

	line, err := readHelperLine(stdout)
	if err != nil {
		t.Fatalf("read helper announcement: %v", err)
	}
	if !strings.HasPrefix(line, "HELPER ") {
		t.Fatalf("unexpected helper announcement %q", line)
	}
	var helperPID int
	if _, err := fmt.Sscanf(line, "HELPER %d", &helperPID); err != nil || helperPID <= 0 {
		t.Fatalf("malformed helper pid in %q: %v", line, err)
	}

	// The helper is a child of this test, so its token user SID must equal
	// ours (same logon), proving the SID is observed per-process, not guessed.
	ours, err := Inspect(os.Getpid())
	if err != nil {
		t.Fatalf("Inspect(self) during helper test: %v", err)
	}
	child, err := Inspect(helperPID)
	if err != nil {
		t.Fatalf("Inspect(helper %d): %v", helperPID, err)
	}
	if child.PID != helperPID {
		t.Fatalf("helper PID = %d, want %d", child.PID, helperPID)
	}
	if child.WindowsSID == "" || child.WindowsStartFileTime <= 0 {
		t.Fatalf("incomplete helper identity: %+v", child)
	}
	if child.WindowsSID != ours.WindowsSID {
		t.Fatalf("helper SID %q != own SID %q for a same-user child", child.WindowsSID, ours.WindowsSID)
	}
	if child.StartSeconds != 0 || child.StartMicroseconds != 0 || child.StartTicks != 0 || child.UID != 0 {
		t.Fatalf("helper identity overloads Unix fields: %+v", child)
	}

	// Terminate and reap the owned helper, then require fail-closed behavior:
	// after termination Inspect must return an error and no identity. Which
	// error surfaces (open failure, exit proof, uncertainty) is not asserted,
	// and access-denied is never interpreted as exited.
	cmd.Process.Kill()
	if err := cmd.Wait(); err != nil {
		t.Logf("helper wait: %v", err)
	}
	if _, err := Inspect(helperPID); err == nil {
		t.Fatalf("Inspect(helper %d) succeeded after the process was terminated and reaped", helperPID)
	}
}

// TestWindowsInspectHelperProcess is the re-exec'd helper target.
func TestWindowsInspectHelperProcess(t *testing.T) {
	if os.Getenv(inspectHelperEnv) != "1" {
		t.Skip("runs only as owned helper process")
	}
	// Announce our PID, then park. The parent owns termination and reaping.
	fmt.Printf("HELPER %d\n", os.Getpid())
	for {
		time.Sleep(10 * time.Second)
	}
}

func readHelperLine(r io.Reader) (string, error) {
	type readResult struct {
		n   int
		err error
	}
	buf := make([]byte, 256)
	ch := make(chan readResult, 1)
	go func() {
		n, err := r.Read(buf)
		ch <- readResult{n, err}
	}()
	select {
	case res := <-ch:
		if res.err != nil && res.n == 0 {
			return "", res.err
		}
		return strings.TrimRight(string(buf[:res.n]), "\r\n"), nil
	case <-time.After(10 * time.Second):
		return "", errors.New("helper did not announce itself within 10s")
	}
}

// kernelImageNamesExecutable reports whether the kernel-reported image path
// names the same file as the test executable itself.
//
// The kernel reports the image path exactly as the process was launched
// (QueryFullProcessImageName preserves the CreateProcess form, as does
// os.Executable via GetModuleFileName). On GitHub Windows runners the test
// binary is launched through the 8.3 short-named %TEMP% directory
// (C:\Users\RUNNER~1\AppData\Local\Temp), so both values keep the short
// form, while filepath.EvalSymlinks re-expands every component through
// FindFirstFile (Go stdlib toNorm) and yields the long name
// (C:\Users\runneradmin\...). String-comparing those two forms produced a
// false failure in CI run 39293833025 even though both name the same file.
//
// A case-insensitive match is accepted only when the strings are otherwise
// identical (Windows paths are case-preserving, case-insensitive). Any
// remaining difference is settled by kernel file identity: os.SameFile
// compares the volume serial number and file index, so a path naming any
// other file still fails. This does not weaken the check and does not
// case-fold the identity contract: Identity.Executable itself is untouched.
func kernelImageNamesExecutable(kernelImage, exe string) (bool, error) {
	if strings.EqualFold(kernelImage, filepath.Clean(exe)) {
		return true, nil
	}
	imageInfo, err := os.Stat(kernelImage)
	if err != nil {
		return false, fmt.Errorf("stat kernel image path %q: %w", kernelImage, err)
	}
	exeInfo, err := os.Stat(exe)
	if err != nil {
		return false, fmt.Errorf("stat test executable %q: %w", exe, err)
	}
	if !imageInfo.Mode().IsRegular() || !exeInfo.Mode().IsRegular() {
		return false, fmt.Errorf("kernel image %q and test executable %q must both be regular files", kernelImage, exe)
	}
	return os.SameFile(imageInfo, exeInfo), nil
}

func requireKernelImageIsExecutable(t *testing.T, kernelImage, exe string) {
	t.Helper()
	if !filepath.IsAbs(kernelImage) {
		t.Fatalf("Executable = %q, want absolute kernel image path", kernelImage)
	}
	same, err := kernelImageNamesExecutable(kernelImage, exe)
	if err != nil {
		t.Fatalf("verify kernel image path %q against test executable %q: %v", kernelImage, exe, err)
	}
	if !same {
		t.Fatalf("Executable = %q does not name the test executable %q (file identity differs)", kernelImage, exe)
	}
}

// shortPathName returns the 8.3 short form of path, or an error when the
// volume or file exposes no short name.
func shortPathName(path string) (string, error) {
	long, err := windows.UTF16PtrFromString(filepath.Clean(path))
	if err != nil {
		return "", err
	}
	n, err := windows.GetShortPathName(long, nil, 0)
	if err != nil {
		return "", err
	}
	if n == 0 {
		return "", fmt.Errorf("GetShortPathName(%q): no short path form", path)
	}
	buf := make([]uint16, n)
	n, err = windows.GetShortPathName(long, &buf[0], n)
	if err != nil {
		return "", err
	}
	if n == 0 || n > uint32(len(buf)) {
		return "", fmt.Errorf("GetShortPathName(%q): truncated result", path)
	}
	return windows.UTF16ToString(buf[:n]), nil
}

// TestWindowsKernelImageComparisonAcceptsShortPathAliases pins the fix for
// CI run 39293833025: a test binary launched through an 8.3 short path
// keeps the short form in the kernel image path while filepath.EvalSymlinks
// expands it to the long name. The comparison must accept the alias via
// kernel file identity while still rejecting any genuinely different file,
// in either path form.
func TestWindowsKernelImageComparisonAcceptsShortPathAliases(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	short, err := shortPathName(exe)
	if err != nil {
		t.Skipf("8.3 short path form unavailable: %v", err)
	}
	if strings.EqualFold(short, filepath.Clean(exe)) {
		t.Skip("filesystem exposes no 8.3 alias for the test executable")
	}
	// Positive: the short form and the long form name the same file. A
	// regression here is exactly the CI run 39293833025 symptom.
	requireKernelImageIsExecutable(t, short, exe)

	// Negative control: a different regular file must never pass, in long
	// or short form.
	other, err := os.CreateTemp(t.TempDir(), "not-the-test-binary-*.exe")
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	if err := os.Chmod(other.Name(), 0o755); err != nil {
		t.Fatal(err)
	}
	otherShort, err := shortPathName(other.Name())
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{other.Name(), otherShort} {
		same, err := kernelImageNamesExecutable(path, exe)
		if err != nil {
			t.Fatalf("kernelImageNamesExecutable(%q, %q): %v", path, exe, err)
		}
		if same {
			t.Fatalf("kernel image comparison accepted %q as the test executable", path)
		}
	}
}
