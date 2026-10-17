//go:build darwin

package sysram

import (
	"reflect"
	"testing"

	"golang.org/x/sys/unix"
)

// Unified is claimed only when the architecture is arm64 and the memory
// probe succeeded; everything else — Intel Macs whose discrete-GPU VRAM
// this package cannot see, a failed sysctl, a failed memory probe — is
// an explicit unknown, never RAM passed off as GPU memory.
func TestTopologyForDarwin(t *testing.T) {
	cases := []struct {
		name    string
		machine string
		total   int64
		want    MemoryTopology
	}{
		{"apple silicon", "arm64", 128 << 30, MemoryTopology{Kind: TopologyUnified, UnifiedMemoryBytes: 128 << 30}},
		{"intel mac", "x86_64", 64 << 30, MemoryTopology{Kind: TopologyUnknown}},
		{"memory probe failed", "arm64", 0, MemoryTopology{Kind: TopologyUnknown}},
		{"sysctl failed", "", 128 << 30, MemoryTopology{Kind: TopologyUnknown}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := topologyForDarwin(c.machine, c.total); !reflect.DeepEqual(got, c.want) {
				t.Fatalf("topologyForDarwin(%q, %d) = %+v, want %+v", c.machine, c.total, got, c.want)
			}
		})
	}
}

// The live probe must stay consistent with Total(): on the unified
// Apple Silicon machines this repo targets, the topology's pool IS the
// total. On any other machine, the probe must never claim a pool.
func TestTopology_LiveProbeConsistentWithTotal(t *testing.T) {
	machine, err := unix.Sysctl("hw.machine")
	if err != nil {
		t.Skip("hw.machine unavailable")
	}
	got := Topology()
	total := Total()
	switch {
	case machine == "arm64" && total > 0:
		if got.Kind != TopologyUnified {
			t.Fatalf("arm64 with known memory: kind = %q, want unified", got.Kind)
		}
		if got.UnifiedMemoryBytes != total {
			t.Fatalf("UnifiedMemoryBytes = %d, want %d (Total)", got.UnifiedMemoryBytes, total)
		}
		if len(got.GPUs) != 0 {
			t.Fatalf("unified topology must not list discrete GPUs: %+v", got.GPUs)
		}
	default:
		if got.Kind != TopologyUnknown {
			t.Fatalf("non-arm64 or failed probe: kind = %q, want unknown", got.Kind)
		}
	}
}
