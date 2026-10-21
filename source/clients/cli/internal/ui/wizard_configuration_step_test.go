package ui

import (
	"cercano/source/server/pkg/agentclient"
	"cercano/source/server/pkg/proto"
	"strings"
	"testing"

	"cercano/source/clients/cli/internal/wizard"
)

func TestWizardProvidersContinueToConfigurationStepTwo(t *testing.T) {
	stub, client := newScenario(t)
	wp := resourceWizard(t, client)
	pressRows(t, wp, "resource_next")
	if wp.state.Step != wizard.StepPreference {
		t.Fatalf("extra informational screen: %s", wp.state.Step)
	}
	if n, total := wp.stepIndex(); n != 2 || total != 3 {
		t.Fatalf("wrong progress %d/%d", n, total)
	}
	for _, row := range wp.rows() {
		switch row.Key {
		case "recheck", "accounts", "local_models", "preferences":
			t.Fatalf("out-of-order action %s", row.Key)
		}
	}
	text := wp.stepDesc()
	for _, want := range []string{"48 GB", "GPU", "How would you like Cercano"} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing explanation %q: %s", want, text)
		}
	}
	if stub.writeCount() != 0 {
		t.Fatal("navigation wrote configuration")
	}
}

func TestWizardProvidersExplainAdditionalAccounts(t *testing.T) {
	_, client := newScenario(t)
	wp := resourceWizard(t, client)
	text := wp.stepDesc()
	for _, want := range []string{"as many cloud providers", "same provider", "backup"} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing account guidance %q: %s", want, text)
		}
	}
	found := false
	for _, row := range wp.rows() {
		if row.Key == "add_provider" {
			found = true
		}
	}
	if !found {
		t.Fatal("existing accounts hide Add cloud provider")
	}
}

func TestWizardFormerResourceSummaryResumesAtConfiguration(t *testing.T) {
	for _, oldStep := range []wizard.Step{wizard.StepResources, wizard.StepOpen} {
		t.Run(string(oldStep), func(t *testing.T) {
			stub, client := newScenario(t)
			old := wizard.State{FlowVersion: 2, Step: oldStep, ResourcePage: 2, LocusMode: "open_only", CloudProfile: "personal", TierPicks: map[string]string{"llama_server.everyday": "keep-explicit"}}
			if err := wizard.Save(old); err != nil {
				t.Fatal(err)
			}
			wp := resourceWizard(t, client)
			if wp.state.Step != wizard.StepPreference || wp.state.TierPicks["llama_server.everyday"] != "keep-explicit" {
				t.Fatalf("bad migration: %+v", wp.state)
			}
			if n, _ := wp.stepIndex(); n != 2 {
				t.Fatal("migrated configuration not numbered 2")
			}
			if wp.back() || wp.state.Step != wizard.StepResources || wp.state.ResourcePage != 0 {
				t.Fatal("Back did not return to cloud providers")
			}
			if stub.writeCount() != 0 {
				t.Fatal("migration changed server configuration")
			}
		})
	}
}

func TestWizardContinueRefreshesCapabilitiesAutomatically(t *testing.T) {
	stub, client := newScenario(t)
	wp := resourceWizard(t, client)
	if ok, _ := wp.localResource(); !ok {
		t.Fatal("fixture must initially be local-eligible")
	}
	stub.mu.Lock()
	stub.catalog = &proto.ListRuntimeModelsResponse{SystemRamBytes: 32 << 30}
	stub.mu.Unlock()
	pressRows(t, wp, "resource_next")
	for _, row := range wp.rows() {
		if row.Key == "local" {
			t.Fatal("continued with stale local eligibility")
		}
	}
	if !strings.Contains(wp.stepDesc(), "48 GB") {
		t.Fatal("missing reason local setup is unavailable")
	}
	if stub.writeCount() != 0 {
		t.Fatal("automatic discovery wrote config")
	}
}

func TestLocalEligibilityExplainsDiscreteMemoryWithoutWiredClaim(t *testing.T) {
	cat := agentclient.RuntimeModelCatalog{SystemRAMBytes: 64 << 30, HardwareOK: true, Hardware: agentclient.HardwareTopology{Kind: agentclient.HardwareTopologyDiscrete, GPUs: []agentclient.HardwareGPU{{Name: "GPU", MemoryBytes: 24 << 30}}}}
	ok, _, text := wizardLocalHardware(cat, true)
	if !ok {
		t.Fatal(text)
	}
	for _, want := range []string{"64", "48 GB", "GPU", "24", "dedicated"} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %q: %s", want, text)
		}
	}
	if strings.Contains(strings.ToLower(text), "wired") || strings.Contains(strings.ToLower(text), "free memory") {
		t.Fatal("capacity probe presented as an availability or wired-memory measurement")
	}
}
