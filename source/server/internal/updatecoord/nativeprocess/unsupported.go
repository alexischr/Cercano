//go:build (!darwin || !cgo) && !linux

package nativeprocess

import (
	"errors"
	"net/netip"
)

var errUnsupported = errors.New("native process inspection requires macOS with cgo")

func Inspect(pid int) (Identity, error) { return Identity{}, errUnsupported }
func HoldsListener(expected Identity, endpoint netip.AddrPort) (bool, error) {
	return false, errUnsupported
}
