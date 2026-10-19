package nativeprocess

import (
	"encoding/binary"
	"fmt"
	"io"
	"net/netip"
	"strings"
	"testing"
)

func TestProcStartParsing(t *testing.T) {
	suffix := "S " + strings.Repeat("0 ", 18) + "12345 0 0\n"
	for _, comm := range []string{"normal", "a ) b (c)", "space\nand newline"} {
		got, e := parseProcStart([]byte("42 ("+comm+") "+suffix), 42)
		if e != nil || got != 12345 {
			t.Fatal(got, e)
		}
	}
	for _, s := range []string{"", "42 (a) S", "41 (a) " + suffix, "42 a) " + suffix, "42 (a) S " + strings.Repeat("0 ", 18) + "18446744073709551616", "42 (a) S " + strings.Repeat("0 ", 18) + "0"} {
		if _, e := parseProcStart([]byte(s), 42); e == nil {
			t.Fatal("invalid stat accepted", s)
		}
	}
}

func TestProcEffectiveUIDParsing(t *testing.T) {
	got, e := parseProcUID([]byte("Name:\ttest\nUid:\t1\t2\t3\t4\n"))
	if e != nil || got != 2 {
		t.Fatal(got, e)
	}
	for _, s := range []string{"", "Uid: 1 2 3", "Uid: 1 -2 3 4", "Uid: 1 4294967296 3 4", "Uid: 1 2 3 4\nUid: 1 2 3 4"} {
		if _, e := parseProcUID([]byte(s)); e == nil {
			t.Fatal("invalid status accepted", s)
		}
	}
}

func TestNativeIdentityIncludesLinuxLifetime(t *testing.T) {
	first := Identity{PID: 42, UID: 1, Executable: "/bin/a", StartTicks: 123}
	second := first
	second.StartTicks++
	if first.SameProcess(second) {
		t.Fatal("PID reuse accepted")
	}
}

// procHexAddress renders an address the way the kernel prints it: 32-bit
// words in host byte order, so the fixtures stay valid on any endianness.
func procHexAddress(addr netip.Addr) string {
	raw := addr.As16()
	if addr.Is4() {
		return fmt.Sprintf("%08X", binary.NativeEndian.Uint32(raw[12:]))
	}
	var b strings.Builder
	for i := 0; i < 4; i++ {
		fmt.Fprintf(&b, "%08X", binary.NativeEndian.Uint32(raw[i*4:]))
	}
	return b.String()
}

func procTCPRecord(local netip.AddrPort, state string, inode uint64) string {
	return fmt.Sprintf("  2: %s:%04X 00000000:0000 %s 00000000:00000000 00:00000000 00000000 0        0 %d 1 0000 0\n",
		procHexAddress(local.Addr()), local.Port(), state, inode)
}

const procTCPHeader = "  sl  local_address rem_address   st tx_queue rx_queue tr tm->when retrnsmt   uid  timeout inode\n"

// procTCP6Header spells the header exactly as the kernel prints it in
// net/ipv6/tcp_ipv6.c: "remote_address", not tcp_ipv4.c's "rem_address".
const procTCP6Header = "  sl  local_address                         remote_address                        st tx_queue rx_queue tr tm->when retrnsmt   uid  timeout inode\n"

func collectTCP(t *testing.T, table string, isIPv6 bool, endpoint netip.AddrPort) (matches, ambiguous map[uint64]bool, err error) {
	t.Helper()
	matches = map[uint64]bool{}
	ambiguous = map[uint64]bool{}
	err = collectListenerInodes([]byte(table), isIPv6, endpoint, matches, ambiguous)
	return matches, ambiguous, err
}

func TestProcTCPListenerTableParsing(t *testing.T) {
	loopback := netip.MustParseAddrPort("127.0.0.1:12345")
	table := procTCPHeader +
		procTCPRecord(loopback, "0A", 4021) +
		procTCPRecord(netip.MustParseAddrPort("127.0.0.1:9999"), "0A", 4022) +
		procTCPRecord(loopback, "01", 4023) + // established, not listening
		procTCPRecord(netip.MustParseAddrPort("0.0.0.0:12345"), "0A", 4024)
	matches, ambiguous, e := collectTCP(t, table, false, loopback)
	if e != nil || len(ambiguous) != 0 || !matches[4021] || !matches[4024] || matches[4022] || matches[4023] {
		t.Fatal(matches, ambiguous, e)
	}
	// A wildcard IPv4 socket serves loopback, but never a different port.
	other := netip.MustParseAddrPort("127.0.0.1:4040")
	matches, ambiguous, e = collectTCP(t, table, false, other)
	if e != nil || len(matches) != 0 || len(ambiguous) != 0 {
		t.Fatal(matches, ambiguous, e)
	}
}

