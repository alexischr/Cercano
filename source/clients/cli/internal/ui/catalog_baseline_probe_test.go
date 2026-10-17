package ui

import (
	"cercano/source/server/pkg/agentclient"
	"cercano/source/server/pkg/proto"
	"context"
	"google.golang.org/grpc"
	"net"
	"testing"
	"time"
)

type catalogBaselineStub struct {
	proto.UnimplementedAgentServer
	called  chan struct{}
	release chan struct{}
}

func (s *catalogBaselineStub) ListCloudProfileModels(ctx context.Context, _ *proto.ListCloudProfileModelsRequest) (*proto.ListCloudProfileModelsResponse, error) {
	close(s.called)
	select {
	case <-s.release:
	case <-ctx.Done():
	}
	return &proto.ListCloudProfileModelsResponse{Error: "no API key"}, nil
}
func TestCatalogBaselineProbe(t *testing.T) {
	l, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	s := grpc.NewServer()
	stub := &catalogBaselineStub{called: make(chan struct{}), release: make(chan struct{})}
	proto.RegisterAgentServer(s, stub)
	go s.Serve(l)
	defer s.Stop()
	c, e := agentclient.DialExisting(context.Background(), l.Addr().String())
	if e != nil {
		t.Fatal(e)
	}
	defer c.Close()
	p := draftTestPage()
	p.agent = c
	p.cloudDraft.Name = "no-key-account"
	done := make(chan struct{})
	go func() { p.cloudModelOptions(cloudRow{IsProfile: true, HasKey: false}, ""); close(done) }()
	select {
	case <-stub.called:
	case <-time.After(time.Second):
		t.Fatal("probe did not reach server")
	}
	select {
	case <-done:
		t.Fatal("expected synchronous call still blocked")
	default:
		t.Log("Observed: building model options blocks until catalog RPC finishes, even with HasKey=false")
	}
	close(stub.release)
	<-done
	t.Logf("Observed missing-key discovery state: fetched=%v, generic error=%q", p.cloudModelsFetched, p.cloudModelsError)
	label := cloudModelMetadataLabel(agentclient.CloudModelInfo{Metadata: &agentclient.CatalogMetadata{ContextLength: 200000, Aliases: []string{"model-alias", "another-alias"}, Deprecated: true, ReplacedBy: "replacement-model"}}, "Example model")
	if len(label) < 100 {
		t.Fatal("expected oversized metadata label")
	}
	t.Logf("Observed metadata-packed dropdown label: %d bytes: %s", len(label), label)
}
