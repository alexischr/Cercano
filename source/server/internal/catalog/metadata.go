package catalog

import (
	"time"
)

// Capability represents the support status of a model feature
type Capability int

const (
	// CapabilityUnknown is the zero value: the source did not report this capability
	CapabilityUnknown Capability = iota
	// CapabilitySupported means the model has this feature
	CapabilitySupported
	// CapabilityUnsupported means the model does not have this feature
	CapabilityUnsupported
)

// String renders the capability for logs and test failures
func (c Capability) String() string {
	switch c {
	case CapabilitySupported:
		return "supported"
	case CapabilityUnsupported:
		return "unsupported"
	default:
		return "unknown"
	}
}

// Metadata contains metadata about a model's capabilities and pricing
type Metadata struct {
	// DisplayName is the human-readable name of the model
	DisplayName string
	// ContextLength is the model's context window size
	ContextLength int
	// Chat is the capability for chat/completion functionality
	Chat Capability
	// Tools is the capability for function/tool calling
	Tools Capability
	// Vision is the capability for image input
	Vision Capability
	// Reasoning is the capability for reasoning features
	Reasoning Capability
	// Aliases are alternative names for the model
	Aliases []string
	// Deprecated marks whether the model has been retired
	Deprecated bool
	// RetirementAt is when the model will be retired (zero time means unknown or not supplied)
	RetirementAt time.Time
	// ReplacedBy names the successor model when Deprecated is set
	ReplacedBy string
	// PriceIn is the per-million-token cost in micro-USD for input tokens
	// nil means unknown, explicit 0 means known free
	PriceIn *int64
	// PriceOut is the per-million-token cost in micro-USD for output tokens
	// nil means unknown, explicit 0 means known free
	PriceOut *int64
}

// AccountObservation contains metadata about a model as observed by a specific account
type AccountObservation struct {
	// Profile is the account profile that made this observation
	Profile string
	// BaseURL is the endpoint URL used for this observation
	BaseURL string
	// Metadata is the model metadata as observed by this account
	Metadata Metadata
}

// AgentEligible checks if a model is eligible for agent use at the given time
func (m Metadata) AgentEligible(now time.Time) bool {
	// Must have affirmative chat and tools capabilities
	if m.Chat != CapabilitySupported || m.Tools != CapabilitySupported {
		return false
	}

	// Must not be deprecated
	if m.Deprecated {
		return false
	}

	// If retirement is set, it must be in the future
	if !m.RetirementAt.IsZero() && !m.RetirementAt.After(now) {
		return false
	}

	return true
}

// Present reports whether any metadata was explicitly supplied.
func (m Metadata) Present() bool {
	return m.DisplayName != "" || m.ContextLength != 0 || m.Chat != CapabilityUnknown || m.Tools != CapabilityUnknown || m.Vision != CapabilityUnknown || m.Reasoning != CapabilityUnknown || len(m.Aliases) != 0 || m.Deprecated || !m.RetirementAt.IsZero() || m.ReplacedBy != "" || m.PriceIn != nil || m.PriceOut != nil
}
