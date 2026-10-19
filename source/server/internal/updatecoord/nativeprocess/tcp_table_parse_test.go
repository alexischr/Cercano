package nativeprocess

import (
	"encoding/binary"
	"errors"
	"net/netip"
	"slices"
	"testing"
)

// These tests are portable: they decode raw GetExtendedTcpTable buffers
// against the MIB_TCPROW_OWNER_PID (24-byte) and MIB_TCP6ROW_OWNER_PID
// (56-byte) layouts and must pass on every host platform without ever
// calling the Windows API.

// tcp4Set writes a 32-bit field into one IPv4 owner-table row.
func tcp4Set(buf []byte, row int, offset int, value uint32) {
	binary.LittleEndian.PutUint32(buf[tcpTableHeaderSize+row*tcp4RowSize+offset:], value)
}

// tcp4OwnerTable builds a well-formed MIB_TCPTABLE_OWNER_PID buffer from raw
// 32-bit words: state, local address, local port, remote address, remote
// port, owning PID.
func tcp4OwnerTable(words ...[6]uint32) []byte {
	buf := make([]byte, tcpTableHeaderSize+len(words)*tcp4RowSize)
	binary.LittleEndian.PutUint32(buf, uint32(len(words)))
	for i, w := range words {
		tcp4Set(buf, i, 0, w[0])
		tcp4Set(buf, i, 4, w[1])
		tcp4Set(buf, i, 8, w[2])
		tcp4Set(buf, i, 12, w[3])
		tcp4Set(buf, i, 16, w[4])
		tcp4Set(buf, i, 20, w[5])
	}
	return buf
}

// tcp6OwnerRow writes one MIB_TCP6ROW_OWNER_PID row into buf at the given
// index: local address (network byte order), local scope ID, local port,
// remote address, remote scope ID, remote port, state, owning PID.
func tcp6OwnerRow(buf []byte, row int, localAddr [16]byte, localPort uint32, remoteAddr [16]byte, remoteScopeID, remotePort, state, pid uint32) {
	base := tcpTableHeaderSize + row*tcp6RowSize
	copy(buf[base+tcp6RowLocalAddr:], localAddr[:])
	binary.LittleEndian.PutUint32(buf[base+16:], 0) // dwLocalScopeId
	binary.LittleEndian.PutUint32(buf[base+tcp6RowLocalPort:], localPort)
	copy(buf[base+24:], remoteAddr[:])
	binary.LittleEndian.PutUint32(buf[base+40:], remoteScopeID)
	binary.LittleEndian.PutUint32(buf[base+44:], remotePort)
	binary.LittleEndian.PutUint32(buf[base+tcp6RowState:], state)
	binary.LittleEndian.PutUint32(buf[base+tcp6RowPID:], pid)
}

// tcp6OwnerTable builds a well-formed MIB_TCP6TABLE_OWNER_PID buffer.
func tcp6OwnerTable(rows ...struct {
	localAddr [16]byte
	localPort uint32
	pid       uint32
}) []byte {
	buf := make([]byte, tcpTableHeaderSize+len(rows)*tcp6RowSize)
	binary.LittleEndian.PutUint32(buf, uint32(len(rows)))
	for i, r := range rows {
		tcp6OwnerRow(buf, i, r.localAddr, r.localPort, [16]byte{}, 0, 0, mibTCPStateListen, r.pid)
	}
	return buf
}

// Address and port field values use the documented on-memory layouts:
// dwLocalAddr stores the IPv4 address in network byte order (127.0.0.1 is
// 0x0100007F read little-endian, identical to the /proc hex spelling), and
// dwLocalPort stores the port network-byte-order in the low-addressed
// bytes (port 443/0x01BB becomes the little-endian word 0xBB01).
const (
	addrLoopbackV4 = 0x0100007F
	addrWildcardV4 = 0
	portHTTPS      = 0xBB01 // (u_short)0xBB01 == 0x01BB == 443
	portEcho       = 0xD107 // (u_short)0xD107 == 0x07D1 == 2001
	pidSample      = 4242
)

