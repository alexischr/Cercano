package agentclient

import (
	"cercano/source/server/pkg/proto"
	"context"
	"google.golang.org/grpc"
	"reflect"
	"testing"
)

func TestCatalogMetadataPricePresence(t *testing.T) {
	zero := int64(0)
	for _, price := range []*int64{nil, &zero} {
		got := catalogMetadataFromProto(&proto.CatalogMetadata{PriceIn: price, PriceOut: price})
		if !reflect.DeepEqual(got.PriceIn, price) || !reflect.DeepEqual(got.PriceOut, price) {
			t.Fatalf("prices lost presence: input=%v output=%#v", price, got)
		}
		round := catalogMetadataToProto(got)
		if !reflect.DeepEqual(round.PriceIn, price) || !reflect.DeepEqual(round.PriceOut, price) {
			t.Fatalf("roundtrip lost presence: input=%v output=%v", price, round)
		}
	}
}

func TestCatalogAccountTransportEntryPoint(t *testing.T) {
	zero := int64(0)
	fake := &fakeAgentClientForRuntimeModels{}
	fake.listRuntimeModelsFunc = func(ctx context.Context, req *proto.ListRuntimeModelsRequest, opts ...grpc.CallOption) (*proto.ListRuntimeModelsResponse, error) {
		if req.ProfileName != "work" {
			t.Fatalf("filter not sent: %q", req.ProfileName)
		}
		return &proto.ListRuntimeModelsResponse{
			Models:               []*proto.RuntimeModel{{Id: "x", Metadata: &proto.CatalogMetadata{PriceOut: &zero}, AccountObservations: []*proto.CatalogAccountObservation{{Profile: "work", Metadata: &proto.CatalogMetadata{ContextLength: 8192}}}}},
			CatalogAccountStatus: []*proto.CatalogAccountStatus{{Profile: "work", Error: "unavailable"}},
		}, nil
	}
	c := &Client{agent: fake}
	got, err := c.ListRuntimeModelsForProfile(context.Background(), "work")
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Models) != 1 || got.Models[0].Metadata == nil {
		t.Fatalf("missing models/metadata: %+v", got)
	}
	m := got.Models[0]
	if m.Metadata.PriceIn != nil || m.Metadata.PriceOut == nil || *m.Metadata.PriceOut != 0 {
		t.Fatalf("lost price presence: %+v", m.Metadata)
	}
	if len(m.AccountObservations) != 1 || m.AccountObservations[0].Profile != "work" {
		t.Fatalf("lost access: %+v", m)
	}
	if len(got.CatalogAccountStatus) != 1 || got.CatalogAccountStatus[0].Error != "unavailable" {
		t.Fatalf("lost status: %+v", got)
	}
}
