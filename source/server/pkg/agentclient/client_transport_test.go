package agentclient

import (
	"context"
	"testing"

	"google.golang.org/grpc"

	"cercano/source/server/pkg/proto"
)

// fakeAgentClientForRuntimeModels returns fake responses for ListRuntimeModels
type fakeAgentClientForRuntimeModels struct {
	proto.AgentClient          // embed; unused methods panic if called
	listRuntimeModelsFunc      func(ctx context.Context, in *proto.ListRuntimeModelsRequest, opts ...grpc.CallOption) (*proto.ListRuntimeModelsResponse, error)
	listCloudProfileModelsFunc func(ctx context.Context, in *proto.ListCloudProfileModelsRequest, opts ...grpc.CallOption) (*proto.ListCloudProfileModelsResponse, error)
}

func (f *fakeAgentClientForRuntimeModels) ListRuntimeModels(ctx context.Context, in *proto.ListRuntimeModelsRequest, opts ...grpc.CallOption) (*proto.ListRuntimeModelsResponse, error) {
	if f.listRuntimeModelsFunc != nil {
		return f.listRuntimeModelsFunc(ctx, in, opts...)
	}

	// Create a proto RuntimeModel with all the new fields
	model := &proto.RuntimeModel{
		Id:                 "test-model-id",
		DisplayName:        "Test Model",
		Runtime:            "test-runtime",
		Source:             "test-source",
		Path:               "/test/path",
		Format:             "gguf",
		Family:             "test-family",
		Quantization:       "q4",
		SizeBytes:          1000000,
		ModifiedAt:         "2024-01-01T00:00:00Z",
		DownloadState:      "downloaded",
		DownloadUrl:        "http://example.com/model.gguf",
		DownloadedBytes:    1000000,
		DownloadTotalBytes: 1000000,
		DownloadError:      "",
		RuntimeState:       "running",
		SupportsChat:       true,
		SupportsEmbed:      false,
		SupportsTools:      true,
		CatalogId:          "catalog:test",
		CatalogSource:      "test-catalog",
		Active:             true,
		KvBytesPerToken:    1000,
		MaxContextTokens:   8192,
		Acquisition:        "download",
		Publisher:          "Test Publisher",
		Kind:               "text-generation",
		Deprecated:         false,
		ReplacedBy:         "",
		PriceIn:            0, // downloadable model
		PriceOut:           0,
		ContextLength:      8192,
		Metadata: &proto.CatalogMetadata{
			DisplayName:   "A test model",
			ContextLength: 8192,
			Chat:          proto.CatalogCapability_SUPPORTED,
			Tools:         proto.CatalogCapability_SUPPORTED,
			Vision:        proto.CatalogCapability_UNSUPPORTED,
			Reasoning:     proto.CatalogCapability_UNSUPPORTED,
			Aliases:       []string{"test-alias"},
		},
		AccountObservations: []*proto.CatalogAccountObservation{
			{
				Profile:  "account-1",
				BaseUrl:  "https://api.example.com",
				Metadata: &proto.CatalogMetadata{DisplayName: "Account One Metadata"},
			},
			{
				Profile:  "account-2",
				BaseUrl:  "https://api.example.com",
				Metadata: &proto.CatalogMetadata{DisplayName: "Account Two Metadata"},
			},
		},
	}

	return &proto.ListRuntimeModelsResponse{
		Models:           []*proto.RuntimeModel{model},
		SystemRamBytes:   16000000000,
		CatalogUpdatedAt: "2024-01-01T00:00:00Z",
	}, nil
}

