package nativeprocess

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"net/netip"
)

// This file decodes the raw buffers returned by the Windows
// GetExtendedTcpTable API when asked for TCP_TABLE_OWNER_PID_LISTENER. The
// layout mirrors the iphlpapi.h definitions of MIB_TCPTABLE_OWNER_PID and
// MIB_TCP6TABLE_OWNER_PID:
//
//	typedef struct _MIB_TCPTABLE_OWNER_PID {
//	    DWORD dwNumEntries;
//	    MIB_TCPROW_OWNER_PID table[ANY_SIZE];     // 24 bytes per row
//	} MIB_TCPTABLE_OWNER_PID;
//
//	typedef struct _MIB_TCPROW_OWNER_PID {
//	    DWORD dwState;                            // MIB_TCP_STATE_LISTEN = 2
//	    DWORD dwLocalAddr;                        // network byte order
//	    DWORD dwLocalPort;                        // network byte order
//	    DWORD dwRemoteAddr;
//	    DWORD dwRemotePort;
//	    DWORD dwOwningPid;
//	} MIB_TCPROW_OWNER_PID;
//
//	typedef struct _MIB_TCP6TABLE_OWNER_PID {
//	    DWORD dwNumEntries;
//	    MIB_TCP6ROW_OWNER_PID table[ANY_SIZE];     // 56 bytes per row
//	} MIB_TCP6TABLE_OWNER_PID;
//
//	typedef struct _MIB_TCP6ROW_OWNER_PID {
//	    UCHAR ucLocalAddr[16];                    // network byte order
//	    DWORD dwLocalScopeId;
//	    DWORD dwLocalPort;                        // network byte order
//	    UCHAR ucRemoteAddr[16];
//	    DWORD dwRemoteScopeId;
//	    DWORD dwRemotePort;
//	    DWORD dwState;
//	    DWORD dwOwningPid;
//	} MIB_TCP6ROW_OWNER_PID;
//
// The LARGE_INTEGER liCreateTimestamp belongs to the MIB_TCP{,6}ROW_OWNER_MODULE
// rows, not to these OWNER_PID rows; a parser sized to the OWNER_MODULE stride
// would misframe every row after the first. The row sizes are compile-time
// constants here rather than unsafe.Sizeof results so that any drift between
// this layout and the operating system's shows up as framing and state
// validation failures instead of silently misparsed rows. Windows is
// little-endian on every supported architecture; the multi-byte fields are
// decoded explicitly through encoding/binary so the parser never depends on
// host layout.

var (
	// errTCPTable marks framing the kernel should not produce: a row count
	// that exceeds the buffer, a wrong state for the requested table class,
	// a zero port, a non-positive PID DWORD, or trailing bytes the header
	// count cannot explain.
	errTCPTable = errors.New("invalid or inconsistent TCP owner table")
	// errTCPTableAttribution marks a LISTEN-class row the kernel could not
	// attribute to an owning PID. The class's contract is per-row owner
	// attribution, so a zero PID means the observation cannot ground either
	// an owned or a not-owned answer.
	errTCPTableAttribution = errors.New("TCP owner table row has no owning PID")
)

const (
	tcpTableHeaderSize = 4  // DWORD dwNumEntries
	tcp4RowSize        = 24 // 6 DWORDs: state, local addr+port, remote addr+port, PID
	tcp6RowSize        = 56 // 16+4+4 + 16+4+4 + 4+4 bytes
	mibTCPStateListen  = 2
	// maxTCPOwnerTableBytes bounds any table this parser will decode. The
	// API sizes buffers from a live size query, so a corrupt or hostile
	// size must fail closed instead of driving an allocation.
	maxTCPOwnerTableBytes = 16 << 20 // 16 MiB
)

// Field offsets inside one MIB_TCPROW_OWNER_PID / MIB_TCP6ROW_OWNER_PID row.
const (
	tcp4RowState     = 0
	tcp4RowLocalAddr = 4
	tcp4RowLocalPort = 8
	tcp4RowPID       = 20

	tcp6RowLocalAddr = 0
	tcp6RowLocalPort = 20 // after ucLocalAddr[16] + dwLocalScopeId
	tcp6RowState     = 48 // after remote addr/scope/port
	tcp6RowPID       = 52
)

