package agentclient

import (
	"cercano/source/server/pkg/proto"
	"testing"
)

func TestDecodeProgressCategory(t *testing.T) {
	for _, kind := range []proto.ProgressKind{0, 1, 2, 99} {
		response := &proto.StreamProcessResponse{Payload: &proto.StreamProcessResponse_Progress{Progress: &proto.ProgressUpdate{Message: "arbitrary wording", Kind: kind}}}
		got := decodeStreamResponse(response)
		if got == nil || got.ProgressKind != kind || got.Note != "arbitrary wording" {
			t.Fatalf("lost category: %+v", got)
		}
	}
}
