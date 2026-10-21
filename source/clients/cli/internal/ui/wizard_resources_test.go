package ui

import (
	"cercano/source/clients/cli/internal/wizard"
	"cercano/source/server/pkg/agentclient"
	"testing"
)

func TestWizardFreshRunStartsWithResources(t *testing.T) {
	t.Setenv("CERCANO_WIZARD_STATE", t.TempDir()+"/state.yaml")
	st := wizard.New()
	if string(st.Step) != "resources" {
		t.Fatalf("fresh setup asks strategy before resources: %s", st.Step)
	}
}

func TestWizardLocalEligibilityBoundary(t *testing.T) {
	const gib = int64(1 << 30)
	for _, tc := range []struct {
		name     string
		ram      int64
		hardware agentclient.HardwareTopology
		ok       bool
		want     bool
	}{
		{"below floor", 47 * gib, agentclient.HardwareTopology{Kind: "unified", UnifiedMemoryBytes: 47 * gib}, true, false},
		{"unified floor", 48 * gib, agentclient.HardwareTopology{Kind: "unified", UnifiedMemoryBytes: 48 * gib}, true, true},
		{"unknown", 128 * gib, agentclient.HardwareTopology{Kind: "unknown"}, false, false},
		{"cpu only", 64 * gib, agentclient.HardwareTopology{Kind: "discrete"}, true, false},
		{"unknown gpu pool", 64 * gib, agentclient.HardwareTopology{Kind: "discrete", GPUs: []agentclient.HardwareGPU{{Name: "GPU"}}}, true, false},
		{"discrete", 64 * gib, agentclient.HardwareTopology{Kind: "discrete", GPUs: []agentclient.HardwareGPU{{Name: "GPU", MemoryBytes: 16 * gib}}}, true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cat := agentclient.RuntimeModelCatalog{SystemRAMBytes: tc.ram, Hardware: tc.hardware, HardwareOK: tc.ok}
			got, _, _ := wizardLocalHardware(cat, true)
			if got != tc.want {
				t.Fatalf("eligible=%v, want %v", got, tc.want)
			}
		})
	}
}

func TestLocalModelBudgetDoesNotAddDiscreteGPUOrSystemMemory(t *testing.T) {
	const gib = int64(1 << 30)
	wp := newTestWizardPage(t)
	wp.catalogOK = true
	wp.catalog = agentclient.RuntimeModelCatalog{SystemRAMBytes: 128 * gib, HardwareOK: true, Hardware: agentclient.HardwareTopology{Kind: "discrete", GPUs: []agentclient.HardwareGPU{{Name: "GPU1", MemoryBytes: 8 * gib}, {Name: "GPU2", MemoryBytes: 8 * gib}}}, RecommendedOpenModels: map[string]string{"most_capable": "small", "everyday": "small", "fast_light": "small", "fast_light_text": "small"}, Models: []agentclient.RuntimeModel{{ID: "small", Runtime: "llama_server", Source: "catalog", SizeBytes: 10 * gib, SupportsChat: true}}}
	if ok, _ := wp.localResource(); ok {
		t.Fatal("summed graphics/system memory made an oversized model eligible")
	}
	wp.catalog.Hardware.GPUs = []agentclient.HardwareGPU{{Name: "GPU", MemoryBytes: 24 * gib}}
	if ok, why := wp.localResource(); !ok {
		t.Fatalf("model within single GPU budget rejected: %s", why)
	}
	wp.state.TierPicks = map[string]string{"llama_server.everyday": ""}
	if ok, _ := wp.localResource(); ok {
		t.Fatal("explicitly cleared required slot silently refilled")
	}
}
