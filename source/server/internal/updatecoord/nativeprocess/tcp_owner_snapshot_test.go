package nativeprocess

import (
	"net/netip"
	"slices"
	"testing"
)

// These tests are portable: they cover the normalized target-PID snapshot
// comparison the Windows adapter uses to refuse changed observations, and
// must pass on every host platform.

func snapshotRows() []tcpOwnerListener {
	return []tcpOwnerListener{
		{localAddr: netip.MustParseAddr("127.0.0.1"), localPort: 443, pid: pidSample},
		{localAddr: netip.MustParseAddr("0.0.0.0"), localPort: 8443, pid: pidSample},
		{localAddr: netip.MustParseAddr("::1"), localPort: 9443, pid: pidSample},
	}
}

func TestTargetOwnerRowsFiltersAndOrders(t *testing.T) {
	rows := append(snapshotRows(),
		tcpOwnerListener{localAddr: netip.MustParseAddr("127.0.0.1"), localPort: 5000, pid: 9999},
		tcpOwnerListener{localAddr: netip.MustParseAddr("::"), localPort: 5001, pid: 7},
	)
	got := targetOwnerRows(rows, pidSample)
	if len(got) != 3 {
		t.Fatalf("kept %d rows, want only the %d target rows", len(got), 3)
	}
	// Canonical order: address, then port. The input arrived in mixed order
	// by port across families; the snapshot must not depend on it.
	if !got[0].localAddr.Is4() || got[0].localPort != 8443 ||
		!got[1].localAddr.Is4() || got[1].localPort != 443 ||
		!got[2].localAddr.Is6() || got[2].localPort != 9443 {
		t.Fatalf("order not canonical: %+v", got)
	}
}

func TestSameOwnerSnapshotIgnoresRowOrder(t *testing.T) {
	first := targetOwnerRows(snapshotRows(), pidSample)
	// The kernel may return the same sockets in any order between reads.
	reordered := slices.Clone(first)
	slices.Reverse(reordered)
	if !sameOwnerSnapshot(first, reordered) {
		t.Fatal("reordered identical rows read as a changed observation")
	}
}

func TestSameOwnerSnapshotDetectsChange(t *testing.T) {
	for name, mutate := range map[string]func(rows []tcpOwnerListener) []tcpOwnerListener{
		"target row removed": func(rows []tcpOwnerListener) []tcpOwnerListener {
			return rows[:len(rows)-1]
		},
		"target port changed": func(rows []tcpOwnerListener) []tcpOwnerListener {
			rows[0].localPort = 4600
			return rows
		},
		"target address changed": func(rows []tcpOwnerListener) []tcpOwnerListener {
			rows[0].localAddr = netip.MustParseAddr("127.0.0.2")
			return rows
		},
		"target pid changed": func(rows []tcpOwnerListener) []tcpOwnerListener {
			rows[0].pid = 4243
			return rows
		},
	} {
		t.Run(name, func(t *testing.T) {
			first := targetOwnerRows(snapshotRows(), pidSample)
			second := targetOwnerRows(mutate(snapshotRows()), pidSample)
			if sameOwnerSnapshot(first, second) {
				t.Fatal("changed target rows read as an unchanged observation")
			}
		})
	}
}

func TestSameOwnerSnapshotIgnoresUnrelatedTraffic(t *testing.T) {
	// Between the two observations another process starts and stops
	// sockets of its own: only the target PID's rows are compared, so
	// unrelated traffic never forces a refusal.
	first := targetOwnerRows(snapshotRows(), pidSample)
	secondRows := append(slices.Clone(snapshotRows()),
		tcpOwnerListener{localAddr: netip.MustParseAddr("127.0.0.1"), localPort: 6000, pid: 9999},
		tcpOwnerListener{localAddr: netip.MustParseAddr("::"), localPort: 6001, pid: 7},
	)
	second := targetOwnerRows(secondRows, pidSample)
	if !sameOwnerSnapshot(first, second) {
		t.Fatal("unrelated other-PID rows read as a changed observation")
	}
}
