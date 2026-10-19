//go:build linux

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

var (
	loopbackV4 = netip.MustParseAddr("127.0.0.1")
	loopbackV6 = netip.MustParseAddr("::1")
)

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

func TestLinuxHoldsOwnLoopbackListenerAndRefusesStaleIdentity(t *testing.T) {
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
	stale := id
	stale.StartTicks++
	if owns, err := HoldsListener(stale, endpoint); err == nil || owns {
		t.Fatal("stale identity accepted")
	}
	foreign := id
	foreign.UID++
	if owns, err := HoldsListener(foreign, endpoint); err == nil || owns {
		t.Fatal("foreign identity accepted")
	}
	for _, address := range []string{"192.0.2.1:80", "127.0.0.1:0", "[::1]:0", "[::1%lo]:80"} {
		if owns, err := HoldsListener(id, netip.MustParseAddrPort(address)); err == nil || owns {
			t.Fatal("invalid endpoint accepted", address)
		}
	}
	listener.Close()
	owns, err = holdsListenerRetry(t, id, endpoint)
	if err != nil || owns {
		t.Fatal("closed listener still reported as held", owns, err)
	}
}

func TestLinuxHoldsIPv6AndWildcardListeners(t *testing.T) {
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

	v4Wildcard, err := net.Listen("tcp4", "0.0.0.0:0")
	if err != nil {
		t.Fatal(err)
	}
	defer v4Wildcard.Close()
	wildcardPort := uint16(v4Wildcard.Addr().(*net.TCPAddr).Port)
	v4Endpoint := netip.AddrPortFrom(loopbackV4, wildcardPort)
	if owns, err := holdsListenerRetry(t, id, v4Endpoint); err != nil || !owns {
		t.Fatal("IPv4 wildcard not matched for loopback endpoint", owns, err)
	}

	// The kernel's default dual-stack IPv6 wildcard listener does serve IPv4,
	// but Go's tcp6 wildcard is IPv6-only, so /proc cannot determine that
	// and ownership must stay undecided.
	dual, err := net.Listen("tcp6", ":0")
	if err != nil {
		t.Skip("IPv6 wildcard unavailable:", err)
	}
	defer dual.Close()
	dualPort := uint16(dual.Addr().(*net.TCPAddr).Port)
	dualV6 := netip.AddrPortFrom(loopbackV6, dualPort)
	if owns, err := holdsListenerRetry(t, id, dualV6); err != nil || !owns {
		t.Fatal("IPv6 wildcard not matched for IPv6 endpoint", owns, err)
	}
	dualV4 := netip.AddrPortFrom(loopbackV4, dualPort)
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

func TestLinuxUnrelatedProcessListenerIsNotOurs(t *testing.T) {
	if os.Getenv("CERCANO_LINUX_LISTENER_HELPER") == "1" {
		t.Skip("helper process")
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, executable, "-test.run=TestLinuxListenerHelperProcess")
	cmd.Env = append(os.Environ(), "CERCANO_LINUX_LISTENER_HELPER=1")
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
	endpoint := netip.AddrPortFrom(loopbackV4, uint16(port))
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

func TestLinuxListenerHelperProcess(t *testing.T) {
	if os.Getenv("CERCANO_LINUX_LISTENER_HELPER") != "1" {
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

func TestLinuxListenerSnapshotChurnIsUncertaintyNotAbsence(t *testing.T) {
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
		}
	}()

	// Ensure cleanup on every path
	defer func() {
		stop.Store(true)
		<-done
	}()

	deadline := time.Now().Add(300 * time.Millisecond)
	for time.Now().Before(deadline) {
		owns, err := HoldsListener(id, endpoint)
		if err == nil && !owns {
			t.Fatal("fd churn reported as verified absence")
		}
	}
	stop.Store(true)
	<-done
	owns, err := holdsListenerRetry(t, id, endpoint)
	if err != nil || !owns {
		t.Fatal("listener lost after churn", owns, err)
	}
}
