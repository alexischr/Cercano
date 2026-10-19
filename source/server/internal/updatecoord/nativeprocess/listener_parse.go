package nativeprocess

import (
	"encoding/binary"
	"errors"
	"io"
	"net/netip"
	"slices"
	"strconv"
	"strings"
)

var errListenerSnapshot = errors.New("process file-descriptor snapshot changed during listener inspection")

// Bounds keep every read and enumeration finite; exceeding them is
// uncertainty, never success or absence proof.
const (
	maxProcFDs      = 1 << 15
	maxTCPTable     = 1 << 20
	procFDReadBatch = 128
)

// parseSocketLink decodes an /proc/<pid>/fd/N symlink target such as
// "socket:[4021]" into the owning kernel socket inode. Links that are not
// sockets are reported as such; a link that claims to be a socket but does
// not carry a well-formed inode is corrupt kernel data and errors.
func parseSocketLink(link string) (inode uint64, isSocket bool, err error) {
	if !strings.HasPrefix(link, "socket:[") {
		return 0, false, nil
	}
	if !strings.HasSuffix(link, "]") {
		return 0, true, errProcRecord
	}
	value, parseErr := strconv.ParseUint(strings.TrimSuffix(strings.TrimPrefix(link, "socket:["), "]"), 10, 64)
	if parseErr != nil || value == 0 {
		return 0, true, errProcRecord
	}
	return value, true, nil
}

// procTCPHeaderColumns and procTCP6HeaderColumns spell the exact column
// names the kernel prints on the /proc/net/tcp and /proc/net/tcp6 header
// lines. The two tables differ by more than padding: net/ipv6/tcp_ipv6.c
// names the peer column "remote_address" while net/ipv4/tcp_ipv4.c prints
// "rem_address". A first line that does not spell out the complete header
// for its family — for example a read truncated to a bare "sl" — is corrupt
// framing, not an empty table, and fails closed.
var procTCPHeaderColumns = []string{
	"sl", "local_address", "rem_address", "st", "tx_queue", "rx_queue",
	"tr", "tm->when", "retrnsmt", "uid", "timeout", "inode",
}

var procTCP6HeaderColumns = []string{
	"sl", "local_address", "remote_address", "st", "tx_queue", "rx_queue",
	"tr", "tm->when", "retrnsmt", "uid", "timeout", "inode",
}

// collectListenerInodes parses /proc/net/tcp{,6} records of the form
// "  sl local_address rem/remote_address st ... inode ..." and adds to matches the
// inodes of TCP LISTEN sockets that could serve the requested loopback
// endpoint. Sockets whose service cannot be decided from /proc alone — an
// IPv6 wildcard binding when an IPv4 endpoint is requested, whose dual-stack
// behaviour the tables do not expose — are collected separately as
// ambiguous so the caller can fail closed instead of guessing. The kernel
// prints addresses and ports in host byte order, so parsed values are laid
// out with native endianness to recover the on-wire address bytes. Any
// malformed, truncated or oversized record is an error, never a skip: a
// LISTEN row whose port rules it out for the endpoint still has its inode
// column validated. Non-LISTEN rows legitimately carry inode 0 (e.g.
// TIME_WAIT, whose socket no longer has an owner), so zero is well-formed
// there; a LISTEN row must carry a non-zero inode, which it can then match
// on, but a zero inode can never authorize a match because it errors.
func collectListenerInodes(data []byte, isIPv6 bool, endpoint netip.AddrPort, matches, ambiguous map[uint64]bool) error {
	// The kernel terminates every table row with '\n', so a read whose last
	// byte is not a newline was truncated mid-record. Such a tail must be
	// refused as corrupt framing: parsing the dangling fragment as if it
	// were a complete record could silently drop or half-read a socket.
	if len(data) == 0 || data[len(data)-1] != '\n' {
		return errProcRecord
	}
	header := procTCPHeaderColumns
	if isIPv6 {
		header = procTCP6HeaderColumns
	}
	for i, line := range strings.Split(strings.TrimSuffix(string(data), "\n"), "\n") {
		if i == 0 {
			if !slices.Equal(strings.Fields(line), header) {
				return errProcRecord
			}
			continue
		}
		if line == "" {
			return errProcRecord
		}
		fields := strings.Fields(line)
		if len(fields) < 10 {
			return errProcRecord
		}
		state, err := parseHex(fields[3], 8)
		if err != nil {
			return err
		}
		if state != 0x0A { // TCP_LISTEN: still validate the inode column.
			if _, err = parseProcInode(fields[9]); err != nil {
				return err
			}
			continue
		}
		local, err := parseProcAddrPort(fields[1], isIPv6)
		if err != nil {
			return err
		}
		// Validate the inode before deciding relevance: a malformed record
		// is an error even when its port rules the row out for this
		// endpoint. A listening socket always has an owning inode, so zero
		// here is corrupt kernel data, never a usable match.
		inode, err := parseProcInode(fields[9])
		if err != nil || inode == 0 {
			return errProcRecord
		}
		if local.Port() != endpoint.Port() {
			continue
		}
		if matchEndpoint(local.Addr(), endpoint.Addr()) {
			matches[inode] = true
			continue
		}
		if isIPv6 && local.Addr().IsUnspecified() && !local.Addr().Is4In6() && endpoint.Addr().Is4() {
			// Cannot tell from /proc whether this wildcard socket accepts
			// IPv4; record it as undecided rather than claiming either way.
			ambiguous[inode] = true
		}
	}
	return nil
}