// tcpOwnerListener is one decoded row of a TCP_TABLE_OWNER_PID_LISTENER
// table: the local address, the local port in host byte order, and the PID
// the kernel attributes the socket to.
type tcpOwnerListener struct {
	localAddr netip.Addr
	localPort uint16
	pid       uint32
}

// decodeTCPTablePort recovers the host-order port from a dwLocalPort field.
// The port is stored network byte order in the first two (lowest-addressed)
// bytes of the DWORD; the remaining two bytes are unused padding the API
// does not define, so they are decoded away, never validated. A zero port
// never occurs on a real listener and is rejected by the caller.
func decodeTCPTablePort(word uint32) uint16 {
	return uint16(word&0xFF)<<8 | uint16(word>>8&0xFF)
}

// decodeTCP4Addr recovers the IPv4 address from a dwLocalAddr field. The
// field stores the address in network byte order, so its lowest-addressed
// bytes, in memory order, are the address bytes.
func decodeTCP4Addr(word uint32) netip.Addr {
	return netip.AddrFrom4([4]byte{byte(word), byte(word >> 8), byte(word >> 16), byte(word >> 24)})
}

// parseTCP4OwnerTable decodes a MIB_TCPTABLE_OWNER_PID buffer as returned by
// GetExtendedTcpTable(AF_INET, TCP_TABLE_OWNER_PID_LISTENER). The header
// count is authoritative: the buffer must hold the complete table it
// claims, and every row is validated — LISTEN state, non-zero port,
// positive owning PID — before it is returned. Anything else is an error,
// never a skipped row that would read as absence. Trailing bytes beyond the
// declared table are tolerated: the caller allocates from a size query that
// may race a shrinking table, so only the header count pins the rows.
func parseTCP4OwnerTable(data []byte) ([]tcpOwnerListener, error) {
	rows, _, err := tcpOwnerRowSpan(data, tcp4RowSize)
	if err != nil {
		return nil, err
	}
	listeners := make([]tcpOwnerListener, 0, len(rows))
	for i := range rows {
		row := rows[i]
		listener := tcpOwnerListener{
			localAddr: decodeTCP4Addr(binary.LittleEndian.Uint32(row[tcp4RowLocalAddr:])),
			localPort: decodeTCPTablePort(binary.LittleEndian.Uint32(row[tcp4RowLocalPort:])),
			pid:       binary.LittleEndian.Uint32(row[tcp4RowPID:]),
		}
		if err := validateTCPOwnerListener(row[tcp4RowState:], listener); err != nil {
			return nil, fmt.Errorf("IPv4 owner table row %d: %w", i, err)
		}
		listeners = append(listeners, listener)
	}
	return listeners, nil
}

// parseTCP6OwnerTable decodes a MIB_TCP6TABLE_OWNER_PID buffer as returned by
// GetExtendedTcpTable(AF_INET6, TCP_TABLE_OWNER_PID_LISTENER), with the same
// validation rules as parseTCP4OwnerTable. The 16-byte local address is
// already network-byte-order bytes; the port uses the same swapped decoding
// as IPv4. The scope IDs are not decoded: loopback endpoints carry no zone
// and the address alone decides a match.
func parseTCP6OwnerTable(data []byte) ([]tcpOwnerListener, error) {
	rows, _, err := tcpOwnerRowSpan(data, tcp6RowSize)
	if err != nil {
		return nil, err
	}
	listeners := make([]tcpOwnerListener, 0, len(rows))
	for i := range rows {
		row := rows[i]
		var addr [16]byte
		copy(addr[:], row[tcp6RowLocalAddr:tcp6RowLocalAddr+16])
		listener := tcpOwnerListener{
			localAddr: netip.AddrFrom16(addr),
			localPort: decodeTCPTablePort(binary.LittleEndian.Uint32(row[tcp6RowLocalPort:])),
			pid:       binary.LittleEndian.Uint32(row[tcp6RowPID:]),
		}
		if err := validateTCPOwnerListener(row[tcp6RowState:], listener); err != nil {
			return nil, fmt.Errorf("IPv6 owner table row %d: %w", i, err)
		}
		listeners = append(listeners, listener)
	}
	return listeners, nil
}

