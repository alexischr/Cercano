package modelevidence

import (
	"cercano/source/server/internal/modelmetadata"
	"cercano/source/server/pkg/config"
	"testing"
)

func TestDefaultSubscriptionIdentityVision(t *testing.T) {
	for _, tc := range []struct {
		name  string
		p     config.CloudProfile
		model string
		want  modelmetadata.Vision
	}{
		{"chatgpt", config.CloudProfile{Name: "account", Flavor: "responses", Route: "chatgpt"}, "gpt-6-astra", modelmetadata.VisionSupported},
		{"claude", config.CloudProfile{Name: "account", Flavor: "messages", Route: "subscription"}, "claude-opus-5", modelmetadata.VisionSupported},
		{"gateway", config.CloudProfile{Name: "account", Flavor: "responses", Route: "chatgpt", BaseURL: "https://gateway.example/v1"}, "gpt-6-astra", modelmetadata.VisionUnknown},
		{"unknown", config.CloudProfile{Name: "openai-responses"}, "gpt-6-astra", modelmetadata.VisionUnknown},
	} {
		t.Run(tc.name, func(t *testing.T) {
			id := IdentityFor(tc.p, tc.model)
			if got := shippedEvidence(id).Vision; got != tc.want {
				t.Fatalf("identity=%+v vision=%v want=%v", id, got, tc.want)
			}
		})
	}
}
