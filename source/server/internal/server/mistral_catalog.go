package server

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"

	"cercano/source/server/internal/catalog"
	"cercano/source/server/internal/mistralcatalog"
	"cercano/source/server/internal/modelevidence"
	"cercano/source/server/internal/modelmetadata"
	"cercano/source/server/pkg/config"
	"cercano/source/server/pkg/proto"
)

func isMistralProfile(p config.CloudProfile) bool {
	if p.Flavor != "chat_completions" || p.Route != "" {
		return false
	}
	if p.Provider == "mistral" {
		return true
	}
	u, err := url.Parse(p.BaseURL)
	return err == nil && u.User == nil && strings.EqualFold(u.Hostname(), "api.mistral.ai")
}

// lockMistralConfiguration serializes profile edits against catalog synchronization.
// The returned idempotent release permits unlocking before provider rebuilds,
// which can themselves request model evidence.
func (s *Server) lockMistralConfiguration() func() {
	s.mistralMu.Lock()
	locked := true
	return func() {
		if locked {
			locked = false
			s.mistralMu.Unlock()
		}
	}
}
func (s *Server) invalidateMistralLocked(profile string) {
	if s.mistralCatalog != nil {
		s.mistralCatalog.Invalidate(profile)
	}
}

func (s *Server) mistralModels(ctx context.Context, profile string) ([]catalog.Model, []mistralcatalog.Status) {
	return s.mistralModelsRefresh(ctx, profile, false)
}

func (s *Server) mistralModelsRefresh(ctx context.Context, profile string, refresh bool) ([]catalog.Model, []mistralcatalog.Status) {
	s.mistralMu.Lock()
	if s.mistralCatalog == nil {
		s.mistralCatalog = mistralcatalog.New(nil)
	}
	store := s.mistralCatalog
	var accounts []mistralcatalog.Account
	if s.cfgSvc != nil {
		for _, p := range s.cfgSvc.Get().CloudProfiles {
			if !isMistralProfile(p) {
				continue
			}
			key := ""
			if secrets := s.cfgSvc.Secrets(); secrets != nil {
				key, _ = secrets.Get(p.Name)
			}
			accounts = append(accounts, mistralcatalog.Account{Name: p.Name, BaseURL: p.BaseURL, Key: key})
		}
	}
	store.Sync(accounts)
	if refresh {
		store.RefreshProfile(profile)
	}
	s.mistralMu.Unlock()
	return store.List(ctx, profile)
}

func (s *Server) listMistralProfileModels(ctx context.Context, profile string) *proto.ListCloudProfileModelsResponse {
	models, status := s.mistralModels(ctx, profile)
	out := &proto.ListCloudProfileModelsResponse{}
	for _, st := range status {
		if st.Error != "" {
			out.Error = st.Error
			return out
		}
	}
	for _, m := range models {
		if !m.Metadata.AgentEligible(time.Now()) {
			continue
		}
		name := m.Metadata.DisplayName
		if name == "" {
			name = m.ID
		}
		entry := &proto.CloudModelInfo{Id: m.ID, DisplayName: name, Metadata: catalogMetadataToProto(m.Metadata)}
		for _, obs := range m.Observations {
			entry.AccountObservations = append(entry.AccountObservations, catalogAccountObservationToProto(obs))
		}
		out.Models = append(out.Models, entry)
	}
	return out
}

func (s *Server) appendMistralModels(ctx context.Context, profile string, out *proto.ListRuntimeModelsResponse) {
	models, status := s.mistralModels(ctx, profile)
	for _, m := range models {
		row := catalogModelToProto(m, true)
		// Combined rows make no provider-wide capability claims. Account observations
		// carry the individual values and the UI filters using the intended account.
		row.SupportsChat = m.Metadata.Chat == catalog.CapabilitySupported
		if m.Metadata.DisplayName != "" {
			row.DisplayName = m.Metadata.DisplayName
		}
		out.Models = append(out.Models, row)
	}
	for _, st := range status {
		row := &proto.CatalogAccountStatus{Profile: st.Profile, Error: st.Error}
		if !st.RefreshedAt.IsZero() {
			row.RefreshedAt = st.RefreshedAt.Format(time.RFC3339)
		}
		out.CatalogAccountStatus = append(out.CatalogAccountStatus, row)
	}
}

// mistralEvidence deliberately bypasses the generic evidence cache: authenticated
// observations have a separate credential lifecycle and must not outlive it.
func (s *Server) mistralEvidence(ctx context.Context, id modelmetadata.Identity) (modelmetadata.Evidence, bool) {
	u, _ := url.Parse(id.BaseURL)
	official := u != nil && strings.EqualFold(u.Hostname(), "api.mistral.ai")
	if s.cfgSvc == nil {
		return modelmetadata.Evidence{}, official
	}
	p, exists := s.cfgSvc.Get().Profile(id.Profile)
	if !exists || !isMistralProfile(p) {
		return modelmetadata.Evidence{}, official
	}
	if modelevidence.IdentityFor(p, id.Model) != id {
		return modelmetadata.Evidence{}, true
	}
	models, _ := s.mistralModels(ctx, p.Name)
	for _, m := range models {
		if m.ID != id.Model {
			continue
		}
		ev := modelmetadata.Evidence{ContextWindow: m.Metadata.ContextLength}
		switch m.Metadata.Vision {
		case catalog.CapabilitySupported:
			ev.Vision = modelmetadata.VisionSupported
		case catalog.CapabilityUnsupported:
			ev.Vision = modelmetadata.VisionUnsupported
		}
		return ev, true
	}
	return modelmetadata.Evidence{}, true
}

// validateMistralSelection confirms exact model access for the requested account.
// A stale picker entry must not change accounts or install an unavailable route.
func (s *Server) validateMistralSelection(ctx context.Context, profile, model string) error {
	p, ok := s.cfgSvc.Get().Profile(profile)
	if !ok || !isMistralProfile(p) {
		return nil
	}
	models, status := s.mistralModelsRefresh(ctx, profile, true)
	for _, st := range status {
		if st.Error != "" {
			return fmt.Errorf("Mistral model access could not be confirmed for account %q: %s", profile, st.Error)
		}
	}
	for _, m := range models {
		if m.ID == model {
			if !m.Metadata.AgentEligible(time.Now()) {
				return fmt.Errorf("Mistral model %q is retired or lacks confirmed chat/tool support for account %q", model, profile)
			}
			return nil
		}
	}
	return fmt.Errorf("Mistral model %q is not available to account %q; refresh that account's model list", model, profile)
}