func TestProcTCP6ListenerTableParsing(t *testing.T) {
	v6loopback := netip.MustParseAddrPort("[::1]:8080")
	// Real /proc/net/tcp6 framing: the tcp6 header names the peer column
	// "remote_address" (net/ipv6/tcp_ipv6.c), unlike tcp's "rem_address".
	table := procTCP6Header +
		procTCPRecord(v6loopback, "0A", 5101) +
		procTCPRecord(netip.MustParseAddrPort("[::]:8080"), "0A", 5102) +
		procTCPRecord(netip.MustParseAddrPort("[::]:9090"), "0A", 5103)
	matches, ambiguous, e := collectTCP(t, table, true, v6loopback)
	if e != nil || len(ambiguous) != 0 || !matches[5101] || !matches[5102] || matches[5103] || len(matches) != 2 {
		t.Fatal(matches, ambiguous, e)
	}
	// An IPv6 wildcard may serve an IPv6 loopback endpoint.
	v6any := netip.MustParseAddrPort("[::1]:9090")
	matches, ambiguous, e = collectTCP(t, table, true, v6any)
	if e != nil || !matches[5103] || len(ambiguous) != 0 {
		t.Fatal(matches, ambiguous, e)
	}
	// An IPv6 wildcard cannot be proven to serve IPv4 from /proc alone:
	// the record must surface as explicit ambiguity, never as a match or a
	// clean miss.
	v4endpoint := netip.MustParseAddrPort("127.0.0.1:8080")
	matches, ambiguous, e = collectTCP(t, table, true, v4endpoint)
	if e != nil || len(matches) != 0 || !ambiguous[5102] || len(ambiguous) != 1 {
		t.Fatal(matches, ambiguous, e)
	}
}

// TestProcTCP6KernelHeaderParsing feeds the parser a table whose header is
// byte-for-byte the one the kernel prints in net/ipv6/tcp_ipv6.c, which
// names the peer column "remote_address" — unlike net/ipv4/tcp_ipv4.c's
// "rem_address". Rows are real /proc/net/tcp6 LISTEN records.
func TestProcTCP6KernelHeaderParsing(t *testing.T) {
	v6loopback := netip.MustParseAddrPort("[::1]:8080")
	table := procTCP6Header +
		procTCPRecord(v6loopback, "0A", 5101) +
		procTCPRecord(netip.MustParseAddrPort("[::]:8080"), "0A", 5102)
	matches, ambiguous, e := collectTCP(t, table, true, v6loopback)
	if e != nil || len(ambiguous) != 0 || !matches[5101] || !matches[5102] || len(matches) != 2 {
		t.Fatal(matches, ambiguous, e)
	}
	// The IPv4 header is not acceptable framing for the IPv6 table, and
	// vice versa: a parser that accepts either header for either family
	// is not reading the table it was given.
	if _, _, e = collectTCP(t, table, false, v6loopback); e == nil {
		t.Fatal("IPv6 header accepted for IPv4 table")
	}
	v4table := procTCPHeader + procTCPRecord(netip.MustParseAddrPort("127.0.0.1:8080"), "0A", 5103)
	if _, _, e = collectTCP(t, v4table, true, v6loopback); e == nil {
		t.Fatal("IPv4 header accepted for IPv6 table")
	}
}

// A read truncated mid-record leaves a tail with no final newline. The
// kernel terminates every table row with '\n', so a missing one is corrupt
// framing and must fail closed — never parse the dangling fragment as if
// it were a complete record.
func TestProcTCPTableRejectsTruncatedTail(t *testing.T) {
	endpoint := netip.MustParseAddrPort("127.0.0.1:12345")
	complete := procTCPHeader + procTCPRecord(endpoint, "0A", 4021)
	if matches, ambiguous, e := collectTCP(t, complete, false, endpoint); e != nil || !matches[4021] || len(ambiguous) != 0 {
		t.Fatal("well-formed table rejected", matches, ambiguous, e)
	}
	for name, table := range map[string]string{
		"cut mid row":       complete[:len(complete)-5],
		"cut after inode":   procTCPHeader + strings.TrimRight(procTCPRecord(endpoint, "0A", 4021), "\n"),
		"cut in header":     strings.TrimRight(procTCPHeader, "\n"),
		"blank trailing ln": procTCPHeader + "\n",
	} {
		if _, _, e := collectTCP(t, table, false, endpoint); e == nil {
			t.Fatal("truncated tail accepted:", name)
		}
	}
}

// A real-format TIME_WAIT row legitimately carries inode 0: the kernel
// prints the socket's owning inode and a closed connection's socket has
// none left to print. Such a row must parse without error, and its zero
// inode must never become a match or an ambiguity. A complete header with
// no rows is a legitimate empty table and must likewise stay accepted.
func TestProcTCPTimeWaitZeroInodeIsWellFormed(t *testing.T) {
	endpoint := netip.MustParseAddrPort("127.0.0.1:12345")
	table := procTCPHeader +
		"   4: 0100007F:3039 0100007F:C350 06 00000000:00000001 03:00000B52 00000000     0        0 0 3 00000000 00000000 0 0\n"
	matches, ambiguous, e := collectTCP(t, table, false, endpoint)
	if e != nil || len(matches) != 0 || len(ambiguous) != 0 {
		t.Fatal(matches, ambiguous, e)
	}
	if matches, ambiguous, e = collectTCP(t, procTCPHeader, false, endpoint); e != nil || len(matches) != 0 || len(ambiguous) != 0 {
		t.Fatal("empty well-formed table rejected", matches, ambiguous, e)
	}
}

