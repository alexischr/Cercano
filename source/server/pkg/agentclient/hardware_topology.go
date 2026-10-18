package agentclient

import "cercano/source/server/pkg/proto"

// Hardware topology kinds, mirroring the server's sysram probe
// vocabulary on the wire ("unified", "discrete", "unknown").
const (
	HardwareTopologyUnified  = "unified"
	HardwareTopologyDiscrete = "discrete"
	HardwareTopologyUnknown  = "unknown"
)

// HardwareGPU is one discrete GPU device's memory pool as reported by
// the server. MemoryBytes 0 means the device was detected but its pool
// size is unknown — a known device with an unknown pool, not zero
// memory.
type HardwareGPU struct {
	Name        string
	MemoryBytes int64
}

// HardwareTopology is how the agent machine organizes compute memory.
// Kind is one of the HardwareTopology* constants: unified (system RAM
// really is the GPU budget, sized by UnifiedMemoryBytes), discrete
// (per-device pools in GPUs, never summed and never conflated with
// system RAM), or unknown (the probe failed or the server predates the
// topology fields). Callers must disclose unknown rather than guess.
type HardwareTopology struct {
	Kind               string
	UnifiedMemoryBytes int64
	GPUs               []HardwareGPU
}

// Insufficient reports whether the topology carries no usable memory
// pool information — an empty or unknown kind — so no hardware fit
// verdict can honestly be drawn from it.
func (h HardwareTopology) Insufficient() bool {
	return h.Kind == "" || h.Kind == HardwareTopologyUnknown
}

// mapHardwareTopology maps the ListRuntimeModels wire response onto
// HardwareTopology. A response with no topology fields (an older
// server) maps to unknown — never to an invented unified or discrete
// claim; unknown is exactly how old servers must read.
func mapHardwareTopology(resp *proto.ListRuntimeModelsResponse) HardwareTopology {
	switch resp.GetMemoryTopology() {
	case HardwareTopologyUnified:
		return HardwareTopology{
			Kind:               HardwareTopologyUnified,
			UnifiedMemoryBytes: resp.GetUnifiedMemoryBytes(),
		}
	case HardwareTopologyDiscrete:
		gpus := resp.GetGpus()
		out := make([]HardwareGPU, 0, len(gpus))
		for _, g := range gpus {
			out = append(out, HardwareGPU{Name: g.GetName(), MemoryBytes: g.GetGpuMemoryBytes()})
		}
		return HardwareTopology{Kind: HardwareTopologyDiscrete, GPUs: out}
	default:
		return HardwareTopology{Kind: HardwareTopologyUnknown}
	}
}