// tcpOwnerRowSpan checks the framing of one owner table: a header count,
// then that many rows of rowSize bytes. The whole buffer is bounded by
// maxTCPOwnerTableBytes, and the row count must fit the remaining bytes —
// a count claiming more rows than present is corruption, not an empty
// table. Trailing bytes after the declared rows are returned as rest: the
// caller allocates from a size query that can race a shrinking table, so
// the header count, not the buffer length, pins the table.
func tcpOwnerRowSpan(data []byte, rowSize int) (rows [][]byte, rest []byte, err error) {
	if len(data) > maxTCPOwnerTableBytes {
		return nil, nil, errTCPTable
	}
	if len(data) < tcpTableHeaderSize {
		return nil, nil, errTCPTable
	}
	count := int(binary.LittleEndian.Uint32(data[:tcpTableHeaderSize]))
	body := data[tcpTableHeaderSize:]
	if count < 0 || count > len(body)/rowSize {
		return nil, nil, errTCPTable
	}
	tableLen := count * rowSize
	rows = make([][]byte, count)
	for i := range rows {
		offset := i * rowSize
		rows[i] = body[offset : offset+rowSize : offset+rowSize]
	}
	return rows, body[tableLen:], nil
}

// validateTCPOwnerListener enforces the contract of the LISTENER table
// class on one decoded row: every row is a listening socket, carries a
// non-zero local port, and is attributed to a positive int32 owning PID
// (a PID DWORD with the sign bit set is corrupt kernel data, not a
// process ID). A violation is a corrupt or unattributable observation and
// errors out; it can never authorize an owned answer and it must not
// silently read as absence.
func validateTCPOwnerListener(stateBytes []byte, listener tcpOwnerListener) error {
	if binary.LittleEndian.Uint32(stateBytes) != mibTCPStateListen {
		return errTCPTable
	}
	if listener.localPort == 0 {
		return errTCPTable
	}
	if listener.pid == 0 {
		return errTCPTableAttribution
	}
	if listener.pid > math.MaxInt32 {
		return errTCPTable
	}
	return nil
}

// queryTCPOwnerListener decides whether one process owns a loopback TCP
// endpoint against already-decoded IPv4 and IPv6 OWNER_PID LISTEN tables.
// The endpoint must be a valid loopback address with a non-zero port and
// no zone; anything else is an error, never a match. Ownership requires
// the kernel-attributed PID and an exact family/address match or a
// wildcard binding of the endpoint's own family (matchEndpoint). An IPv6
// wildcard socket cannot verify an IPv4 endpoint — the tables do not
// expose IPV6_V6ONLY — so a same-PID, same-port IPv6 wildcard against an
// IPv4 endpoint is reported as uncertain, never as positive ownership,
// and the caller must fail closed when only that evidence exists.
func queryTCPOwnerListener(rows4, rows6 []tcpOwnerListener, endpoint netip.AddrPort, pid int32) (owned, uncertain bool, err error) {
	if !endpoint.IsValid() || !endpoint.Addr().IsLoopback() || endpoint.Port() == 0 || endpoint.Addr().Zone() != "" {
		return false, false, fmt.Errorf("restart requires an explicit loopback TCP endpoint")
	}
	if pid <= 0 {
		// A non-positive expected PID could never be kernel-attributed;
		// treating it as a match would be impossible and as absence would
		// be a silent lie, so refuse the observation.
		return false, false, fmt.Errorf("expected PID %d cannot own a socket: %w", pid, errTCPTableAttribution)
	}
	for _, row := range rows4 {
		if row.localPort != endpoint.Port() {
			continue
		}
		if matchEndpoint(row.localAddr, endpoint.Addr()) && row.pid == uint32(pid) {
			owned = true
		}
	}
	for _, row := range rows6 {
		if row.localPort != endpoint.Port() {
			continue
		}
		if matchEndpoint(row.localAddr, endpoint.Addr()) {
			if row.pid == uint32(pid) {
				owned = true
			}
			continue
		}
		if row.localAddr.IsUnspecified() && !row.localAddr.Is4In6() &&
			endpoint.Addr().Is4() && row.pid == uint32(pid) {
			// The process holds an IPv6 wildcard socket on this port; the
			// tables cannot say whether it serves IPv4, so this stays
			// undecided rather than positive.
			uncertain = true
		}
	}
	return owned, uncertain, nil
}
