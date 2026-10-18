package agentclient

import (
	"context"
	"errors"
	"testing"

	"google.golang.org/grpc"

	"cercano/source/server/pkg/proto"
)

// healthFakeAgentClient returns a canned GetAgentHealth response/error.
type healthFakeAgentClient struct {
	proto.AgentClient // embed; unused methods panic if called
	resp              *proto.GetAgentHealthResponse
	err               error
}

func (f *healthFakeAgentClient) GetAgentHealth(ctx context.Context, in *proto.GetAgentHealthRequest, opts ...grpc.CallOption) (*proto.GetAgentHealthResponse, error) {
	return f.resp, f.err
}

func TestHealthMapsResponse(t *testing.T) {
	fake := &healthFakeAgentClient{resp: &proto.GetAgentHealthResponse{
		Version:        "v9.8.7",
		Pid:            4242,
		Ready:          false,
		NotReadyReasons: []string{"admission_paused"},
	}}
	c := &Client{agent: fake}
	h, err := c.Health(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if h.Version != "v9.8.7" || h.Pid != 4242 || h.Ready {
		t.Fatalf("health = %+v", h)
	}
	if len(h.NotReadyReasons) != 1 || h.NotReadyReasons[0] != "admission_paused" {
		t.Fatalf("reasons = %v", h.NotReadyReasons)
	}
	// Mapping must deep-copy: a caller mutating the returned slice must not
	// corrupt the shared proto response (which may be reused).
	h.NotReadyReasons[0] = "mutated"
	if fake.resp.NotReadyReasons[0] != "admission_paused" {
		t.Fatal("NotReadyReasons not deep-copied")
	}
}

func TestHealthPropagatesError(t *testing.T) {
	wantErr := errors.New("rpc error")
	c := &Client{agent: &healthFakeAgentClient{err: wantErr}}
	if _, err := c.Health(context.Background()); !errors.Is(err, wantErr) {
		t.Fatalf("err = %v; want %v", err, wantErr)
	}
}

func TestHealthNotConnected(t *testing.T) {
	c := &Client{}
	if _, err := c.Health(context.Background()); err == nil {
		t.Fatal("nil agent client: want error, got nil")
	}
}
