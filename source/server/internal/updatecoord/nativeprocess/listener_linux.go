//go:build linux

package nativeprocess

import (
	"fmt"
	"io"
	"net/netip"
	"os"
	"path/filepath"
	"sort"
	"strconv"
)

// HoldsListener verifies a known same-user process owns a loopback TCP
// endpoint. It matches LISTEN socket inodes from the target's own
// network-namespace tables (/proc/<pid>/net/tcp{,6}) against file
// descriptors the process actually holds, using no elevation. Every
// unreadable, truncated, malformed or permission-denied observation is an
// error, and fd/socket churn between snapshots is reported as uncertainty:
// disappearing descriptors must never be reported as verified absence. This
// is a per-candidate observation, never a proof of exclusive endpoint
// ownership or of service health.
func HoldsListener(expected Identity, endpoint netip.AddrPort) (bool, error) {
	if !endpoint.IsValid() || !endpoint.Addr().IsLoopback() || endpoint.Port() == 0 || endpoint.Addr().Zone() != "" {
		return false, fmt.Errorf("restart requires an explicit loopback TCP endpoint")
	}
	before, err := Inspect(expected.PID)
	if err != nil {
		return false, err
	}
	if !before.SameProcess(expected) || before.UID != uint32(os.Geteuid()) {
		return false, fmt.Errorf("listener process identity changed or belongs to another user")
	}
	// Two fd snapshots bracket the table reads; any change between them
	// invalidates the observation in both directions.
	first, err := procSocketInodes(expected.PID)
	if err != nil {
		return false, err
	}
	matches, ambiguous, err := procListenerInodes(expected.PID, endpoint)
	if err != nil {
		return false, err
	}
	second, err := procSocketInodes(expected.PID)
	if err != nil {
		return false, err
	}
	if !sameInodes(first, second) {
		return false, errListenerSnapshot
	}
	found := false
	for inode := range matches {
		if first[inode] {
			found = true
			break
		}
	}
	if !found {
		for inode := range ambiguous {
			if first[inode] {
				// The process holds a socket that may or may not serve the
				// endpoint; /proc cannot decide, so refuse to answer.
				return false, fmt.Errorf("cannot determine from proc whether IPv6 wildcard socket %d serves the IPv4 endpoint: %w", inode, errListenerSnapshot)
			}
		}
	}
	after, err := Inspect(expected.PID)
	if err != nil {
		return false, err
	}
	if !before.SameProcess(after) {
		return false, fmt.Errorf("process changed during socket inspection")
	}
	return found, nil
}

// procSocketInodes enumerates the process's fd directory through an anchored
// /proc root and returns the set of open socket inodes. Anything unreadable
// (including descriptors that vanish mid-enumeration), unparseable or beyond
// the bound is an error, never an empty result that would read as absence.
func procSocketInodes(pid int) (map[uint64]bool, error) {
	root, err := os.OpenRoot(filepath.Join("/proc", strconv.Itoa(pid), "fd"))
	if err != nil {
		return nil, err
	}
	defer root.Close()
	dir, err := root.Open(".")
	if err != nil {
		return nil, err
	}
	defer dir.Close()
	var names []string
	for {
		entries, err := dir.ReadDir(procFDReadBatch)
		for _, entry := range entries {
			fd, e := strconv.Atoi(entry.Name())
			if e != nil || fd < 0 {
				return nil, errProcRecord
			}
			names = append(names, entry.Name())
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		if len(names) > maxProcFDs {
			return nil, errProcRecord
		}
	}
	sort.Strings(names)
	inodes := make(map[uint64]bool, len(names))
	for _, name := range names {
		link, err := root.Readlink(name)
		if err != nil {
			return nil, err
		}
		inode, isSocket, err := parseSocketLink(link)
		if err != nil {
			return nil, err
		}
		if isSocket {
			inodes[inode] = true
		}
	}
	return inodes, nil
}

// procListenerInodes reads the target process's network-namespace socket
// tables. Using /proc/<pid>/net rather than /proc/net judges a namespaced
// target by the namespace it actually listens in, not the host's. A missing
// table is treated as unreadable data: only a fully read, well-formed pair
// of tables can ground an answer.
func procListenerInodes(pid int, endpoint netip.AddrPort) (matches, ambiguous map[uint64]bool, err error) {
	matches = make(map[uint64]bool)
	ambiguous = make(map[uint64]bool)
	root, err := os.OpenRoot(filepath.Join("/proc", strconv.Itoa(pid)))
	if err != nil {
		return nil, nil, err
	}
	defer root.Close()
	for _, table := range []struct {
		name   string
		isIPv6 bool
	}{{"net/tcp", false}, {"net/tcp6", true}} {
		f, err := root.Open(table.name)
		if err != nil {
			return nil, nil, err
		}
		data, err := readBounded(f, maxTCPTable)
		f.Close()
		if err != nil {
			return nil, nil, err
		}
		if err := collectListenerInodes(data, table.isIPv6, endpoint, matches, ambiguous); err != nil {
			return nil, nil, err
		}
	}
	return matches, ambiguous, nil
}

func sameInodes(a, b map[uint64]bool) bool {
	if len(a) != len(b) {
		return false
	}
	for inode := range a {
		if !b[inode] {
			return false
		}
	}
	return true
}