func addrLoopbackV6() [16]byte {
	var a [16]byte
	a[15] = 1 // ::1
	return a
}

func addrWildcardV6() [16]byte { return [16]byte{} }

func equalListeners(a, b []tcpOwnerListener) bool {
	return slices.EqualFunc(a, b, func(x, y tcpOwnerListener) bool {
		return x.localAddr == y.localAddr && x.localPort == y.localPort && x.pid == y.pid
	})
}

func TestParseTCP4OwnerTableDecodesRows(t *testing.T) {
	listeners, err := parseTCP4OwnerTable(tcp4OwnerTable(
		[6]uint32{mibTCPStateListen, addrLoopbackV4, portHTTPS, 0, 0, pidSample},
		[6]uint32{mibTCPStateListen, addrWildcardV4, portEcho, 0, 0, 7},
	))
	if err != nil {
		t.Fatal(err)
	}
	want := []tcpOwnerListener{
		{localAddr: netip.MustParseAddr("127.0.0.1"), localPort: 443, pid: pidSample},
		{localAddr: netip.MustParseAddr("0.0.0.0"), localPort: 2001, pid: 7},
	}
	if !equalListeners(listeners, want) {
		t.Fatalf("decoded %+v, want %+v", listeners, want)
	}
}

func TestParseTCP6OwnerTableDecodesRows(t *testing.T) {
	// MIB_TCP6ROW_OWNER_PID: ucLocalAddr[16], dwLocalScopeId, dwLocalPort,
	// ucRemoteAddr[16], dwRemoteScopeId, dwRemotePort, dwState, dwOwningPid
	// — 56 bytes, no trailing timestamp field.
	listeners, err := parseTCP6OwnerTable(tcp6OwnerTable(
		struct {
			localAddr [16]byte
			localPort uint32
			pid       uint32
		}{addrLoopbackV6(), portHTTPS, 4242},
		struct {
			localAddr [16]byte
			localPort uint32
			pid       uint32
		}{addrWildcardV6(), portEcho, 7},
	))
	if err != nil {
		t.Fatal(err)
	}
	want := []tcpOwnerListener{
		{localAddr: netip.MustParseAddr("::1"), localPort: 443, pid: 4242},
		{localAddr: netip.MustParseAddr("::"), localPort: 2001, pid: 7},
	}
	if !equalListeners(listeners, want) {
		t.Fatalf("decoded %+v, want %+v", listeners, want)
	}
}

func TestDecodeTCPTablePortUsesNetworkByteOrder(t *testing.T) {
	for _, tc := range []struct {
		word uint32
		port uint16
	}{
		{0xBB01, 443},
		{0xD107, 2001},
		{0xEF0B, 3055},
		{0x0BEF, 61195}, // swapped 0x0BEF -> 0xEF0B
	} {
		if got := decodeTCPTablePort(tc.word); got != tc.port {
			t.Fatalf("decodeTCPTablePort(%#x) = %d, want %d", tc.word, got, tc.port)
		}
	}
}

// TestDecodeTCPTablePortIgnoresPaddingWords pins the documented decoding:
// only the first two network-order bytes of the port DWORD carry the port
// and the API does not define the unused bytes, so undefined padding must
// never change the decoded value or be rejected.
func TestDecodeTCPTablePortIgnoresPaddingWords(t *testing.T) {
	// Memory bytes 01 BB ?? ?? (port 443, undefined padding) read
	// little-endian as 0x????BB01; the port must still decode as 443.
	for _, word := range []uint32{0xADDEBB01, 0xFFFFBB01, 0x0000BB01} {
		if got := decodeTCPTablePort(word); got != 443 {
			t.Errorf("decodeTCPTablePort(%#x) = %d, want 443 despite padding", word, got)
		}
	}
}

