package modelmetadata_test

import (
	"testing"

	"cercano/source/server/internal/modelmetadata"
)

func TestIdentityEquality(t *testing.T) {
	identity1 := modelmetadata.Identity{
		Profile:  "profile1",
		Provider: "provider1",
		BaseURL:  "https://api1.example.com",
		Route:    "v1",
		Model:    "model1",
	}

	identity2 := modelmetadata.Identity{
		Profile:  "profile1",
		Provider: "provider1",
		BaseURL:  "https://api1.example.com",
		Route:    "v1",
		Model:    "model1",
	}

	identity3 := modelmetadata.Identity{
		Profile:  "profile2",
		Provider: "provider1",
		BaseURL:  "https://api1.example.com",
		Route:    "v1",
		Model:    "model1",
	}

	identity4 := modelmetadata.Identity{
		Profile:  "profile1",
		Provider: "provider1",
		BaseURL:  "https://api2.example.com",
		Route:    "v1",
		Model:    "model1",
	}

	identity5 := modelmetadata.Identity{
		Profile:  "profile1",
		Provider: "provider1",
		BaseURL:  "https://api1.example.com",
		Route:    "v2",
		Model:    "model1",
	}

	identity6 := modelmetadata.Identity{
		Profile:  "profile1",
		Provider: "provider1",
		BaseURL:  "https://api1.example.com",
		Route:    "v1",
		Model:    "model2",
	}

	// Same identities should be equal
	if identity1 != identity2 {
		t.Error("identical identities should be equal")
	}

	// Different profiles should not be equal
	if identity1 == identity3 {
		t.Error("identities with different profiles should not be equal")
	}

	// Different BaseURLs should not be equal
	if identity1 == identity4 {
		t.Error("identities with different BaseURLs should not be equal")
	}

	// Different Routes should not be equal
	if identity1 == identity5 {
		t.Error("identities with different Routes should not be equal")
	}

	// Different Models should not be equal
	if identity1 == identity6 {
		t.Error("identities with different Models should not be equal")
	}
}
