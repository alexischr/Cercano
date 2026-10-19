package nativeprocess

import (
	"errors"
	"fmt"
)

// This file is the portable half of the Windows TCP owner-table query. The
// query-class constants and the bounded sizing/retry driver are plain Go
// with no build tag, so they are exercised on every host platform against a
// fake query function. The Windows build (listener_windows.go) supplies the
// native wrapper that resolves iphlpapi's GetExtendedTcpTable from the
// system directory only and adapts it to the tcpTableQueryFunc seam.

const (
	// tcpTableQueryClassOwnerPIDListener is the TCP_TABLE_CLASS value passed
	// for the TCP_TABLE_OWNER_PID_LISTENER table. Confirmed against the
	// Microsoft TCP_TABLE_CLASS enum: BASIC_LISTENER=0,
	// BASIC_CONNECTIONS=1, BASIC_ALL=2, OWNER_PID_LISTENER=3,
	// OWNER_PID_CONNECTIONS=4, OWNER_PID_ALL=5, OWNER_MODULE_LISTENER=6.
	// The old value of 4 selected OWNER_PID_CONNECTIONS and returned
	// established connections instead of listening sockets.
	tcpTableQueryClassOwnerPIDListener = 3
	// tcpTableStatusInsufficientBuffer is ERROR_INSUFFICIENT_BUFFER, the
	// documented DWORD status used to size the output buffer.
	tcpTableStatusInsufficientBuffer = 122
	// tcpTableQueryAttempts bounds the size query + read cycles: a table
	// that keeps growing between the calls is a changing observation and
	// fails closed instead of looping.
	tcpTableQueryAttempts = 4
)

// errListenerObsChanged marks two bounded observations of the same target
// that disagreed: the listener table changed between them, so neither
// snapshot can ground an owned or a not-owned answer.
var errListenerObsChanged = errors.New("TCP owner listener table changed between the two bounded observations")

// tcpTableQueryFunc performs one raw GetExtendedTcpTable-style call.
// buf is the output buffer to fill; nil asks the sizing question (a null
// table pointer whose pdwSize starts at zero). It returns the API's DWORD
// status, the DWORD size value the call reported through pdwSize (the
// required size on the sizing call, the written byte count on success),
// and an error only for a seam that cannot call at all. The status, not
// GetLastError, is the outcome that decides success.
type tcpTableQueryFunc func(buf []byte) (status uint32, size uint32, err error)

// boundedTCPTableQuery drives the documented two-call sizing protocol with
// bounded retries and a bounded buffer: a null-buffer sizing call must
// report ERROR_INSUFFICIENT_BUFFER and a plausible required size, the read
// into that buffer must either succeed or report growth (retry), and any
// other status, any seam error, a zero/too-short/oversize size, or a
// success that reports more bytes than the buffer holds or fewer than the
// header plus one row is an error, never an empty result that would read
// as absence. rowSize is the family's MIB row size (24 or 56 bytes), used
// only for the minimum-size sanity checks.
func boundedTCPTableQuery(query tcpTableQueryFunc, rowSize int) ([]byte, error) {
	minimum := tcpTableHeaderSize + rowSize
	for attempt := 0; attempt < tcpTableQueryAttempts; attempt++ {
		// Sizing call: a null table pointer with a zero size reports the
		// required byte count and the insufficient-buffer status.
		status, size, err := query(nil)
		if err != nil {
			return nil, fmt.Errorf("TCP owner table size query: %w", err)
		}
		if status != tcpTableStatusInsufficientBuffer {
			return nil, fmt.Errorf("TCP owner table size query: unexpected status %d: %w", status, errTCPTable)
		}
		if size == 0 {
			return nil, fmt.Errorf("TCP owner table size query reported zero bytes: %w", errTCPTable)
		}
		if uint64(size) < uint64(minimum) {
			return nil, fmt.Errorf("TCP owner table size query reported %d bytes, below the %d-byte header and one row: %w", size, minimum, errTCPTable)
		}
		if uint64(size) > maxTCPTable {
			return nil, fmt.Errorf("TCP owner table exceeds the %d-byte bound: %w", maxTCPTable, errTCPTable)
		}
		buf := make([]byte, size)
		status, written, err := query(buf)
		if err != nil {
			return nil, fmt.Errorf("TCP owner table read: %w", err)
		}
		if status == 0 {
			if uint64(written) > uint64(len(buf)) {
				return nil, fmt.Errorf("TCP owner table read reported %d bytes, more than the %d-byte buffer: %w", written, len(buf), errTCPTable)
			}
			if uint64(written) < uint64(minimum) {
				return nil, fmt.Errorf("TCP owner table read reported %d bytes, below the %d-byte header and one row: %w", written, minimum, errTCPTable)
			}
			return buf[:written], nil
		}
		if status != tcpTableStatusInsufficientBuffer {
			return nil, fmt.Errorf("TCP owner table read: unexpected status %d: %w", status, errTCPTable)
		}
		// The table grew between the sizing query and the read; retry with a
		// fresh sizing call. A table that outgrows the retries is changing
		// faster than it can be observed and errors out.
	}
	return nil, fmt.Errorf("TCP owner table changed size across %d bounded queries: %w", tcpTableQueryAttempts, errTCPTable)
}
