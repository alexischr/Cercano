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
	"sync/atomic"
	"testing"
	"time"
)

const listenerHelperEnv = "CERCANO_NATIVEPROCESS_LISTENER_HELPER"

// listenerHelperSelfLifetime bounds a helper's own life: the parent owns
// termination and reaping within its bounded context, but a helper whose
// parent crashed before killing it must not park forever either.
const listenerHelperSelfLifetime = 60 * time.Second

// holdsListenerRetry tolerates only transient observation churn — a snapshot
// that changed mid-read (errListenerSnapshot) or a target table that changed
// between the adapter's two bounded reads (errListenerObsChanged) — by
// re-observing within a bounded window until the two reads agree. Every
// other error must surface so a test can never mask a real failure as
// uncertainty, and the loop is bounded in time, never unbounded.
func holdsListenerRetry(t *testing.T, id Identity, endpoint netip.AddrPort) (bool, error) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		owns, err := HoldsListener(id, endpoint)
		if err == nil || (!errors.Is(err, errListenerSnapshot) && !errors.Is(err, errListenerObsChanged)) {
			return owns, err
		}
		if time.Now().After(deadline) {
			t.Fatal("listener inspection never stabilized within the bounded retry window", err)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// TestWindowsHoldsOwnLoopbackListenerAndRefusesStaleIdentity verifies a
// listener the test process itself owns with only ever-true fixtures: an
// ephemeral 127.0.0.1 binding. Every negative is grounded in a socket that
// really is held by someone — the same real port queried at a different
// loopback address — or in a port this process genuinely freed by Close.
// No fixed port is ever assumed absent.
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
		t.Fatalf("own ephemeral loopback listener must be owned positive, got owned=%v err=%v", owns, err)
	}

	// Different loopback address, same really-held port: the table contains
	// this process's row for 127.0.0.1:<port>, which does not match
	// 127.0.0.2:<port>, so the answer is decided absence for this
	// candidate — (false, nil), never an error.
	otherLoopback := netip.AddrPortFrom(netip.MustParseAddr("127.0.0.2"), endpoint.Port())
	if owns, err := holdsListenerRetry(t, id, otherLoopback); err != nil || owns {
		t.Fatalf("same held port at 127.0.0.2 must be decided absence for us, got owned=%v err=%v", owns, err)
	}

	// Stale identity (later incarnation of the same PID) must be refused.
	stale := id
	stale.WindowsStartFileTime++
	if owns, err := HoldsListener(stale, endpoint); err == nil || owns {
		t.Fatal("stale identity accepted", owns, err)
	}

	// Structurally invalid endpoints error before any table is read.
	for _, address := range []string{"192.0.2.1:80", "127.0.0.1:0", "[::1]:0", "[::1%lo]:80"} {
		if owns, err := HoldsListener(id, netip.MustParseAddrPort(address)); err == nil || owns {
			t.Fatal("invalid endpoint accepted", address, owns, err)
		}
	}

	// Close the listener and query the very port it held: after Close the
	// port is genuinely freed by this process, so decided absence
	// (false, nil) is the faithful answer — reached via stabilization, not
	// a single racing read.
	listener.Close()
	if owns, err := holdsListenerRetry(t, id, endpoint); err != nil || owns {
		t.Fatalf("own closed listener's actual port must be decided absence, got owned=%v err=%v", owns, err)
	}
}

// TestWindowsHoldsIPv4WildcardListener is deliberately a separate test from
// the IPv6 cases: a t.Skip for unavailable IPv6 must never remove IPv4
// wildcard coverage.
func TestWindowsHoldsIPv4WildcardListener(t *testing.T) {
	id, err := Inspect(os.Getpid())
	if err != nil {
		t.Fatal(err)
	}
	v4Wildcard, err := net.Listen("tcp4", "0.0.0.0:0")
	if err != nil {
		t.Fatal(err)
	}
	defer v4Wildcard.Close()
	wildcardPort := uint16(v4Wildcard.Addr().(*net.TCPAddr).Port)
	// A same-family wildcard row matches every IPv4 loopback address, so
	// both queried endpoints are owned positive against the really-held
	// socket.
	for _, address := range []netip.Addr{netip.MustParseAddr("127.0.0.1"), netip.MustParseAddr("127.0.0.2")} {
		endpoint := netip.AddrPortFrom(address, wildcardPort)
		if owns, err := holdsListenerRetry(t, id, endpoint); err != nil || !owns {
			t.Fatalf("IPv4 wildcard not matched for loopback endpoint %s: owned=%v err=%v", endpoint, owns, err)
		}
	}
}

