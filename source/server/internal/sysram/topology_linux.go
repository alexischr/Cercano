//go:build linux

package sysram

import (
	"context"
	"os/exec"
	"time"
)

// nvidiaSmiTimeout bounds the discrete-GPU probe. nvidia-smi normally
// answers in well under a second; the bound exists so a wedged driver
// cannot hang a catalog listing.
const nvidiaSmiTimeout = 2 * time.Second

// Topology reports the machine's memory organization via a best-effort
// discrete-GPU probe: it enumerates NVIDIA GPUs with nvidia-smi and
// reports one entry per device, each with its own pool.
//
// The probe is deliberately narrow rather than guessy. GPUs nvidia-smi
// cannot see (AMD, Intel, integrated graphics) and machines without the
// tool report unknown — never "unified", and never system RAM passed
// off as GPU memory. System RAM remains available through Total(), but
// it is a different quantity, and conflating them is the exact bug this
// API exists to prevent.
func Topology() MemoryTopology {
	return topologyForLinux(probeNvidiaGPUs())
}

// probeNvidiaGPUs runs the one nvidia-smi query the topology needs and
// reports whether the tool ran at all. ok=false covers a missing tool,
// a non-zero exit, and a timeout alike: all mean "unknown", and the
// parser is what turns the output into devices.
func probeNvidiaGPUs() (devs []GPUDevice, ok bool) {
	ctx, cancel := context.WithTimeout(context.Background(), nvidiaSmiTimeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, "nvidia-smi",
		"--query-gpu=name,memory.total",
		"--format=csv,noheader,nounits").Output()
	if err != nil {
		return nil, false
	}
	return parseNvidiaSmiGPUs(string(out)), true
}
