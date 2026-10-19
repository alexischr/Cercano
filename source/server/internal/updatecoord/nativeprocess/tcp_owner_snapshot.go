package nativeprocess

import (
	"cmp"
	"slices"
)

// This file normalizes and compares listener-table snapshots portably, so
// the conservative changing-observation rule is testable on every host
// platform. The Windows adapter reads the same family tables twice and
// refuses to answer when the two normalized snapshots disagree.

// ownerRowLess is the canonical row order: address, then port.
func ownerRowLess(a, b tcpOwnerListener) int {
	if c := a.localAddr.Compare(b.localAddr); c != 0 {
		return c
	}
	return cmp.Compare(a.localPort, b.localPort)
}

// targetOwnerRows returns the rows of one family's decoded listener table
// that the kernel attributes to the target PID, in a canonical order
// (address, then port). Rows owned by other processes are deliberately
// dropped: the snapshot describes this candidate's own sockets, so
// unrelated traffic starting or stopping between the two observations
// cannot force a refusal, and it can never contribute a match either.
func targetOwnerRows(rows []tcpOwnerListener, pid int32) []tcpOwnerListener {
	snapshot := make([]tcpOwnerListener, 0, len(rows))
	for _, row := range rows {
		if row.pid == uint32(pid) {
			snapshot = append(snapshot, row)
		}
	}
	slices.SortFunc(snapshot, ownerRowLess)
	return snapshot
}

// sameOwnerSnapshot compares two normalized snapshots irrespective of the
// order the kernel happened to return the rows in. Both sides are sorted
// into the canonical order on a copy, so the comparison never depends on
// the caller having pre-sorted them.
func sameOwnerSnapshot(a, b []tcpOwnerListener) bool {
	if len(a) != len(b) {
		return false
	}
	x := slices.Clone(a)
	y := slices.Clone(b)
	slices.SortFunc(x, ownerRowLess)
	slices.SortFunc(y, ownerRowLess)
	return slices.EqualFunc(x, y, func(x, y tcpOwnerListener) bool {
		return x.localAddr == y.localAddr && x.localPort == y.localPort && x.pid == y.pid
	})
}
