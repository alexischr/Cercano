package nativeprocess

import (
	"errors"
	"testing"
)

// These tests are portable: they drive boundedTCPTableQuery through a fake
// tcpTableQueryFunc and must pass on every host platform without ever
// calling the Windows API. The fake mirrors the documented GetExtendedTcpTable
// contract: the DWORD status is the outcome, pdwSize carries the required
// size on the sizing call and the written byte count on success, and a
// successful read fills the caller's buffer.

var errFakeSeam = errors.New("proc missing")

// fakeCall scripts one raw query call. payload is copied into the caller's
// buffer on a read call (nil for a sizing call, which passes a nil buffer).
type fakeCall struct {
	status  uint32
	size    uint32
	err     error
	payload []byte

	// Recorded, not scripted: the recorded call marks whether the caller
	// passed a nil (sizing) buffer and how large the buffer was.
	sizing bool
	bufLen int
}

// fakeQuery builds a tcpTableQueryFunc from per-call scripted outcomes,
// applied in order. It records every call (sizing or read) so tests can
// assert boundedness.
func fakeQuery(t *testing.T, script ...fakeCall) (tcpTableQueryFunc, *[]fakeCall) {
	t.Helper()
	calls := &[]fakeCall{}
	return func(buf []byte) (uint32, uint32, error) {
		if len(script) == 0 {
			t.Fatalf("fake query called beyond the scripted %d outcomes", len(*calls))
		}
		out := script[0]
		script = script[1:]
		*calls = append(*calls, fakeCall{sizing: buf == nil, bufLen: len(buf)})
		if out.err == nil && out.payload != nil && buf != nil {
			if len(out.payload) > len(buf) {
				t.Fatalf("fake payload of %d bytes does not fit the %d-byte buffer", len(out.payload), len(buf))
			}
			copy(buf, out.payload)
		}
		return out.status, out.size, out.err
	}, calls
}

// sizingCall scripts a well-formed sizing outcome for the given table size.
func sizingCall(n int) fakeCall {
	return fakeCall{status: tcpTableStatusInsufficientBuffer, size: uint32(n)}
}

// readCall scripts a successful read outcome that delivers the table.
func readCall(table []byte) fakeCall {
	return fakeCall{status: 0, size: uint32(len(table)), payload: table}
}

// TestTCPTableQueryClassOwnerPIDListener pins the TCP_TABLE_CLASS value for
// TCP_TABLE_OWNER_PID_LISTENER. The Microsoft TCP_TABLE_CLASS enum is
// BASIC_LISTENER=0, BASIC_CONNECTIONS=1, BASIC_ALL=2, OWNER_PID_LISTENER=3,
// OWNER_PID_CONNECTIONS=4, OWNER_PID_ALL=5, OWNER_MODULE_LISTENER=6 — a
// value of 4 would select OWNER_PID_CONNECTIONS and return established
// connections instead of listening sockets, so the regression asserts
// exactly 3.
func TestTCPTableQueryClassOwnerPIDListener(t *testing.T) {
	if tcpTableQueryClassOwnerPIDListener != 3 {
		t.Fatalf("tcpTableQueryClassOwnerPIDListener = %d, want exactly 3 (OWNER_PID_LISTENER; 4 is OWNER_PID_CONNECTIONS)", tcpTableQueryClassOwnerPIDListener)
	}
	if tcpTableStatusInsufficientBuffer != 122 {
		t.Fatalf("tcpTableStatusInsufficientBuffer = %d, want 122 (ERROR_INSUFFICIENT_BUFFER)", tcpTableStatusInsufficientBuffer)
	}
}

func TestBoundedTCPTableQuerySuccess(t *testing.T) {
	// A well-formed single-row IPv4 table: sizing reports the required size,
	// the read reports success and the same written byte count.
	table := tcp4OwnerTable([6]uint32{mibTCPStateListen, addrLoopbackV4, portHTTPS, 0, 0, pidSample})
	query, _ := fakeQuery(t, sizingCall(len(table)), readCall(table))
	data, err := boundedTCPTableQuery(query, tcp4RowSize)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != string(table) {
		t.Fatalf("returned % x, want the %d-byte table % x", data, len(table), table)
	}
}

func TestBoundedTCPTableQueryGrowthThenSuccess(t *testing.T) {
	// The table grew between the sizing call and the read: the read reports
	// ERROR_INSUFFICIENT_BUFFER, the next sizing/read cycle succeeds.
	small := tcp4OwnerTable([6]uint32{mibTCPStateListen, addrLoopbackV4, portHTTPS, 0, 0, pidSample})
	big := append(small, tcp4OwnerTable([6]uint32{mibTCPStateListen, addrLoopbackV4, portEcho, 0, 0, 7})...)
	query, _ := fakeQuery(t,
		sizingCall(len(small)),
		fakeCall{status: tcpTableStatusInsufficientBuffer, size: uint32(len(small))},
		sizingCall(len(big)),
		readCall(big),
	)
	data, err := boundedTCPTableQuery(query, tcp4RowSize)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != string(big) {
		t.Fatalf("returned %d bytes, want the %d-byte grown table", len(data), len(big))
	}
}