func TestParseTCP4OwnerTableRejectsMalformedFraming(t *testing.T) {
	for name, data := range map[string][]byte{
		"empty":               {},
		"partial header":      {0x02, 0x00, 0x00},
		"count beyond buffer": tcp4OwnerTable([6]uint32{mibTCPStateListen, addrLoopbackV4, portHTTPS, 0, 0, pidSample}, [6]uint32{mibTCPStateListen, addrLoopbackV4, portHTTPS, 0, 0, pidSample})[:tcpTableHeaderSize+tcp4RowSize+4],
		"partial last row":    tcp4OwnerTable([6]uint32{mibTCPStateListen, addrLoopbackV4, portHTTPS, 0, 0, pidSample})[:tcpTableHeaderSize+tcp4RowSize-1],
		"oversized row count": append([]byte{0xFF, 0xFF, 0xFF, 0x7F}, make([]byte, tcp4RowSize)...),
	} {
		if _, err := parseTCP4OwnerTable(data); !errors.Is(err, errTCPTable) {
			t.Errorf("%s: parse error = %v, want errTCPTable", name, err)
		}
	}
}

func TestParseTCP4OwnerTableRejectsInvalidRows(t *testing.T) {
	for name, row := range map[string][6]uint32{
		"non-listen state":  {1, addrLoopbackV4, portHTTPS, 0, 0, pidSample},
		"established state": {5, addrLoopbackV4, portHTTPS, 0, 0, pidSample},
		"zero port":         {mibTCPStateListen, addrLoopbackV4, 0, 0, 0, pidSample},
		"zero pid":          {mibTCPStateListen, addrLoopbackV4, portHTTPS, 0, 0, 0},
		"negative pid":      {mibTCPStateListen, addrLoopbackV4, portHTTPS, 0, 0, 0x80000000},
	} {
		_, err := parseTCP4OwnerTable(tcp4OwnerTable(row))
		switch name {
		case "zero pid":
			if !errors.Is(err, errTCPTableAttribution) {
				t.Errorf("%s: parse error = %v, want errTCPTableAttribution", name, err)
			}
		default:
			if !errors.Is(err, errTCPTable) {
				t.Errorf("%s: parse error = %v, want errTCPTable", name, err)
			}
		}
	}
	// A single bad row invalidates the whole table: it can never silently
	// read as absence of the other rows.
	if _, err := parseTCP4OwnerTable(tcp4OwnerTable(
		[6]uint32{mibTCPStateListen, addrLoopbackV4, portHTTPS, 0, 0, pidSample},
		[6]uint32{mibTCPStateListen, addrLoopbackV4, portEcho, 0, 0, 0},
	)); !errors.Is(err, errTCPTableAttribution) {
		t.Fatalf("second-row zero PID accepted: %v", err)
	}
}

func TestParseTCP6OwnerTableRejectsMalformedFraming(t *testing.T) {
	full := tcp6OwnerTable(struct {
		localAddr [16]byte
		localPort uint32
		pid       uint32
	}{addrLoopbackV6(), portHTTPS, 4242})

	for name, data := range map[string][]byte{
		"empty":               {},
		"partial header":      {0x01, 0x00},
		"count beyond buffer": append(append([]byte{2, 0, 0, 0}, full[tcpTableHeaderSize:]...), 4),
		"partial last row":    full[:tcpTableHeaderSize+tcp6RowSize-8],
		"one byte short":      full[:tcpTableHeaderSize+tcp6RowSize-1],
		"oversized count":     append([]byte{0xFF, 0xFF, 0xFF, 0x7F}, full[tcpTableHeaderSize:]...),
	} {
		if _, err := parseTCP6OwnerTable(data); !errors.Is(err, errTCPTable) {
			t.Errorf("%s: parse error = %v, want errTCPTable", name, err)
		}
	}
}

