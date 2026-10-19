//go:build (!darwin || !cgo) && !linux && !windows

package nativeprocess

import (
	"errors"
	"net/netip"
)

var errUnsupported = errors.New("native process inspection is not implemented on this platform")

func Inspect(pid int) (Identity, error) { return Identity{}, errUnsupported }
func HoldsListener(expected Identity, endpoint netip.AddrPort) (bool, error) {
	return false, errUnsupported
}
