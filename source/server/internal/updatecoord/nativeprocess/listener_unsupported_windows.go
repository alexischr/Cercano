//go:build windows

package nativeprocess

import (
	"errors"
	"net/netip"
)

// HoldsListener is explicitly unsupported on Windows: no loopback-listener
// ownership probe exists for this platform yet. Returning unsupported rather
// than a guess keeps update authorization fail closed.
var errWindowsListenerUnsupported = errors.New("Windows listener ownership verification is not implemented")

func HoldsListener(expected Identity, endpoint netip.AddrPort) (bool, error) {
	return false, errWindowsListenerUnsupported
}
