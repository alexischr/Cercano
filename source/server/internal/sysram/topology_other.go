//go:build !darwin && !linux

package sysram

// Topology reports unknown on platforms without a probe
// implementation. Callers must disclose the unknown instead of treating
// Total() (itself 0 here) as a GPU budget.
func Topology() MemoryTopology {
	return MemoryTopology{Kind: TopologyUnknown}
}