// TestWindowsHoldsIPv6LoopbackListener covers an owned IPv6 loopback
// listener; it skips only the IPv6 coverage it cannot run.
func TestWindowsHoldsIPv6LoopbackListener(t *testing.T) {
	id, err := Inspect(os.Getpid())
	if err != nil {
		t.Fatal(err)
	}
	v6, err := net.Listen("tcp6", "[::1]:0")
	if err != nil {
		t.Skip("IPv6 loopback unavailable:", err)
	}
	defer v6.Close()
	v6Endpoint := netip.MustParseAddrPort(v6.Addr().String())
	if owns, err := holdsListenerRetry(t, id, v6Endpoint); err != nil || !owns {
		t.Fatal("owned IPv6 listener not found", owns, err)
	}
}

// TestWindowsIPv6WildcardDualStackIsUndecidableForIPv4 covers the dual-stack
// boundary in its own test so its IPv6-unavailability skip cannot hide any
// other coverage.
func TestWindowsIPv6WildcardDualStackIsUndecidableForIPv4(t *testing.T) {
	id, err := Inspect(os.Getpid())
	if err != nil {
		t.Fatal(err)
	}
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
	// The tables cannot say whether the IPv6 wildcard socket serves IPv4:
	// the adapter must report that specific undecidability, bounded by
	// transient-churn retries, and never a decided answer in either
	// direction.
	dualV4 := netip.AddrPortFrom(netip.MustParseAddr("127.0.0.1"), dualPort)
	deadline := time.Now().Add(2 * time.Second)
	for {
		owns, err := HoldsListener(id, dualV4)
		if err != nil {
			if errors.Is(err, errListenerSnapshot) && strings.Contains(err.Error(), "cannot determine") {
				break
			}
			if (!errors.Is(err, errListenerSnapshot) && !errors.Is(err, errListenerObsChanged)) || time.Now().After(deadline) {
				t.Fatal("expected IPv4-over-dual-stack uncertainty, got", owns, err)
			}
			time.Sleep(10 * time.Millisecond)
			continue
		}
		t.Fatal("undecidable dual-stack service reported as decided:", owns)
	}
}

// TestWindowsUnrelatedProcessListenerIsNotOurs spawns a helper process that
// holds an ephemeral loopback listener and uses that really-held socket as
// the wrong-port/wrong-owner negative for the parent: the parent is decided
// absent on the helper's port, and the helper is decided absent on a port
// the parent really holds. Both negatives rest on sockets someone actually
// holds, never on a fixed port assumed free.
func TestWindowsUnrelatedProcessListenerIsNotOurs(t *testing.T) {
	if os.Getenv(listenerHelperEnv) == "1" {
		t.Skip("helper process")
	}
	// A port the parent really holds, for the wrong-port negative below.
	own, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer own.Close()
	ownEndpoint := netip.MustParseAddrPort(own.Addr().String())

	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
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

	// Correct helper cleanup: the parent owns termination and reaping, and
	// the defer kills and reaps only if the happy path has not already
	// reaped the helper.
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
	if err != nil || port <= 0 {
		t.Fatalf("malformed helper port in %q: %v", line, err)
	}
	helperEndpoint := netip.AddrPortFrom(netip.MustParseAddr("127.0.0.1"), uint16(port))
	child, err := Inspect(cmd.Process.Pid)
	if err != nil {
		t.Fatal(err)
	}
	if owns, err := holdsListenerRetry(t, child, helperEndpoint); err != nil || !owns {
		t.Fatalf("helper-owned listener not verified: owned=%v err=%v", owns, err)
	}
	parent, err := Inspect(os.Getpid())
	if err != nil {
		t.Fatal(err)
	}
	// Wrong-owner negative: the helper's port is really held, but by another
	// process, so the parent is decided absent — (false, nil).
	if owns, err := holdsListenerRetry(t, parent, helperEndpoint); err != nil || owns {
		t.Fatalf("listener of an unrelated process reported as ours: owned=%v err=%v", owns, err)
	}
	// Wrong-port negative: the parent's port is really held, but by another
	// process, so the helper is decided absent — (false, nil). The port is
	// never assumed free: its owner row exists.
	if owns, err := holdsListenerRetry(t, child, ownEndpoint); err != nil || owns {
		t.Fatalf("helper reported as owner of the parent's held port: owned=%v err=%v", owns, err)
	}
	// After the helper exits, its vanished identity must fail closed: an
	// error, never decided absence and never ownership.
	if err := cmd.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = cmd.Wait()
	reaped = true
	if owns, err := HoldsListener(child, helperEndpoint); err == nil || owns {
		t.Fatal("vanished process reported as decided absence", owns, err)
	}
}

