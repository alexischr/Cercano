package nativeprocess

import "testing"

func TestBoundedTCPTableQueryEmptyCountNeedsOnlyHeader(t *testing.T) {
	for _, rowSize := range []int{tcp4RowSize, tcp6RowSize} {
		table := make([]byte, tcpTableHeaderSize)
		query, _ := fakeQuery(t, sizingCall(len(table)), readCall(table))
		data, err := boundedTCPTableQuery(query, rowSize)
		if err != nil {
			t.Fatalf("empty family table refused: %v", err)
		}
		if len(data) != tcpTableHeaderSize {
			t.Fatal("unexpected empty table length", len(data))
		}
	}
}
