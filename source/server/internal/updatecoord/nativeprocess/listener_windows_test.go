//go:build windows

package nativeprocess

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"
)

const listenerHelperEnv = "CERCANO_NATIVEPROCESS_LISTENER_HELPER"

// holdsListenerRetry tolerates only transient snapshot churn; every other
// error must surface so a test can never mask a real failure as uncertainty.
func holdsListenerRetry(t *testing.T, id Identity, endpoint netip.AddrPort) (bool, error) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		owns, err := HoldsListener(id, endpoint)
		if err == nil || !errors.Is(err, errListenerSnapshot) {
			return owns, err
		}
		if time.Now().After(deadline) {
			t.Fatal("listener inspection never stabilized", err)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestWindowsHoldsOwnLoopbackListenerAndRefusesStaleIdentity(t *testing.T) {
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()

	endpoint := netip.MustParseAddrPort(listener.Addr().String())
	id, err := Inspect(os.Getpid())
	if err != nil {
		t.Fatal(err)
	}
	owns, err := holdsListenerRetry(t, id, endpoint)
	if err != nil || !owns {
		t.Fatal(owns, err)
	}

	// Test wrong port - use a reserved separate listener
	reservedListener, err := net.Listen("tcp4", "127.0.0.1:1")
	if err != nil {
		t.Fatal(err)
	}
	defer reservedListener.Close()
	reservedEndpoint := netip.MustParseAddrPort(reservedListener.Addr().String())
	if owns, err := HoldsListener(id, reservedEndpoint); err == nil || owns {
		t.Fatal("wrong port accepted", owns, err)
	}

	// Test closed port with no ambiguity
	closedEndpoint := netip.MustParseAddrPort("127.0.0.1:12345")
	if owns, err := HoldsListener(id, closedEndpoint); err == nil || owns {
		t.Fatal("closed port accepted", owns, err)
	}

	// Test different loopback address
	otherLoopback := netip.MustParseAddrPort("127.0.0.2:80")
	if owns, err := HoldsListener(id, otherLoopback); err == nil || owns {
		t.Fatal("different loopback address accepted", owns, err)
	}

	// Test wrong expected lifetime error
	stale := id
	stale.WindowsStartFileTime++
	if owns, err := HoldsListener(stale, endpoint); err == nil || owns {
		t.Fatal("stale identity accepted", owns, err)
	}

	// Test explicit invalid endpoint errors
	for _, address := range []string{"192.0.2.1:80", "127.0.0.1:0", "[::1]:0", "[::1%lo]:80"} {
		if owns, err := HoldsListener(id, netip.MustParseAddrPort(address)); err == nil || owns {
			t.Fatal("invalid endpoint accepted", address, owns, err)
		}
	}

	listener.Close()
	owns, err = holdsListenerRetry(t, id, endpoint)
	if err != nil || owns {
		t.Fatal("closed listener still reported as held", owns, err)
	}
}

func TestWindowsHoldsIPv6AndWildcardListeners(t *testing.T) {
	id, err := Inspect(os.Getpid())
	if err != nil {
		t.Fatal(err)
	}

	// Test IPv6 loopback if supported
	v6, err := net.Listen("tcp6", "[::1]:0")
	if err != nil {
		t.Skip("IPv6 loopback unavailable:", err)
	}
	defer v6.Close()
	v6Endpoint := netip.MustParseAddrPort(v6.Addr().String())
	if owns, err := holdsListenerRetry(t, id, v6Endpoint); err != nil || !owns {
		t.Fatal("owned IPv6 listener not found", owns, err)
	}

	// Test IPv4 wildcard
	v4Wildcard, err := net.Listen("tcp4", "0.0.0.0:0")
	if err != nil {
		t.Fatal(err)
	}
	defer v4Wildcard.Close()
	wildcardPort := uint16(v4Wildcard.Addr().(*net.TCPAddr).Port)
	v4Endpoint := netip.AddrPortFrom(netip.MustParseAddr("127.0.0.1"), wildcardPort)
	if owns, err := holdsListenerRetry(t, id, v4Endpoint); err != nil || !owns {
		t.Fatal("IPv4 wildcard not matched for loopback endpoint", owns, err)
	}

	// Test IPv6 wildcard if supported
	dual, err := net.Listen("tcp6", ":0")
	if err != nil {
		t.Skip("IPv6 wildcard unavailable:", err)
	}
	defer dual.Close()
	dualPort := uint16(dual.Addr().(*net.TCPAddr).Port)
	dualV6 := netip.AddrPortFrom(netip.MustParseAddr("::1"), dualPort)
	if owns, err := holdsListenerRetry(t, id, dualV6); err != nil || !owns {
		t.Fatal("IPv6 wildcard not matched for IPv6 endpoint", owns, err)
	}

	// IPv4 over dual-stack should be uncertain
	dualV4 := netip.AddrPortFrom(netip.MustParseAddr("127.0.0.1"), dualPort)
	deadline := time.Now().Add(2 * time.Second)
	for {
		owns, err := HoldsListener(id, dualV4)
		if err != nil {
			if strings.Contains(err.Error(), "cannot determine") {
				break
			}
			if !errors.Is(err, errListenerSnapshot) || time.Now().After(deadline) {
				t.Fatal("expected IPv4-over-dual-stack uncertainty, got", owns, err)
			}
			time.Sleep(10 * time.Millisecond)
			continue
		}
		t.Fatal("undecidable dual-stack service reported as decided:", owns)
	}
}

func TestWindowsUnrelatedProcessListenerIsNotOurs(t *testing.T) {
	if os.Getenv(listenerHelperEnv) == "1" {
		t.Skip("helper process")
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, executable, "-test.run=TestWindowsListenerHelperProcess")
	cmd.Env = append(os.Environ(), listenerHelperEnv+"=1")
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}

	reaped := false
	defer func() {
		if !reaped {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
		}
	}()

	line, err := bufio.NewReader(stdout).ReadString('\n')
	if err != nil {
		t.Fatal(err)
	}
	fields := strings.Fields(line)
	if len(fields) != 2 || fields[0] != "PORT" {
		t.Fatal("unexpected helper output", line)
	}
	port, err := strconv.Atoi(fields[1])
	if err != nil {
		t.Fatal(err)
	}
	endpoint := netip.AddrPortFrom(netip.MustParseAddr("127.0.0.1"), uint16(port))
	child, err := Inspect(cmd.Process.Pid)
	if err != nil {
		t.Fatal(err)
	}
	owns, err := holdsListenerRetry(t, child, endpoint)
	if err != nil || !owns {
		t.Fatal("helper-owned listener not verified", owns, err)
	}
	parent, err := Inspect(os.Getpid())
	if err != nil {
		t.Fatal(err)
	}
	owns, err = holdsListenerRetry(t, parent, endpoint)
	if err != nil || owns {
		t.Fatal("listener of an unrelated process reported as ours", owns, err)
	}
	// After the helper exits, its listener must not become clean absence.
	if err := cmd.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = cmd.Wait()
	reaped = true
	if owns, err := HoldsListener(child, endpoint); err == nil || owns {
		t.Fatal("vanished process reported as decided absence", owns, err)
	}
}

