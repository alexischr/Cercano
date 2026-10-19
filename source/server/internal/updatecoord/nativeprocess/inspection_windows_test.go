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
	want, err := filepath.EvalSymlinks(exe)
	if err != nil {
		want = filepath.Clean(exe)
	}
	// Windows paths are case-insensitive; compare accordingly.
	if !strings.EqualFold(id.Executable, filepath.Clean(want)) {
		t.Fatalf("Executable = %q, want %q", id.Executable, want)
	}
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