func TestProcTCPTableRejectsMalformedRecords(t *testing.T) {
	endpoint := netip.MustParseAddrPort("127.0.0.1:12345")
	records := map[string]string{
		"missing header":      procTCPRecord(endpoint, "0A", 1),
		"wrong header":        "  id  local rem\n" + procTCPRecord(endpoint, "0A", 1),
		"truncated header":    "sl",
		"partial header":      "  sl  local_address rem_address   st",
		"truncated line":      procTCPHeader + "  2: 0100007F:3039\n",
		"short line":          procTCPHeader + "  2: 0100007F:3039 00000000:0000 0A\n",
		"bad state":           procTCPHeader + procTCPRecord(endpoint, "ZZ", 1),
		"bad address hex":     procTCPHeader + "  2: 010000G:3039 00000000:0000 0A 0 0 0 0 0 1\n",
		"bad v6 address size": procTCPHeader + "  2: 0000000000000000000000000100000:3039 00000000:0000 0A 0 0 0 0 0 1\n",
		"zero port":           procTCPHeader + "  2: 0100007F:0000 00000000:0000 0A 0 0 0 0 0 1\n",
		"zero inode":          procTCPHeader + procTCPRecord(endpoint, "0A", 0),
		"non numeric inode":   procTCPHeader + "  2: 0100007F:3039 00000000:0000 0A 0 0 0 0 0 inode\n",
		"off-port bad inode":  procTCPHeader + "  2: 0100007F:270F 00000000:0000 0A 00000000:00000000 00:00000000 00000000     0        0 inode 1 0000 0\n",
		"off-port zero inode": procTCPHeader + "  2: 0100007F:270F 00000000:0000 0A 00000000:00000000 00:00000000 00000000     0        0 0 1 0000 0\n",
		"empty middle line":   procTCPHeader + procTCPRecord(endpoint, "0A", 1) + "\n" + procTCPRecord(endpoint, "0A", 2),
		"negative port hex":   procTCPHeader + "  2: 0100007F:-3039 00000000:0000 0A 0 0 0 0 0 1\n",
	}
	for name, table := range records {
		if _, _, e := collectTCP(t, table, false, endpoint); e == nil {
			t.Fatal("malformed table accepted:", name)
		}
	}
	v6table := procTCPHeader + "  2: 0000000000000000000000000100000:3039 00000000000000000000000000000000:0000 0A 0 0 0 0 0 1\n"
	if _, _, e := collectTCP(t, v6table, true, endpoint); e == nil {
		t.Fatal("malformed IPv6 address length accepted")
	}
	v6zeroPort := procTCPHeader + "  2: 00000000000000000000000001000000:0000 00000000000000000000000000000000:0000 0A 0 0 0 0 0 1\n"
	v6endpoint := netip.MustParseAddrPort("[::1]:12345")
	if _, _, e := collectTCP(t, v6zeroPort, true, v6endpoint); e == nil {
		t.Fatal("malformed IPv6 table accepted")
	}
}

func TestProcSocketLinkParsing(t *testing.T) {
	inode, isSocket, e := parseSocketLink("socket:[4021]")
	if e != nil || !isSocket || inode != 4021 {
		t.Fatal(inode, isSocket, e)
	}
	if inode, isSocket, e = parseSocketLink("pipe:[4021]"); e != nil || isSocket || inode != 0 {
		t.Fatal(inode, isSocket, e)
	}
	if inode, isSocket, e = parseSocketLink("anon_inode:[eventfd]"); e != nil || isSocket || inode != 0 {
		t.Fatal(inode, isSocket, e)
	}
	for _, link := range []string{"socket:[abc]", "socket:[]", "socket:[0]", "socket:[4021", "socket:[18446744073709551616]"} {
		if _, _, e := parseSocketLink(link); e == nil {
			t.Fatal("malformed socket link accepted", link)
		}
	}
}

func TestBoundedReadRefusesOversizedRecords(t *testing.T) {
	data, e := readBounded(strings.NewReader("small"), 1<<20)
	if e != nil || string(data) != "small" {
		t.Fatal(string(data), e)
	}
	if _, e = readBounded(strings.NewReader(strings.Repeat("a", 65)), 64); e == nil {
		t.Fatal("oversized record accepted")
	}
	if _, e = readBounded(io.MultiReader(strings.NewReader(strings.Repeat("a", 64)), strings.NewReader("x")), 64); e == nil {
		t.Fatal("record at the bound plus one byte accepted")
	}
	if data, e = readBounded(strings.NewReader(strings.Repeat("a", 64)), 64); e != nil || len(data) != 64 {
		t.Fatal(len(data), e)
	}
	if _, e = readBounded(erringReader{}, 64); e == nil {
		t.Fatal("read error swallowed")
	}
}

type erringReader struct{}

func (erringReader) Read([]byte) (int, error) { return 0, io.ErrClosedPipe }
