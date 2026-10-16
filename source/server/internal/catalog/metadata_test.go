package catalog_test

import (
	"encoding/json"
	"testing"
	"time"

	"cercano/source/server/internal/catalog"
)

func TestCapabilityString(t *testing.T) {
	tests := []struct {
		name     string
		cap      catalog.Capability
		expected string
	}{
		{"unknown", catalog.CapabilityUnknown, "unknown"},
		{"supported", catalog.CapabilitySupported, "supported"},
		{"unsupported", catalog.CapabilityUnsupported, "unsupported"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.cap.String(); got != tt.expected {
				t.Errorf("Capability.String() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestAgentEligible(t *testing.T) {
	now := time.Now()

	tests := []struct {
		name     string
		metadata catalog.Metadata
		expected bool
	}{
		{
			name: "eligible - all requirements met",
			metadata: catalog.Metadata{
				Chat:       catalog.CapabilitySupported,
				Tools:      catalog.CapabilitySupported,
				Deprecated: false,
			},
			expected: true,
		},
		{
			name: "not eligible - missing chat",
			metadata: catalog.Metadata{
				Chat:       catalog.CapabilityUnsupported,
				Tools:      catalog.CapabilitySupported,
				Deprecated: false,
			},
			expected: false,
		},
		{
			name: "not eligible - missing tools",
			metadata: catalog.Metadata{
				Chat:       catalog.CapabilitySupported,
				Tools:      catalog.CapabilityUnsupported,
				Deprecated: false,
			},
			expected: false,
		},
		{
			name: "not eligible - deprecated",
			metadata: catalog.Metadata{
				Chat:       catalog.CapabilitySupported,
				Tools:      catalog.CapabilitySupported,
				Deprecated: true,
			},
			expected: false,
		},
		{
			name: "not eligible - retired in past",
			metadata: catalog.Metadata{
				Chat:         catalog.CapabilitySupported,
				Tools:        catalog.CapabilitySupported,
				Deprecated:   false,
				RetirementAt: now.Add(-24 * time.Hour),
			},
			expected: false,
		},
		{
			name:     "not eligible - retirement at exact boundary",
			metadata: catalog.Metadata{Chat: catalog.CapabilitySupported, Tools: catalog.CapabilitySupported, RetirementAt: now},
			expected: false,
		},
		{
			name: "eligible - retirement in future",
			metadata: catalog.Metadata{
				Chat:         catalog.CapabilitySupported,
				Tools:        catalog.CapabilitySupported,
				Deprecated:   false,
				RetirementAt: now.Add(24 * time.Hour),
			},
			expected: true,
		},
		{
			name: "eligible - no retirement date",
			metadata: catalog.Metadata{
				Chat:       catalog.CapabilitySupported,
				Tools:      catalog.CapabilitySupported,
				Deprecated: false,
			},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.metadata.AgentEligible(now); got != tt.expected {
				t.Errorf("Metadata.AgentEligible() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestMetadataJSONRoundtrip(t *testing.T) {
	now := time.Now()

	original := catalog.Metadata{
		DisplayName:   "test-model",
		ContextLength: 8192,
		Chat:          catalog.CapabilitySupported,
		Tools:         catalog.CapabilitySupported,
		Vision:        catalog.CapabilityUnsupported,
		Reasoning:     catalog.CapabilityUnknown,
		Aliases:       []string{"alias1", "alias2"},
		Deprecated:    true,
		RetirementAt:  now.Add(24 * time.Hour),
		ReplacedBy:    "new-model",
		PriceIn:       int64Ptr(1000),
		PriceOut:      int64Ptr(2000),
	}

	// Test JSON serialization and deserialization
	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}

	var decoded catalog.Metadata
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}

	// Compare fields
	if decoded.DisplayName != original.DisplayName {
		t.Errorf("DisplayName = %v, want %v", decoded.DisplayName, original.DisplayName)
	}
	if decoded.ContextLength != original.ContextLength {
		t.Errorf("ContextLength = %v, want %v", decoded.ContextLength, original.ContextLength)
	}
	if decoded.Chat != original.Chat {
		t.Errorf("Chat = %v, want %v", decoded.Chat, original.Chat)
	}
	if decoded.Tools != original.Tools {
		t.Errorf("Tools = %v, want %v", decoded.Tools, original.Tools)
	}
	if decoded.Vision != original.Vision {
		t.Errorf("Vision = %v, want %v", decoded.Vision, original.Vision)
	}
	if decoded.Reasoning != original.Reasoning {
		t.Errorf("Reasoning = %v, want %v", decoded.Reasoning, original.Reasoning)
	}
	if len(decoded.Aliases) != len(original.Aliases) {
		t.Errorf("Aliases length = %v, want %v", len(decoded.Aliases), len(original.Aliases))
	} else {
		for i, alias := range decoded.Aliases {
			if alias != original.Aliases[i] {
				t.Errorf("Aliases[%d] = %v, want %v", i, alias, original.Aliases[i])
			}
		}
	}
	if decoded.Deprecated != original.Deprecated {
		t.Errorf("Deprecated = %v, want %v", decoded.Deprecated, original.Deprecated)
	}
	if !decoded.RetirementAt.Equal(original.RetirementAt) {
		t.Errorf("RetirementAt = %v, want %v", decoded.RetirementAt, original.RetirementAt)
	}
	if decoded.ReplacedBy != original.ReplacedBy {
		t.Errorf("ReplacedBy = %v, want %v", decoded.ReplacedBy, original.ReplacedBy)
	}
	if decoded.PriceIn == nil && original.PriceIn != nil {
		t.Errorf("PriceIn = nil, want %v", *original.PriceIn)
	} else if decoded.PriceIn != nil && original.PriceIn == nil {
		t.Errorf("PriceIn = %v, want nil", *decoded.PriceIn)
	} else if decoded.PriceIn != nil && original.PriceIn != nil && *decoded.PriceIn != *original.PriceIn {
		t.Errorf("PriceIn = %v, want %v", *decoded.PriceIn, *original.PriceIn)
	}
	if decoded.PriceOut == nil && original.PriceOut != nil {
		t.Errorf("PriceOut = nil, want %v", *original.PriceOut)
	} else if decoded.PriceOut != nil && original.PriceOut == nil {
		t.Errorf("PriceOut = %v, want nil", *decoded.PriceOut)
	} else if decoded.PriceOut != nil && original.PriceOut != nil && *decoded.PriceOut != *original.PriceOut {
		t.Errorf("PriceOut = %v, want %v", *decoded.PriceOut, *original.PriceOut)
	}
}

func TestAccountObservationJSONRoundtrip(t *testing.T) {
	now := time.Now()

	original := catalog.AccountObservation{
		Profile: "test-profile",
		BaseURL: "https://api.example.com",
		Metadata: catalog.Metadata{
			DisplayName:   "test-model",
			ContextLength: 8192,
			Chat:          catalog.CapabilitySupported,
			Tools:         catalog.CapabilitySupported,
			Vision:        catalog.CapabilityUnsupported,
			Reasoning:     catalog.CapabilityUnknown,
			Aliases:       []string{"alias1", "alias2"},
			Deprecated:    true,
			RetirementAt:  now.Add(24 * time.Hour),
			ReplacedBy:    "new-model",
			PriceIn:       int64Ptr(1000),
			PriceOut:      int64Ptr(2000),
		},
	}

	// Test JSON serialization and deserialization
	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}

	var decoded catalog.AccountObservation
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}

	// Compare fields
	if decoded.Profile != original.Profile {
		t.Errorf("Profile = %v, want %v", decoded.Profile, original.Profile)
	}
	if decoded.BaseURL != original.BaseURL {
		t.Errorf("BaseURL = %v, want %v", decoded.BaseURL, original.BaseURL)
	}

	// Compare nested metadata
	if decoded.Metadata.DisplayName != original.Metadata.DisplayName {
		t.Errorf("Metadata.DisplayName = %v, want %v", decoded.Metadata.DisplayName, original.Metadata.DisplayName)
	}
	if decoded.Metadata.ContextLength != original.Metadata.ContextLength {
		t.Errorf("Metadata.ContextLength = %v, want %v", decoded.Metadata.ContextLength, original.Metadata.ContextLength)
	}
	if decoded.Metadata.Chat != original.Metadata.Chat {
		t.Errorf("Metadata.Chat = %v, want %v", decoded.Metadata.Chat, original.Metadata.Chat)
	}
	if decoded.Metadata.Tools != original.Metadata.Tools {
		t.Errorf("Metadata.Tools = %v, want %v", decoded.Metadata.Tools, original.Metadata.Tools)
	}
	if decoded.Metadata.Vision != original.Metadata.Vision {
		t.Errorf("Metadata.Vision = %v, want %v", decoded.Metadata.Vision, original.Metadata.Vision)
	}
	if decoded.Metadata.Reasoning != original.Metadata.Reasoning {
		t.Errorf("Metadata.Reasoning = %v, want %v", decoded.Metadata.Reasoning, original.Metadata.Reasoning)
	}
	if len(decoded.Metadata.Aliases) != len(original.Metadata.Aliases) {
		t.Errorf("Metadata.Aliases length = %v, want %v", len(decoded.Metadata.Aliases), len(original.Metadata.Aliases))
	} else {
		for i, alias := range decoded.Metadata.Aliases {
			if alias != original.Metadata.Aliases[i] {
				t.Errorf("Metadata.Aliases[%d] = %v, want %v", i, alias, original.Metadata.Aliases[i])
			}
		}
	}
	if decoded.Metadata.Deprecated != original.Metadata.Deprecated {
		t.Errorf("Metadata.Deprecated = %v, want %v", decoded.Metadata.Deprecated, original.Metadata.Deprecated)
	}
	if !decoded.Metadata.RetirementAt.Equal(original.Metadata.RetirementAt) {
		t.Errorf("Metadata.RetirementAt = %v, want %v", decoded.Metadata.RetirementAt, original.Metadata.RetirementAt)
	}
	if decoded.Metadata.ReplacedBy != original.Metadata.ReplacedBy {
		t.Errorf("Metadata.ReplacedBy = %v, want %v", decoded.Metadata.ReplacedBy, original.Metadata.ReplacedBy)
	}
	if decoded.Metadata.PriceIn == nil && original.Metadata.PriceIn != nil {
		t.Errorf("Metadata.PriceIn = nil, want %v", *original.Metadata.PriceIn)
	} else if decoded.Metadata.PriceIn != nil && original.Metadata.PriceIn == nil {
		t.Errorf("Metadata.PriceIn = %v, want nil", *decoded.Metadata.PriceIn)
	} else if decoded.Metadata.PriceIn != nil && original.Metadata.PriceIn != nil && *decoded.Metadata.PriceIn != *original.Metadata.PriceIn {
		t.Errorf("Metadata.PriceIn = %v, want %v", *decoded.Metadata.PriceIn, *original.Metadata.PriceIn)
	}
	if decoded.Metadata.PriceOut == nil && original.Metadata.PriceOut != nil {
		t.Errorf("Metadata.PriceOut = nil, want %v", *original.Metadata.PriceOut)
	} else if decoded.Metadata.PriceOut != nil && original.Metadata.PriceOut == nil {
		t.Errorf("Metadata.PriceOut = %v, want nil", *decoded.Metadata.PriceOut)
	} else if decoded.Metadata.PriceOut != nil && original.Metadata.PriceOut != nil && *decoded.Metadata.PriceOut != *original.Metadata.PriceOut {
		t.Errorf("Metadata.PriceOut = %v, want %v", *decoded.Metadata.PriceOut, *original.Metadata.PriceOut)
	}
}

func TestDifferingAccountObservations(t *testing.T) {
	now := time.Now()

	observation1 := catalog.AccountObservation{
		Profile: "profile1",
		BaseURL: "https://api1.example.com",
		Metadata: catalog.Metadata{
			DisplayName:   "test-model",
			ContextLength: 8192,
			Chat:          catalog.CapabilitySupported,
			Tools:         catalog.CapabilitySupported,
			Vision:        catalog.CapabilityUnsupported,
			Reasoning:     catalog.CapabilityUnknown,
			Aliases:       []string{"alias1"},
			Deprecated:    false,
			RetirementAt:  time.Time{}, // never
			PriceIn:       int64Ptr(1000),
			PriceOut:      int64Ptr(2000),
		},
	}

	observation2 := catalog.AccountObservation{
		Profile: "profile2",
		BaseURL: "https://api2.example.com",
		Metadata: catalog.Metadata{
			DisplayName:   "test-model",
			ContextLength: 16384,
			Chat:          catalog.CapabilitySupported,
			Tools:         catalog.CapabilitySupported,
			Vision:        catalog.CapabilitySupported,
			Reasoning:     catalog.CapabilitySupported,
			Aliases:       []string{"alias2"},
			Deprecated:    false,
			RetirementAt:  now.Add(24 * time.Hour),
			PriceIn:       int64Ptr(1500),
			PriceOut:      int64Ptr(2500),
		},
	}

	// Both observations should be preserved
	if observation1.Profile == observation2.Profile {
		t.Error("Profiles should be different")
	}
	if observation1.BaseURL == observation2.BaseURL {
		t.Error("BaseURLs should be different")
	}
	if observation1.Metadata.ContextLength == observation2.Metadata.ContextLength {
		t.Error("ContextLengths should be different")
	}
	if observation1.Metadata.Vision == observation2.Metadata.Vision {
		t.Error("Vision capabilities should be different")
	}
	if observation1.Metadata.Reasoning == observation2.Metadata.Reasoning {
		t.Error("Reasoning capabilities should be different")
	}
	aliasesDifferent := false
	if len(observation1.Metadata.Aliases) != len(observation2.Metadata.Aliases) {
		aliasesDifferent = true
	} else {
		for i, alias := range observation1.Metadata.Aliases {
			if alias != observation2.Metadata.Aliases[i] {
				aliasesDifferent = true
				break
			}
		}
	}
	if !aliasesDifferent {
		t.Error("Aliases should be different")
	}
	if observation1.Metadata.RetirementAt.IsZero() == observation2.Metadata.RetirementAt.IsZero() {
		t.Error("RetirementAt should be different")
	}
	if observation1.Metadata.PriceIn == nil || observation2.Metadata.PriceIn == nil || *observation1.Metadata.PriceIn == *observation2.Metadata.PriceIn {
		t.Error("PriceIn should be different")
	}
	if observation1.Metadata.PriceOut == nil || observation2.Metadata.PriceOut == nil || *observation1.Metadata.PriceOut == *observation2.Metadata.PriceOut {
		t.Error("PriceOut should be different")
	}
}

func int64Ptr(i int64) *int64 {
	return &i
}
