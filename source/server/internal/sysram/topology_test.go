package sysram

import (
	"reflect"
	"testing"
)

// realNvidiaSmiOutput is the format nvidia-smi produces for
// `--query-gpu=name,memory.total --format=csv,noheader,nounits`:
// one "name, mib" line per GPU, memory in MiB.
const realNvidiaSmiOutput = `NVIDIA GeForce RTX 3090, 24576
NVIDIA GeForce RTX 4070, 12268
`

func TestParseNvidiaSmiGPUs_MatchesCapturedFormat(t *testing.T) {
	got := parseNvidiaSmiGPUs(realNvidiaSmiOutput)
	want := []GPUDevice{
		{Name: "NVIDIA GeForce RTX 3090", MemoryBytes: 24576 << 20},
		{Name: "NVIDIA GeForce RTX 4070", MemoryBytes: 12268 << 20},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("parse = %+v, want %+v", got, want)
	}
}

// A GPU name containing a comma must not corrupt the memory field: the
// split is from the end of the line.
func TestParseNvidiaSmiGPUs_NameWithComma(t *testing.T) {
	got := parseNvidiaSmiGPUs("GPU Foo, Bar, 24576\n")
	if len(got) != 1 || got[0].Name != "GPU Foo, Bar" || got[0].MemoryBytes != 24576<<20 {
		t.Fatalf("parse = %+v, want one 24576 MiB GPU named 'GPU Foo, Bar'", got)
	}
}

// An unparsable memory figure keeps the device with MemoryBytes 0: the
// GPU is known to exist, its size is an explicit unknown. It must not
// be dropped (that would silently hide hardware) or guessed.
func TestParseNvidiaSmiGPUs_UnparsableMemoryIsExplicitUnknown(t *testing.T) {
	got := parseNvidiaSmiGPUs("NVIDIA A100, [N/A]\n")
	want := []GPUDevice{{Name: "NVIDIA A100", MemoryBytes: 0}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("parse = %+v, want %+v", got, want)
	}
}

func TestParseNvidiaSmiGPUs_GarbageYieldsNoDevices(t *testing.T) {
	for name, in := range map[string]string{
		"empty":   "",
		"blank lines": "\n\n",
		"no comma": "NVIDIA GeForce RTX 3090",
		"error text": "No devices were found",
	} {
		t.Run(name, func(t *testing.T) {
			if got := parseNvidiaSmiGPUs(in); len(got) != 0 {
				t.Fatalf("parse = %+v, want no devices", got)
			}
		})
	}
}

// The Linux classification is conservative: without a working probe —
// missing nvidia-smi, AMD/Intel-only machine, or a driver error — the
// topology is unknown, never unified and never RAM-as-GPU-memory.
func TestTopologyForLinux_UnknownWithoutProbeOrDevices(t *testing.T) {
	cases := []struct {
		name string
		devs []GPUDevice
		ok   bool
	}{
		{"probe failed", nil, false},
		{"probe ok, no devices", nil, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := topologyForLinux(c.devs, c.ok)
			if got.Kind != TopologyUnknown {
				t.Fatalf("kind = %q, want %q", got.Kind, TopologyUnknown)
			}
			if got.UnifiedMemoryBytes != 0 || len(got.GPUs) != 0 {
				t.Fatalf("unknown topology must carry no memory claims: %+v", got)
			}
		})
	}
}

// Devices are reported individually with their own pools — never summed
// into a single fit guarantee (two 24 GB GPUs are not one 48 GB GPU).
func TestTopologyForLinux_DiscreteKeepsDevicesSeparate(t *testing.T) {
	devs := []GPUDevice{
		{Name: "NVIDIA GeForce RTX 3090", MemoryBytes: 24576 << 20},
		{Name: "NVIDIA GeForce RTX 3090", MemoryBytes: 24576 << 20},
	}
	got := topologyForLinux(devs, true)
	if got.Kind != TopologyDiscrete {
		t.Fatalf("kind = %q, want %q", got.Kind, TopologyDiscrete)
	}
	if got.UnifiedMemoryBytes != 0 {
		t.Fatalf("discrete topology must not claim unified memory: %+v", got)
	}
	if !reflect.DeepEqual(got.GPUs, devs) {
		t.Fatalf("GPUs = %+v, want %+v", got.GPUs, devs)
	}
}
