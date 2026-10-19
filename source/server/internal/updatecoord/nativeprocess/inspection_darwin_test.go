//go:build darwin && cgo

package nativeprocess

import (
	"errors"
	"net"
	"net/netip"
	"os"
	"syscall"
	"testing"
	"time"
)

func TestNativeOwnListenerAndLifetime(t *testing.T) {
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
	if id.PID != os.Getpid() || id.UID != uint32(os.Getuid()) || id.Executable == "" || id.StartSeconds == 0 {
		t.Fatalf("incomplete identity: %+v", id)
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		owns, e := HoldsListener(id, endpoint)
		if e == nil {
			if !owns {
				t.Fatal("owned listener not found")
			}
			break
		}
		if !(errors.Is(e, syscall.EBADF) || errors.Is(e, syscall.EAGAIN)) || time.Now().After(deadline) {
			t.Fatal(e)
		}
		time.Sleep(10 * time.Millisecond)
	}
	stale := id
	stale.StartMicroseconds++
	if owns, e := HoldsListener(stale, endpoint); e == nil || owns {
		t.Fatal("stale lifetime accepted")
	}
	foreign := id
	foreign.UID++
	if owns, e := HoldsListener(foreign, endpoint); e == nil || owns {
		t.Fatal("foreign identity accepted")
	}
	for _, address := range []string{"192.0.2.1:80", "127.0.0.1:0"} {
		if owns, e := HoldsListener(id, netip.MustParseAddrPort(address)); e == nil || owns {
			t.Fatal("invalid endpoint accepted", address)
		}
	}
}
