package modelevidence

import (
	"context"
	"testing"

	"cercano/source/server/internal/modelmetadata"
)

func TestAstraVisionEvidence(t *testing.T) {
	for _, tt := range []struct {
		name, base string
		want       modelmetadata.Vision
	}{
		{"OpenAI", "https://api.openai.com/v1", modelmetadata.VisionSupported},
		{"unknown gateway", "https://example.com/v1", modelmetadata.VisionUnknown},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got := New(nil).Resolve(context.Background(), modelmetadata.Identity{Provider: "openai", BaseURL: tt.base, Model: "gpt-6-astra"})
			if got.Vision != tt.want {
				t.Fatalf("vision = %v, want %v", got.Vision, tt.want)
			}
		})
	}
}