func (f *fakeAgentClientForRuntimeModels) ListCloudProfileModels(ctx context.Context, in *proto.ListCloudProfileModelsRequest, opts ...grpc.CallOption) (*proto.ListCloudProfileModelsResponse, error) {
	if f.listCloudProfileModelsFunc != nil {
		return f.listCloudProfileModelsFunc(ctx, in, opts...)
	}

	// Create a proto CloudModelInfo with all the new fields
	model := &proto.CloudModelInfo{
		Id:          "cloud-model-id",
		DisplayName: "Cloud Model",
		Metadata: &proto.CatalogMetadata{
			DisplayName:   "A cloud model",
			ContextLength: 4096,
			Chat:          proto.CatalogCapability_SUPPORTED,
			Tools:         proto.CatalogCapability_UNSUPPORTED,
			Vision:        proto.CatalogCapability_UNSUPPORTED,
			Reasoning:     proto.CatalogCapability_UNSUPPORTED,
			Aliases:       []string{"cloud-alias"},
		},
		AccountObservations: []*proto.CatalogAccountObservation{
			{
				Profile:  "cloud-account-1",
				BaseUrl:  "https://cloud.api.example.com",
				Metadata: &proto.CatalogMetadata{DisplayName: "Cloud Account Metadata"},
			},
		},
	}

	return &proto.ListCloudProfileModelsResponse{
		Models: []*proto.CloudModelInfo{model},
		Error:  "",
	}, nil
}

func TestListRuntimeModels_MissingMetadataAndObservations(t *testing.T) {
	fake := &fakeAgentClientForRuntimeModels{}
	c := &Client{agent: fake}

	ctx := context.Background()
	catalog, err := c.ListRuntimeModels(ctx)
	if err != nil {
		t.Fatalf("ListRuntimeModels failed: %v", err)
	}

	if len(catalog.Models) != 1 {
		t.Fatalf("Expected 1 model, got %d", len(catalog.Models))
	}

	model := catalog.Models[0]

	// These should be present but are currently missing due to incomplete mapping
	if model.Metadata == nil {
		t.Error("Metadata should be present but is nil")
	} else {
		if model.Metadata.DisplayName != "A test model" {
			t.Errorf("Expected Metadata.DisplayName='A test model', got '%s'", model.Metadata.DisplayName)
		}
		if model.Metadata.ContextLength != 8192 {
			t.Errorf("Expected Metadata.ContextLength=8192, got %d", model.Metadata.ContextLength)
		}
		if len(model.Metadata.Aliases) != 1 || model.Metadata.Aliases[0] != "test-alias" {
			t.Errorf("Expected Metadata.Aliases=['test-alias'], got %v", model.Metadata.Aliases)
		}
	}

	if len(model.AccountObservations) != 2 {
		t.Errorf("Expected 2 AccountObservations, got %d", len(model.AccountObservations))
	} else {
		// Check first observation
		obs1 := model.AccountObservations[0]
		if obs1.Profile != "account-1" {
			t.Errorf("Expected Profile='account-1', got '%s'", obs1.Profile)
		}
		if obs1.BaseURL != "https://api.example.com" {
			t.Errorf("Expected BaseURL='https://api.example.com', got '%s'", obs1.BaseURL)
		}
		if obs1.Metadata == nil {
			t.Error("obs1.Metadata should be present but is nil")
		} else if obs1.Metadata.DisplayName != "Account One Metadata" {
			t.Errorf("Expected obs1.Metadata.DisplayName='Account One Metadata', got '%s'", obs1.Metadata.DisplayName)
		}

		// Check second observation
		obs2 := model.AccountObservations[1]
		if obs2.Profile != "account-2" {
			t.Errorf("Expected Profile='account-2', got '%s'", obs2.Profile)
		}
		if obs2.Metadata == nil {
			t.Error("obs2.Metadata should be present but is nil")
		} else if obs2.Metadata.DisplayName != "Account Two Metadata" {
			t.Errorf("Expected obs2.Metadata.DisplayName='Account Two Metadata', got '%s'", obs2.Metadata.DisplayName)
		}
	}

	// Check that price fields are preserved (0 for downloadable model)
	if model.PriceIn != 0 {
		t.Errorf("Expected PriceIn=0 for downloadable model, got %d", model.PriceIn)
	}
	if model.PriceOut != 0 {
		t.Errorf("Expected PriceOut=0 for downloadable model, got %d", model.PriceOut)
	}
}