func TestWindowsListenerHelperProcess(t *testing.T) {
	if os.Getenv(listenerHelperEnv) != "1" {
		t.Skip("helper process only")
	}
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	fmt.Printf("PORT %d\n", listener.Addr().(*net.TCPAddr).Port)
	for {
		time.Sleep(time.Hour) // hold the socket until the parent kills us
	}
}

func TestWindowsListenerSnapshotChurnIsUncertaintyNotAbsence(t *testing.T) {
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	endpoint := netip.MustParseAddrPort(listener.Addr().String())
	id, err := Inspect(os.Getpid())
	if err != nil {
		t.Fatal(err)
	}
	var stop bool
	done := make(chan struct{})
	go func() {
		defer close(done)
		for !stop {
			churn, err := net.Listen("tcp4", "127.0.0.1:0")
			if err == nil {
				churn.Close()
			}
		}
	}()

	// Ensure cleanup on every path
	defer func() {
		stop = true
		<-done
	}()

	deadline := time.Now().Add(300 * time.Millisecond)
	for time.Now().Before(deadline) {
		owns, err := HoldsListener(id, endpoint)
		if err == nil && !owns {
			t.Fatal("fd churn reported as verified absence")
		}
	}
	stop = true
	<-done
	owns, err := holdsListenerRetry(t, id, endpoint)
	if err != nil || !owns {
		t.Fatal("listener lost after churn", owns, err)
	}
}

func TestWindowsOwnedDisposableHelperProcess(t *testing.T) {
	if os.Getenv(listenerHelperEnv) == "1" {
		t.Skip("helper process")
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, executable, "-test.run=TestWindowsDisposableListenerHelperProcess")
	cmd.Env = append(os.Environ(), listenerHelperEnv+"=1")
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

	// Verify the helper's listener while it's alive
	portLine, err := readHelperLine(stdout)
	if err != nil {
		t.Fatalf("read helper port announcement: %v", err)
	}
	if !strings.HasPrefix(portLine, "PORT ") {
		t.Fatalf("unexpected helper port announcement %q", portLine)
	}
	var helperPort int
	if _, err := fmt.Sscanf(portLine, "PORT %d", &helperPort); err != nil || helperPort <= 0 {
		t.Fatalf("malformed helper port in %q: %v", portLine, err)
	}
	helperEndpoint := netip.AddrPortFrom(netip.MustParseAddr("127.0.0.1"), uint16(helperPort))

	owns, err := holdsListenerRetry(t, child, helperEndpoint)
	if err != nil || !owns {
		t.Fatalf("helper-owned listener not verified: %v", err)
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

	// Verify the listener is no longer owned after termination
	if owns, err := HoldsListener(child, helperEndpoint); err == nil || owns {
		t.Fatalf("listener still reported as owned after helper termination: %v", err)
	}
}

func TestWindowsDisposableListenerHelperProcess(t *testing.T) {
	if os.Getenv(listenerHelperEnv) != "1" {
		t.Skip("runs only as owned helper process")
	}
	// Announce our PID, then park. The parent owns termination and reaping.
	fmt.Printf("HELPER %d\n", os.Getpid())

	// Announce our listener port
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		fmt.Printf("PORT -1\n") // Indicate failure
		return
	}
	defer listener.Close()
	fmt.Printf("PORT %d\n", listener.Addr().(*net.TCPAddr).Port)

	// Hold the socket until the parent kills us
	for {
		time.Sleep(10 * time.Second)
	}
}

func readHelperLine(r *bufio.Reader) (string, error) {
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
