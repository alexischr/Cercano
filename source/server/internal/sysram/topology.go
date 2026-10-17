package sysram

import (
	"strconv"
	"strings"
)

// Topology kinds reported by MemoryTopology.Kind.
const (
	// TopologyUnified means CPU and GPU share one physical memory pool
	// (Apple Silicon): UnifiedMemoryBytes is that pool and is the same
	// budget the GPU draws from. GPUs is empty — the integrated GPU has
	// no separate pool.
	TopologyUnified = "unified"
	// TopologyDiscrete means at least one GPU with its own memory pool
	// was detected; each device is listed individually in GPUs with its
	// own memory. The pools are deliberately NOT summed, and system RAM
	// is NOT GPU memory: a fit verdict for such machines must treat each
	// device separately and never assume the full system RAM is
	// GPU-addressable.
	TopologyDiscrete = "discrete"
	// TopologyUnknown means the probe could not determine the topology
	// (unsupported platform, failed probe, missing tooling). Callers
	// must disclose this rather than guessing "unified".
	TopologyUnknown = "unknown"
)

// GPUDevice is one discrete GPU and its own memory pool. MemoryBytes is
// the device's video memory in bytes; 0 means the device was detected
// but its size could not be determined — an explicit unknown, not zero.
//
// Devices are reported individually. Summing them (or adding them to
// system RAM) would fabricate a fit guarantee no single device
// provides: two 24 GB GPUs cannot hold one 48 GB model.
type GPUDevice struct {
	Name        string
	MemoryBytes int64
}

// MemoryTopology describes how the machine's compute memory is
// organized, so callers can tell a unified-memory machine (system RAM
// really is the GPU budget) from a discrete-GPU machine (it is not)
// from unknown. UnifiedMemoryBytes is nonzero only when Kind is
// TopologyUnified; GPUs is populated only when Kind is
// TopologyDiscrete.
type MemoryTopology struct {
	Kind               string
	UnifiedMemoryBytes int64
	GPUs               []GPUDevice
}

// topologyForLinux classifies the discrete-GPU probe result. The probe
// is explicitly best-effort: a missing or failed nvidia-smi, or a
// machine whose GPUs it cannot see (AMD/Intel/integrated), reports
// unknown rather than treating system RAM as GPU memory. Devices are
// kept individually — never summed.
func topologyForLinux(devs []GPUDevice, ok bool) MemoryTopology {
	if !ok || len(devs) == 0 {
		return MemoryTopology{Kind: TopologyUnknown}
	}
	return MemoryTopology{Kind: TopologyDiscrete, GPUs: devs}
}

// parseNvidiaSmiGPUs parses `nvidia-smi --query-gpu=name,memory.total
// --format=csv,noheader,nounits` output: one "name, mib" line per GPU.
// Split out from the probe so the parser is testable against captured
// fixtures without nvidia-smi installed.
//
// The comma is taken from the end of the line (LastIndex), so a GPU
// name containing a comma still parses. A line that cannot be
// attributed is skipped — never guessed. An unparsable memory figure
// keeps the device with MemoryBytes 0: the GPU is known to exist but
// its size is explicitly unknown.
func parseNvidiaSmiGPUs(out string) []GPUDevice {
	var devs []GPUDevice
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		i := strings.LastIndex(line, ",")
		if i < 0 {
			continue
		}
		name := strings.TrimSpace(line[:i])
		if name == "" {
			continue
		}
		var mem int64
		// --format=csv,nounits reports memory.total in MiB.
		if f, err := strconv.ParseFloat(strings.TrimSpace(line[i+1:]), 64); err == nil && f >= 0 {
			mem = int64(f * (1 << 20))
		}
		devs = append(devs, GPUDevice{Name: name, MemoryBytes: mem})
	}
	return devs
}
