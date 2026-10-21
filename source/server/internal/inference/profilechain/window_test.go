package profilechain

import (
	"cercano/source/server/internal/modelmetadata"
	"testing"
)

type windowEvidenceFixture struct {
	accountingProfileFixture
	window int
}

func (p *windowEvidenceFixture) ModelEvidence(string) modelmetadata.Evidence {
	return modelmetadata.Evidence{ContextWindow: p.window}
}

func TestRouteWindowUsesRequestedModel(t *testing.T) {
	for _, tc := range []struct {
		name, model    string
		evidence, want int
		known          bool
	}{
		{"astra without evidence", "gpt-6-astra", 0, 1050000, true},
		{"sol without evidence", "gpt-6.1-sol", 0, 1050000, true},
		{"sol provider capacity wins", "gpt-6.1-sol", 262144, 262144, true},
		{"provider capacity wins", "gpt-6-astra", 262144, 262144, true},
		{"unknown stays unknown", "unrecognized-model", 0, 128000, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := &routeProvider{Provider: &windowEvidenceFixture{window: tc.evidence}}
			route := p.TargetFor(tc.model, "")
			if route.ContextWindow != tc.want || route.ContextWindowKnown != tc.known {
				t.Fatalf("route window = %d/%v, want %d/%v", route.ContextWindow, route.ContextWindowKnown, tc.want, tc.known)
			}
		})
	}
}