func TestListCloudProfileModels_MissingMetadataAndObservations(t *testing.T) {
	fake := &fakeAgentClientForRuntimeModels{}
	c := &Client{agent: fake}

	ctx := context.Background()
	models, error, err := c.ListCloudProfileModels(ctx, "test-profile")
	if err != nil {
		t.Fatalf("ListCloudProfileModels failed: %v", err)
	}

	if error != "" {
		t.Fatalf("Expected no error, got '%s'", error)
	}

	if len(models) != 1 {
		t.Fatalf("Expected 1 model, got %d", len(models))
	}

	model := models[0]

	// These should be present but are currently missing due to incomplete mapping
	if model.Metadata == nil {
		t.Error("Metadata should be present but is nil")
	} else {
		if model.Metadata.DisplayName != "A cloud model" {
			t.Errorf("Expected Metadata.DisplayName='A cloud model', got '%s'", model.Metadata.DisplayName)
		}
		if model.Metadata.ContextLength != 4096 {
			t.Errorf("Expected Metadata.ContextLength=4096, got %d", model.Metadata.ContextLength)
		}
		if len(model.Metadata.Aliases) != 1 || model.Metadata.Aliases[0] != "cloud-alias" {
			t.Errorf("Expected Metadata.Aliases=['cloud-alias'], got %v", model.Metadata.Aliases)
		}
	}

	if len(model.AccountObservations) != 1 {
		t.Errorf("Expected 1 AccountObservation, got %d", len(model.AccountObservations))
	} else {
		obs := model.AccountObservations[0]
		if obs.Profile != "cloud-account-1" {
			t.Errorf("Expected Profile='cloud-account-1', got '%s'", obs.Profile)
		}
		if obs.BaseURL != "https://cloud.api.example.com" {
			t.Errorf("Expected BaseURL='https://cloud.api.example.com', got '%s'", obs.BaseURL)
		}
		if obs.Metadata == nil {
			t.Error("obs.Metadata should be present but is nil")
		} else if obs.Metadata.DisplayName != "Cloud Account Metadata" {
			t.Errorf("Expected obs.Metadata.DisplayName='Cloud Account Metadata', got '%s'", obs.Metadata.DisplayName)
		}
	}
}

func TestListRuntimeModels_PricePreservation(t *testing.T) {
	fake := &fakeAgentClientForRuntimeModels{}
	c := &Client{agent: fake}

	// Create a served model with prices
	servedModel := &proto.RuntimeModel{
		Id:          "served-model-id",
		DisplayName: "Served Model",
		Acquisition: "serve",
		PriceIn:     1000, // $0.001 per 1M tokens
		PriceOut:    2000, // $0.002 per 1M tokens
		AccountObservations: []*proto.CatalogAccountObservation{
			{
				Profile:  "served-account",
				BaseUrl:  "https://served.api.example.com",
				Metadata: &proto.CatalogMetadata{DisplayName: "Served Account Metadata"},
			},
		},
	}

	// Override the ListRuntimeModels method
	fake.listRuntimeModelsFunc = func(ctx context.Context, in *proto.ListRuntimeModelsRequest, opts ...grpc.CallOption) (*proto.ListRuntimeModelsResponse, error) {
		return &proto.ListRuntimeModelsResponse{
			Models:           []*proto.RuntimeModel{servedModel},
			SystemRamBytes:   16000000000,
			CatalogUpdatedAt: "2024-01-01T00:00:00Z",
		}, nil
	}

	ctx := context.Background()
	catalog, err := c.ListRuntimeModels(ctx)
	if err != nil {
		t.Fatalf("ListRuntimeModels failed: %v", err)
	}

	if len(catalog.Models) != 1 {
		t.Fatalf("Expected 1 model, got %d", len(catalog.Models))
	}

	model := catalog.Models[0]

	// Check that prices are preserved for served models
	if model.PriceIn != 1000 {
		t.Errorf("Expected PriceIn=1000 for served model, got %d", model.PriceIn)
	}
	if model.PriceOut != 2000 {
		t.Errorf("Expected PriceOut=2000 for served model, got %d", model.PriceOut)
	}

	// Check that account observations are preserved
	if len(model.AccountObservations) != 1 {
		t.Errorf("Expected 1 AccountObservation, got %d", len(model.AccountObservations))
	} else {
		obs := model.AccountObservations[0]
		if obs.Profile != "served-account" {
			t.Errorf("Expected Profile='served-account', got '%s'", obs.Profile)
		}
		if obs.Metadata == nil {
			t.Error("obs.Metadata should be present but is nil")
		} else if obs.Metadata.DisplayName != "Served Account Metadata" {
			t.Errorf("Expected obs.Metadata.DisplayName='Served Account Metadata', got '%s'", obs.Metadata.DisplayName)
		}
	}
}
