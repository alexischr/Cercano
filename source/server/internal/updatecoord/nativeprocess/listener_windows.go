//go:build windows

package nativeprocess

import (
	"fmt"
	"net/netip"
	"os"
	"unsafe"

	"golang.org/x/sys/windows"
)

// HoldsListener verifies a known same-user process owns a loopback TCP
// endpoint on Windows. It asks iphlpapi's GetExtendedTcpTable for the
// TCP_TABLE_OWNER_PID_LISTENER tables (query class
// tcpTableQueryClassOwnerPIDListener) and matches the endpoint through
// queryTCPOwnerListener, using no elevation. The process's exact lifetime
// is verified before and after the table reads, the endpoint must be a
// valid loopback address with a non-zero port and no zone, and the
// endpoint's own family is matched exactly or by its family-appropriate
// wildcard. An IPv6 wildcard socket never verifies an IPv4 endpoint: the
// listener tables do not expose IPV6_V6ONLY, so that case is reported as
// an explicit cannot-determine error, never as verified absence. Every
// unreadable, malformed or unattributable observation is an error. This is
// a per-candidate observation, never a proof of exclusive endpoint
// ownership or of service health.
//
// The observation is conservative under churn: the tables are read twice
// in bounded queries, and the answer is refused unless the two snapshots
// of the target PID's own rows agree. The comparison is normalized
// (targetOwnerRows) so unrelated processes' rows are dropped and the
// kernel's row order is irrelevant: background traffic from other
// processes starting or stopping sockets between the two reads cannot
// force a refusal, while any change to the target's own rows errors out
// instead of grounding an owned or not-owned answer.
//
// The PID the kernel attributes to a LISTEN socket is the process that
// created the socket; a handle inherited by another process keeps the
// original creator's PID, which only adds foreign-created sockets this
// check will (conservatively) not claim.
func HoldsListener(expected Identity, endpoint netip.AddrPort) (bool, error) {
	if !endpoint.IsValid() || !endpoint.Addr().IsLoopback() || endpoint.Port() == 0 || endpoint.Addr().Zone() != "" {
		return false, fmt.Errorf("restart requires an explicit loopback TCP endpoint")
	}
	self, err := Inspect(os.Getpid())
	if err != nil {
		return false, err
	}
	before, err := Inspect(expected.PID)
	if err != nil {
		return false, err
	}
	// Same user, exactly this process: the SID of the observed token must
	// equal the caller's own, and every identity field must match the
	// expected lifetime record before any table is read.
	if !before.SameProcess(expected) || before.WindowsSID != self.WindowsSID {
		return false, fmt.Errorf("listener process identity changed or belongs to another user")
	}
	pid := int32(expected.PID)
	// An IPv4 endpoint needs both family tables: the IPv6 table is the only
	// way to see an IPv6 wildcard socket whose dual-stack IPv4 service the
	// tables cannot decide. A host whose IPv6 table cannot be read therefore
	// answers with uncertainty, not absence. An IPv6 endpoint needs only the
	// IPv6 table: no IPv4 socket can serve an IPv6 address.
	families := []windowsOwnerTable{{family: windows.AF_INET6, ipv6: true}}
	if endpoint.Addr().Is4() {
		families = []windowsOwnerTable{
			{family: windows.AF_INET, ipv6: false},
			{family: windows.AF_INET6, ipv6: true},
		}
	}
	readFamilies := func() (rows4, rows6 []tcpOwnerListener, err error) {
		for _, table := range families {
			rows, err := readOwnerListenerTable(table.family, table.ipv6)
			if err != nil {
				return nil, nil, err
			}
			if table.ipv6 {
				rows6 = rows
			} else {
				rows4 = rows
			}
		}
		return rows4, rows6, nil
	}
	first4, first6, err := readFamilies()
	if err != nil {
		return false, err
	}
	found, uncertain, err := queryTCPOwnerListener(first4, first6, endpoint, pid)
	if err != nil {
		return false, err
	}
	if !found && uncertain {
		return false, fmt.Errorf("cannot determine from the TCP owner tables whether IPv6 wildcard socket on port %d serves the IPv4 endpoint: %w", endpoint.Port(), errListenerSnapshot)
	}
	// Conservative changing observation: read the tables a second time and
	// compare only the target PID's rows, normalized so table order is
	// irrelevant. A disagreement is a changing observation and refuses to
	// answer either way.
	second4, second6, err := readFamilies()
	if err != nil {
		return false, err
	}
	if !sameOwnerSnapshot(targetOwnerRows(first4, pid), targetOwnerRows(second4, pid)) ||
		!sameOwnerSnapshot(targetOwnerRows(first6, pid), targetOwnerRows(second6, pid)) {
		return false, fmt.Errorf("the listener tables for PID %d changed between the two bounded observations: %w", expected.PID, errListenerObsChanged)
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

// windowsOwnerTable names one GetExtendedTcpTable family query.
type windowsOwnerTable struct {
	family uint32
	ipv6   bool
}

// GetExtendedTcpTable is not wrapped by x/sys; it is resolved lazily from
// the system iphlpapi.dll. NewLazySystemDLL loads from the system directory
// only, and LazyProc.Call resolves the entry point once and fails with an
// explicit error if it is missing.
var (
	modIphlpapi             = windows.NewLazySystemDLL("iphlpapi.dll")
	procGetExtendedTcpTable = modIphlpapi.NewProc("GetExtendedTcpTable")
)

// readOwnerListenerTable returns the decoded LISTEN rows of one family's
// owner table. The native side supplies the system DLL call as the
// tcpTableQueryFunc seam; all sizing, retry, and status handling lives in
// the portable boundedTCPTableQuery driver, which decodes with the
// corrected 24- and 56-byte row parsers. The DWORD return value is the
// only outcome checked; LazyProc.Call's error would only reflect
// GetLastError, which can be stale for a returning-status API.
func readOwnerListenerTable(family uint32, ipv6 bool) ([]tcpOwnerListener, error) {
	if err := procGetExtendedTcpTable.Find(); err != nil {
		return nil, fmt.Errorf("iphlpapi GetExtendedTcpTable unavailable: %w", err)
	}
	rowSize := tcp4RowSize
	if ipv6 {
		rowSize = tcp6RowSize
	}
	query := func(buf []byte) (status uint32, size uint32, err error) {
		// A nil buffer asks the sizing question: a null table pointer with
		// a zero size reports the required byte count and the
		// insufficient-buffer status. On a successful read pdwSize receives
		// the written byte count, which the driver validates.
		sizeArg := uint32(len(buf))
		tablePtr := uintptr(0)
		if buf != nil {
			tablePtr = uintptr(unsafe.Pointer(&buf[0]))
		}
		r0, _, _ := procGetExtendedTcpTable.Call(
			tablePtr,
			uintptr(unsafe.Pointer(&sizeArg)),
			0,
			uintptr(family),
			tcpTableQueryClassOwnerPIDListener,
			0,
		)
		return uint32(r0), sizeArg, nil
	}
	data, err := boundedTCPTableQuery(query, rowSize)
	if err != nil {
		return nil, fmt.Errorf("GetExtendedTcpTable(family %d): %w", family, err)
	}
	if ipv6 {
		return parseTCP6OwnerTable(data)
	}
	return parseTCP4OwnerTable(data)
}
