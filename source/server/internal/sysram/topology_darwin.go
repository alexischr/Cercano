//go:build darwin

package sysram

import "golang.org/x/sys/unix"

// Topology reports the machine's memory organization. On Apple Silicon
// (hw.machine == "arm64") hw.memsize is the unified pool the GPU draws
// from, so the machine reports unified and UnifiedMemoryBytes reuses
// Total() — one probe, one number.
//
// Intel Macs (hw.machine != "arm64") can carry discrete GPUs whose VRAM
// macOS exposes no probe for here; rather than pass system RAM off as
// GPU memory, the probe reports unknown. Callers must disclose that
// instead of treating Total() as a GPU budget.
func Topology() MemoryTopology {
	machine, err := unix.Sysctl("hw.machine")
	if err != nil {
		return MemoryTopology{Kind: TopologyUnknown}
	}
	return topologyForDarwin(machine, Total())
}

// topologyForDarwin is the pure decision over the probed values, split
// out so it is testable without a live sysctl. unified is claimed only
// when the architecture is arm64 and the memory probe succeeded; every
// other combination is an explicit unknown.
func topologyForDarwin(machine string, total int64) MemoryTopology {
	if machine != "arm64" || total <= 0 {
		return MemoryTopology{Kind: TopologyUnknown}
	}
	return MemoryTopology{Kind: TopologyUnified, UnifiedMemoryBytes: total}
}