func TestParseTCP6OwnerTableRejectsInvalidRows(t *testing.T) {
	build := func(mutate func(row []byte)) []byte {
		buf := tcp6OwnerTable(struct {
			localAddr [16]byte
			localPort uint32
			pid       uint32
		}{addrLoopbackV6(), portHTTPS, 4242})
		mutate(buf[tcpTableHeaderSize:])
		return buf
	}
	for name, tc := range map[string]struct {
		data    []byte
		wantErr error
	}{
		"non-listen state": {build(func(row []byte) {
			binary.LittleEndian.PutUint32(row[tcp6RowState:], 12)
		}), errTCPTable},
		"zero port": {build(func(row []byte) {
			binary.LittleEndian.PutUint32(row[tcp6RowLocalPort:], 0)
		}), errTCPTable},
		"zero pid": {build(func(row []byte) {
			binary.LittleEndian.PutUint32(row[tcp6RowPID:], 0)
		}), errTCPTableAttribution},
		"negative pid": {build(func(row []byte) {
			binary.LittleEndian.PutUint32(row[tcp6RowPID:], 0xFFFFFFFF)
		}), errTCPTable},
	} {
		if _, err := parseTCP6OwnerTable(tc.data); !errors.Is(err, tc.wantErr) {
			t.Errorf("%s: parse error = %v, want %v", name, err, tc.wantErr)
		}
	}
}

