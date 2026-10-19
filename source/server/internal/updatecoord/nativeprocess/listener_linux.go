//go:build linux

package nativeprocess

import (
	"errors"
	"net/netip"
)

// HoldsListener is deliberately unavailable until native endpoint ownership
// inspection is implemented. Process identity alone cannot establish this.
func HoldsListener(expected Identity, endpoint netip.AddrPort) (bool, error) {
	return false, errors.New("Linux listener ownership inspection is not implemented")
}