// TestWindowsListenerHelperProcess is the helper child of
// TestWindowsUnrelatedProcessListenerIsNotOurs. It announces its ephemeral
// port, then parks with bounded ticks under a bounded self-lifetime so it
// can never outlive the parent's kill by more than a leak guard.
func TestWindowsListenerHelperProcess(t *testing.T) {
	if os.Getenv(listenerHelperEnv) != "1" {
		t.Skip("helper process only")
	}
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	fmt.Printf("PORT %d\n", listener.Addr().(*net.TCPAddr).Port)
	selfDestruct := time.Now().Add(listenerHelperSelfLifetime)
	for time.Now().Before(selfDestruct) {
		time.Sleep(1 * time.Second) // hold the socket until the parent kills us
	}
}

// TestWindowsListenerSnapshotChurnIsUncertaintyNotAbsence churns this
// process's own listener population under a bounded, atomically-stopped
// goroutine and requires that the adapter never report verified absence for
// a listener it still owns.
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
	var stop atomic.Bool
	done := make(chan struct{})
	go func() {
		defer close(done)
		for !stop.Load() {
			churn, err := net.Listen("tcp4", "127.0.0.1:0")
			if err == nil {
				churn.Close()
			}
			time.Sleep(time.Millisecond)
		}
	}()

	// Ensure cleanup on every path: stop first, then join the bounded
	// goroutine.
	defer func() {
		stop.Store(true)
		<-done
	}()

	deadline := time.Now().Add(300 * time.Millisecond)
	for time.Now().Before(deadline) {
		owns, err := HoldsListener(id, endpoint)
		if err == nil && !owns {
			t.Fatal("churn reported as verified absence")
		}
	}
	stop.Store(true)
	<-done
	if owns, err := holdsListenerRetry(t, id, endpoint); err != nil || !owns {
		t.Fatalf("listener lost after churn: owned=%v err=%v", owns, err)
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
	// Correct helper cleanup: kill only if not reaped, so the happy path's
	// explicit kill+wait is not doubled.
	reaped := false
	defer func() {
		if !reaped {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
		}
	}()

	line, err := readListenerHelperLine(bufio.NewReader(stdout))
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
	portLine, err := readListenerHelperLine(bufio.NewReader(stdout))
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

	if owns, err := holdsListenerRetry(t, child, helperEndpoint); err != nil || !owns {
		t.Fatalf("helper-owned listener not verified: owned=%v err=%v", owns, err)
	}

	// Terminate and reap the owned helper, then require fail-closed behavior:
	// after termination Inspect must return an error and no identity. Which
	// error surfaces (open failure, exit proof, uncertainty) is not asserted,
	// and access-denied is never interpreted as exited.
	cmd.Process.Kill()
	if err := cmd.Wait(); err != nil {
		t.Logf("helper wait: %v", err)
	}
	reaped = true
	if _, err := Inspect(helperPID); err == nil {
		t.Fatalf("Inspect(helper %d) succeeded after the process was terminated and reaped", helperPID)
	}

	// The listener is no longer owned by the vanished helper: an error, never
	// ownership and never clean absence.
	if owns, err := HoldsListener(child, helperEndpoint); err == nil || owns {
		t.Fatalf("listener still reported as owned after helper termination: owned=%v err=%v", owns, err)
	}
}

// TestWindowsDisposableListenerHelperProcess is the helper child of
// TestWindowsOwnedDisposableHelperProcess. It announces its PID and its
// ephemeral listener, then parks under a bounded self-lifetime; the parent
// owns termination and reaping.
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

	selfDestruct := time.Now().Add(listenerHelperSelfLifetime)
	for time.Now().Before(selfDestruct) {
		time.Sleep(1 * time.Second)
	}
}

// readListenerHelperLine reads one announcement line from the disposable
// helper's bufio.Reader with a bounded 10s wait. On timeout the underlying
// reader goroutine remains blocked only until the helper's pipe is closed,
// which every test path does by killing and reaping the helper. It is named
// apart from inspection_windows_test.go's readHelperLine to keep the windows
// test package single-named.
func readListenerHelperLine(r *bufio.Reader) (string, error) {
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
