package brewrestart

import (
	"cercano/source/server/internal/updatecoord/nativeprocess"
	"net/netip"
)

// Inspect preserves the Homebrew inspection API using the shared kernel reader.
func Inspect(pid int) (Identity, error) { return nativeprocess.Inspect(pid) }

// HoldsListener proves only per-process listener ownership, not uniqueness.
func HoldsListener(expected Identity, endpoint netip.AddrPort) (bool, error) {
	return nativeprocess.HoldsListener(expected, endpoint)
}