func TestParseTCPOwnerTablesTolerateShrunkTableTrailingBytes(t *testing.T) {
	// The Windows reader allocates from a size query that can race a table
	// that shrinks before the read; trailing bytes beyond the header count
	// are stale capacity, not missing rows, so the declared count wins.
	data := append(tcp4OwnerTable([6]uint32{mibTCPStateListen, addrLoopbackV4, portHTTPS, 0, 0, pidSample}), make([]byte, tcp4RowSize)...)
	listeners, err := parseTCP4OwnerTable(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(listeners) != 1 || listeners[0].pid != pidSample {
		t.Fatalf("unexpected rows: %+v", listeners)
	}
}

func TestParseTCPOwnerTableAcceptsEmptyTable(t *testing.T) {
	empty := []byte{0, 0, 0, 0}
	if listeners, err := parseTCP4OwnerTable(empty); err != nil || len(listeners) != 0 {
		t.Fatal("empty IPv4 table rejected", listeners, err)
	}
	if listeners, err := parseTCP6OwnerTable(empty); err != nil || len(listeners) != 0 {
		t.Fatal("empty IPv6 table rejected", listeners, err)
	}
}

// TestParseTCP4OwnerTableUsesDocumentedOwnerRowLayout pins the parser to the
// documented MIB_TCPROW_OWNER_PID ABI: six DWORDs, 24 bytes per row, with no
// trailing liCreateTimestamp (that field belongs to the OWNER_MODULE row). A
// parser sized to a 32-byte OWNER_MODULE stride would reject this well-formed
// two-row table on its count check and misframe every row after the first.
func TestParseTCP4OwnerTableUsesDocumentedOwnerRowLayout(t *testing.T) {
	const abiTCP4OwnerRowSize = 24 // exactly the six DWORD fields
	buf := make([]byte, tcpTableHeaderSize+2*abiTCP4OwnerRowSize)
	binary.LittleEndian.PutUint32(buf, 2)
	for i, pid := range []uint32{111, 222} {
		row := buf[tcpTableHeaderSize+i*abiTCP4OwnerRowSize:]
		binary.LittleEndian.PutUint32(row[0:], mibTCPStateListen)
		binary.LittleEndian.PutUint32(row[4:], addrLoopbackV4)
		binary.LittleEndian.PutUint32(row[8:], portHTTPS)
		binary.LittleEndian.PutUint32(row[12:], 0)
		binary.LittleEndian.PutUint32(row[16:], 0)
		binary.LittleEndian.PutUint32(row[20:], pid)
	}
	listeners, err := parseTCP4OwnerTable(buf)
	if err != nil {
		t.Fatalf("documented-ABI IPv4 owner table rejected: %v", err)
	}
	if len(listeners) != 2 {
		t.Fatalf("decoded %d rows, want 2", len(listeners))
	}
	for i, want := range []uint32{111, 222} {
		got := listeners[i]
		if got.pid != want || got.localPort != 443 || got.localAddr != netip.MustParseAddr("127.0.0.1") {
			t.Fatalf("row %d decoded %+v, want pid %d on 127.0.0.1:443", i, got, want)
		}
	}
}

// TestParseTCP6OwnerTableUsesDocumentedOwnerRowLayout pins the IPv6 ABI the
// same way: ucLocalAddr[16], dwLocalScopeId, dwLocalPort, ucRemoteAddr[16],
// dwRemoteScopeId, dwRemotePort, dwState, dwOwningPid — 56 bytes, and the
// second row's state must not be read out of the first row's remote fields.
func TestParseTCP6OwnerTableUsesDocumentedOwnerRowLayout(t *testing.T) {
	const abiTCP6OwnerRowSize = 56
	buf := make([]byte, tcpTableHeaderSize+2*abiTCP6OwnerRowSize)
	binary.LittleEndian.PutUint32(buf, 2)
	for i, pid := range []uint32{111, 222} {
		tcp6OwnerRow(buf, i, addrLoopbackV6(), portHTTPS, [16]byte{}, 0, 0, mibTCPStateListen, pid)
	}
	listeners, err := parseTCP6OwnerTable(buf)
	if err != nil {
		t.Fatalf("documented-ABI IPv6 owner table rejected: %v", err)
	}
	if len(listeners) != 2 {
		t.Fatalf("decoded %d rows, want 2", len(listeners))
	}
	for i, want := range []uint32{111, 222} {
		got := listeners[i]
		if got.pid != want || got.localPort != 443 || got.localAddr != netip.MustParseAddr("::1") {
			t.Fatalf("row %d decoded %+v, want pid %d on [::1]:443", i, got, want)
		}
	}
}

func TestParseTCPOwnerTableRowArithmeticBounds(t *testing.T) {
	// The largest count whose rows still fit the buffer must parse without
	// overrunning slice bounds; one row more must fail closed. Bounded
	// sizes, not raw kernel counts, cap allocation.
	const rows = 8
	data := make([]byte, tcpTableHeaderSize+rows*tcp4RowSize)
	binary.LittleEndian.PutUint32(data, rows)
	for i := 0; i < rows; i++ {
		tcp4Set(data, i, 0, mibTCPStateListen)
		tcp4Set(data, i, 4, addrLoopbackV4)
		tcp4Set(data, i, 8, portHTTPS)
		tcp4Set(data, i, 20, pidSample)
	}
	listeners, err := parseTCP4OwnerTable(data)
	if err != nil || len(listeners) != rows {
		t.Fatal("full bounded table rejected", len(listeners), err)
	}
	binary.LittleEndian.PutUint32(data, rows+1)
	if _, err := parseTCP4OwnerTable(data); !errors.Is(err, errTCPTable) {
		t.Fatal("count beyond bounds accepted", err)
	}
}

// TestParseTCPOwnerTableRejectsOversizedBuffer bounds the whole decode: a
// buffer beyond maxTCPOwnerTableBytes is rejected before any count-derived
// allocation, even when its header count would otherwise be consistent.
func TestParseTCPOwnerTableRejectsOversizedBuffer(t *testing.T) {
	oversized := make([]byte, maxTCPOwnerTableBytes+1)
	binary.LittleEndian.PutUint32(oversized, 0)
	if _, err := parseTCP4OwnerTable(oversized); !errors.Is(err, errTCPTable) {
		t.Fatalf("oversized IPv4 buffer accepted: %v", err)
	}
	if _, err := parseTCP6OwnerTable(oversized); !errors.Is(err, errTCPTable) {
		t.Fatalf("oversized IPv6 buffer accepted: %v", err)
	}
	// Exactly at the bound is still decodable.
	atBound := make([]byte, maxTCPOwnerTableBytes)
	binary.LittleEndian.PutUint32(atBound, 0)
	if _, err := parseTCP4OwnerTable(atBound); err != nil {
		t.Fatalf("buffer at the 16 MiB bound rejected: %v", err)
	}
}

// TestParseTCPOwnerTableRowCountCannotOverflowAllocation drives the count
// field with values whose count*rowSize product would overflow a naive
// int32/int multiplication; the derived bound must reject them all.
func TestParseTCPOwnerTableRowCountCannotOverflowAllocation(t *testing.T) {
	for _, count := range []uint32{0xFFFFFFFF, 0x80000000, 0x40000000} {
		data := make([]byte, tcpTableHeaderSize)
		binary.LittleEndian.PutUint32(data, count)
		if _, err := parseTCP4OwnerTable(data); !errors.Is(err, errTCPTable) {
			t.Fatalf("count %#x accepted", count)
		}
		if _, err := parseTCP6OwnerTable(data); !errors.Is(err, errTCPTable) {
			t.Fatalf("IPv6 count %#x accepted", count)
		}
	}
}

func TestQueryTCPOwnerListenerValidatesEndpointAndPID(t *testing.T) {
	rows4 := []tcpOwnerListener{{localAddr: netip.MustParseAddr("127.0.0.1"), localPort: 443, pid: pidSample}}
	for name, endpoint := range map[string]netip.AddrPort{
		"zero value":    {},
		"non-loopback":  netip.MustParseAddrPort("192.0.2.1:443"),
		"zero port":     netip.MustParseAddrPort("127.0.0.1:0"),
		"zoned address": netip.MustParseAddrPort("[::1%lo]:443"),
	} {
		if _, _, err := queryTCPOwnerListener(rows4, nil, endpoint, pidSample); err == nil {
			t.Errorf("%s: invalid endpoint accepted", name)
		}
	}
	if _, _, err := queryTCPOwnerListener(rows4, nil, netip.MustParseAddrPort("127.0.0.1:443"), 0); err == nil {
		t.Error("zero expected PID accepted")
	}
	if _, _, err := queryTCPOwnerListener(rows4, nil, netip.MustParseAddrPort("127.0.0.1:443"), -1); err == nil {
		t.Error("negative expected PID accepted")
	}
}

func TestQueryTCPOwnerListenerMatchesIPv4(t *testing.T) {
	endpoint := netip.MustParseAddrPort("127.0.0.1:443")
	// Exact loopback address, right PID and port.
	owned, uncertain, err := queryTCPOwnerListener([]tcpOwnerListener{
		{localAddr: netip.MustParseAddr("127.0.0.1"), localPort: 443, pid: pidSample},
	}, nil, endpoint, pidSample)
	if err != nil || !owned || uncertain {
		t.Fatalf("exact IPv4 match: owned=%v uncertain=%v err=%v", owned, uncertain, err)
	}
	// Same-family wildcard also matches.
	owned, uncertain, err = queryTCPOwnerListener([]tcpOwnerListener{
		{localAddr: netip.MustParseAddr("0.0.0.0"), localPort: 443, pid: pidSample},
	}, nil, endpoint, pidSample)
	if err != nil || !owned || uncertain {
		t.Fatalf("IPv4 wildcard match: owned=%v uncertain=%v err=%v", owned, uncertain, err)
	}
	// Wrong owner on the right address/port is decided absence for this
	// candidate, never ownership and never uncertainty.
	owned, uncertain, err = queryTCPOwnerListener([]tcpOwnerListener{
		{localAddr: netip.MustParseAddr("127.0.0.1"), localPort: 443, pid: 9999},
	}, nil, endpoint, pidSample)
	if err != nil || owned || uncertain {
		t.Fatalf("wrong owner: owned=%v uncertain=%v err=%v", owned, uncertain, err)
	}
	// Wrong port never matches, whatever the PID.
	owned, uncertain, err = queryTCPOwnerListener([]tcpOwnerListener{
		{localAddr: netip.MustParseAddr("127.0.0.1"), localPort: 8443, pid: pidSample},
	}, nil, endpoint, pidSample)
	if err != nil || owned || uncertain {
		t.Fatalf("wrong port: owned=%v uncertain=%v err=%v", owned, uncertain, err)
	}
	// A different loopback address on the same port is not a match.
	owned, uncertain, err = queryTCPOwnerListener([]tcpOwnerListener{
		{localAddr: netip.MustParseAddr("127.0.0.2"), localPort: 443, pid: pidSample},
	}, nil, endpoint, pidSample)
	if err != nil || owned || uncertain {
		t.Fatalf("different address: owned=%v uncertain=%v err=%v", owned, uncertain, err)
	}
	// An IPv6-only row can never decide an IPv4 endpoint.
	owned, uncertain, err = queryTCPOwnerListener(nil, []tcpOwnerListener{
		{localAddr: netip.MustParseAddr("::1"), localPort: 443, pid: pidSample},
	}, endpoint, pidSample)
	if err != nil || owned || uncertain {
		t.Fatalf("IPv6 row vs IPv4 endpoint: owned=%v uncertain=%v err=%v", owned, uncertain, err)
	}
}

func TestQueryTCPOwnerListenerMatchesIPv6(t *testing.T) {
	endpoint := netip.MustParseAddrPort("[::1]:443")
	owned, uncertain, err := queryTCPOwnerListener(nil, []tcpOwnerListener{
		{localAddr: netip.MustParseAddr("::1"), localPort: 443, pid: pidSample},
	}, endpoint, pidSample)
	if err != nil || !owned || uncertain {
		t.Fatalf("exact IPv6 match: owned=%v uncertain=%v err=%v", owned, uncertain, err)
	}
	// Same-family IPv6 wildcard matches an IPv6 endpoint.
	owned, uncertain, err = queryTCPOwnerListener(nil, []tcpOwnerListener{
		{localAddr: netip.MustParseAddr("::"), localPort: 443, pid: pidSample},
	}, endpoint, pidSample)
	if err != nil || !owned || uncertain {
		t.Fatalf("IPv6 wildcard match: owned=%v uncertain=%v err=%v", owned, uncertain, err)
	}
	owned, uncertain, err = queryTCPOwnerListener(nil, []tcpOwnerListener{
		{localAddr: netip.MustParseAddr("::1"), localPort: 443, pid: 9999},
	}, endpoint, pidSample)
	if err != nil || owned || uncertain {
		t.Fatalf("wrong owner: owned=%v uncertain=%v err=%v", owned, uncertain, err)
	}
	owned, uncertain, err = queryTCPOwnerListener(nil, []tcpOwnerListener{
		{localAddr: netip.MustParseAddr("::1"), localPort: 8443, pid: pidSample},
	}, endpoint, pidSample)
	if err != nil || owned || uncertain {
		t.Fatalf("wrong port: owned=%v uncertain=%v err=%v", owned, uncertain, err)
	}
}

// TestQueryTCPOwnerListenerIPv6WildcardVsIPv4IsUncertain pins the dual-stack
// boundary: a same-PID, same-port IPv6 wildcard row cannot verify an IPv4
// endpoint because the tables do not expose IPV6_V6ONLY, so it is reported
// as uncertainty and never as positive ownership — and a foreign PID's
// wildcard is not even uncertainty for this candidate.
func TestQueryTCPOwnerListenerIPv6WildcardVsIPv4IsUncertain(t *testing.T) {
	endpoint := netip.MustParseAddrPort("127.0.0.1:443")
	owned, uncertain, err := queryTCPOwnerListener(nil, []tcpOwnerListener{
		{localAddr: netip.MustParseAddr("::"), localPort: 443, pid: pidSample},
	}, endpoint, pidSample)
	if err != nil || owned || !uncertain {
		t.Fatalf("IPv6 wildcard vs IPv4 endpoint: owned=%v uncertain=%v err=%v", owned, uncertain, err)
	}
	owned, uncertain, err = queryTCPOwnerListener(nil, []tcpOwnerListener{
		{localAddr: netip.MustParseAddr("::"), localPort: 443, pid: 9999},
	}, endpoint, pidSample)
	if err != nil || owned || uncertain {
		t.Fatalf("foreign IPv6 wildcard vs IPv4 endpoint: owned=%v uncertain=%v err=%v", owned, uncertain, err)
	}
	// An exact IPv4 match makes the answer positive regardless of any
	// coexisting wildcard uncertainty.
	owned, uncertain, err = queryTCPOwnerListener([]tcpOwnerListener{
		{localAddr: netip.MustParseAddr("127.0.0.1"), localPort: 443, pid: pidSample},
	}, []tcpOwnerListener{
		{localAddr: netip.MustParseAddr("::"), localPort: 443, pid: pidSample},
	}, endpoint, pidSample)
	if err != nil || !owned {
		t.Fatalf("exact match alongside wildcard: owned=%v uncertain=%v err=%v", owned, uncertain, err)
	}
}
