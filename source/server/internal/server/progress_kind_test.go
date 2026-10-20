package server

import (
	"cercano/source/server/internal/runner"
	"cercano/source/server/pkg/proto"
	"testing"
)

func TestProgressCategoryDeliveryAndReplay(t *testing.T) {
	s, _ := newServerWithStore(t)
	_, gen, release := s.turnBroker.BeginTurn(t.Context(), "owner")
	defer release()
	for _, kind := range []runner.ProgressKind{0, 1, 2, 99} {
		s.turnBroker.Publish("owner", gen, runner.Event{Kind: runner.EventProgress, Text: "arbitrary wording", ProgressKind: kind})
	}
	replay, _, detach := s.turnBroker.Attach("owner")
	defer detach()
	for i, ev := range replay {
		probe := &attachmentProbe{ctx: t.Context(), send: func(response *proto.StreamProcessResponse) error {
			if response.GetProgress().GetKind() != proto.ProgressKind(ev.ProgressKind) {
				t.Fatalf("event %d lost category", i)
			}
			return nil
		}}
		if err := sendRunnerEvent(probe, ev); err != nil {
			t.Fatal(err)
		}
	}
	if len(replay) != 4 {
		t.Fatalf("replay count=%d", len(replay))
	}
}