func TestBoundedTCPTableQueryChurnIsBounded(t *testing.T) {
	// A table that keeps growing is a changing observation: after the
	// bounded attempt count the driver errors instead of looping.
	script := make([]fakeCall, 0, 2*tcpTableQueryAttempts)
	for i := 0; i < tcpTableQueryAttempts; i++ {
		script = append(script, sizingCall(tcpTableHeaderSize+tcp4RowSize), fakeCall{status: tcpTableStatusInsufficientBuffer})
	}
	query, calls := fakeQuery(t, script...)
	_, err := boundedTCPTableQuery(query, tcp4RowSize)
	if !errors.Is(err, errTCPTable) {
		t.Fatalf("churning table: err = %v, want errTCPTable", err)
	}
	// Bounded: one sizing and one read per attempt, never more.
	sizing, reads := 0, 0
	for _, call := range *calls {
		if call.sizing {
			sizing++
		} else {
			reads++
		}
	}
	if sizing != tcpTableQueryAttempts || reads != tcpTableQueryAttempts {
		t.Fatalf("made %d sizing and %d read calls, want %d each", sizing, reads, tcpTableQueryAttempts)
	}
}

func TestBoundedTCPTableQueryRejectsBadSizes(t *testing.T) {
	for name, tc := range map[string]struct {
		script  []fakeCall
		wantErr error
	}{
		"zero size": {
			script:  []fakeCall{sizingCall(0)},
			wantErr: errTCPTable,
		},
		"too short, header only": {
			script:  []fakeCall{sizingCall(tcpTableHeaderSize)},
			wantErr: errTCPTable,
		},
		"too short, partial row": {
			script:  []fakeCall{sizingCall(tcpTableHeaderSize + tcp4RowSize - 1)},
			wantErr: errTCPTable,
		},
		"oversize": {
			script:  []fakeCall{sizingCall(maxTCPTable + 1)},
			wantErr: errTCPTable,
		},
		"sizing status not insufficient-buffer": {
			script:  []fakeCall{fakeCall{status: 0, size: uint32(tcpTableHeaderSize + tcp4RowSize)}},
			wantErr: errTCPTable,
		},
		"other status on read": {
			script: []fakeCall{
				sizingCall(tcpTableHeaderSize + tcp4RowSize),
				fakeCall{status: 87},
			},
			wantErr: errTCPTable,
		},
		"seam error on sizing": {
			script:  []fakeCall{fakeCall{err: errFakeSeam}},
			wantErr: errFakeSeam,
		},
		"seam error on read": {
			script: []fakeCall{
				sizingCall(tcpTableHeaderSize + tcp4RowSize),
				fakeCall{err: errFakeSeam},
			},
			wantErr: errFakeSeam,
		},
		"success reports more bytes than the buffer": {
			script: []fakeCall{
				sizingCall(tcpTableHeaderSize + tcp4RowSize),
				fakeCall{status: 0, size: uint32(tcpTableHeaderSize + tcp4RowSize + 1)},
			},
			wantErr: errTCPTable,
		},
		"success reports fewer bytes than header and one row": {
			script: []fakeCall{
				sizingCall(tcpTableHeaderSize + tcp4RowSize),
				fakeCall{status: 0, size: uint32(tcpTableHeaderSize)},
			},
			wantErr: errTCPTable,
		},
	} {
		t.Run(name, func(t *testing.T) {
			query, _ := fakeQuery(t, tc.script...)
			data, err := boundedTCPTableQuery(query, tcp4RowSize)
			if err == nil {
				t.Fatalf("returned %d bytes, want an error", len(data))
			}
			// The error must be the expected sentinel: a seam error keeps
			// its cause, every protocol violation wraps errTCPTable. No
			// failure path may read as an empty (absent) result.
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("err = %v, want it to wrap %v (no false absence)", err, tc.wantErr)
			}
		})
	}
}

func TestBoundedTCPTableQueryFeedsParser(t *testing.T) {
	// End to end through the portable surface the adapter uses: the driver's
	// output must decode with the corrected 24-byte row parser.
	table := tcp4OwnerTable(
		[6]uint32{mibTCPStateListen, addrWildcardV4, portEcho, 0, 0, 7},
		[6]uint32{mibTCPStateListen, addrLoopbackV4, portHTTPS, 0, 0, pidSample},
	)
	query, _ := fakeQuery(t, sizingCall(len(table)), readCall(table))
	data, err := boundedTCPTableQuery(query, tcp4RowSize)
	if err != nil {
		t.Fatal(err)
	}
	listeners, err := parseTCP4OwnerTable(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(listeners) != 2 {
		t.Fatalf("decoded %d rows, want 2", len(listeners))
	}
	if listeners[0].localPort != 2001 || listeners[0].pid != 7 || listeners[1].localPort != 443 || listeners[1].pid != pidSample {
		t.Fatalf("decoded %+v, want pid 7 on port 2001 and pid %d on port 443", listeners, pidSample)
	}
}