// matchEndpoint accepts an exact address match, or a wildcard binding of the
// endpoint's own family. It never claims an IPv6 socket serves an IPv4
// endpoint: that case is left to the caller as explicit uncertainty.
func matchEndpoint(local, endpoint netip.Addr) bool {
	if local == endpoint {
		return true
	}
	if endpoint.Is4() {
		return local.Is4() && local.IsUnspecified()
	}
	return local.Is6() && !local.Is4In6() && local.IsUnspecified()
}

// parseProcAddrPort decodes an /proc/net/tcp{,6} "local_address" column such
// as "0100007F:3039" (IPv4) or "00000000000000000000000001000000:0EA7"
// (IPv6). Address words are host-byte-order hex; the port is printed in the
// socket's byte order, i.e. the literal port value.
func parseProcAddrPort(field string, isIPv6 bool) (netip.AddrPort, error) {
	address, portText, found := strings.Cut(field, ":")
	if !found {
		return netip.AddrPort{}, errProcRecord
	}
	port, err := parseHex(portText, 16)
	if err != nil || port == 0 {
		return netip.AddrPort{}, errProcRecord
	}
	if isIPv6 {
		if len(address) != 32 {
			return netip.AddrPort{}, errProcRecord
		}
		var raw [16]byte
		for i := 0; i < 4; i++ {
			word, err := parseHex(address[i*8:(i+1)*8], 32)
			if err != nil {
				return netip.AddrPort{}, errProcRecord
			}
			// Kernel emits each 32-bit word in host byte order.
			binary.NativeEndian.PutUint32(raw[i*4:], uint32(word))
		}
		addr := netip.AddrFrom16(raw)
		if !addr.IsValid() {
			return netip.AddrPort{}, errProcRecord
		}
		return netip.AddrPortFrom(addr, uint16(port)), nil
	}
	if len(address) != 8 {
		return netip.AddrPort{}, errProcRecord
	}
	word, err := parseHex(address, 32)
	if err != nil {
		return netip.AddrPort{}, errProcRecord
	}
	var raw [4]byte
	binary.NativeEndian.PutUint32(raw[:], uint32(word))
	return netip.AddrPortFrom(netip.AddrFrom4(raw), uint16(port)), nil
}

func parseHex(s string, bits uint) (uint64, error) {
	if s == "" {
		return 0, errProcRecord
	}
	value, err := strconv.ParseUint(s, 16, int(bits))
	if err != nil {
		return 0, errProcRecord
	}
	return value, nil
}

// parseProcInode decodes the decimal socket inode column. The kernel prints
// 0 for entries whose socket no longer has an owner (e.g. TIME_WAIT), so
// zero is a well-formed value here; callers that need an owning socket —
// anything matching on an inode — must reject it themselves.
func parseProcInode(s string) (uint64, error) {
	value, err := strconv.ParseUint(s, 10, 64)
	if err != nil {
		return 0, errProcRecord
	}
	return value, nil
}

// readBounded reads at most limit bytes; exactly limit+1 marks the record as
// oversized rather than silently truncated.
func readBounded(r io.Reader, limit int) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(r, int64(limit)+1))
	if err != nil {
		return nil, err
	}
	if len(data) > limit {
		return nil, errProcRecord
	}
	return data, nil
}
