// Package server — StartChatGPTLogin RPC handler.
//
// Server side of the ChatGPT subscription sign-in. The client opens a stream;
// the agent starts OpenAI's device-authorization flow and immediately sends a
// frame carrying the user_code + verification_url for the client to display,
// then blocks polling for the user's approval. On success it stores the token
// set in the keychain under the profile name, creates/updates a
// responses+chatgpt cloud profile, optionally activates it, and sends a
// terminal frame (done=true, ok=true). Failures send a terminal frame with
// ok=false and the reason. Mirrors the InstallOpenRuntime streaming shape.
package server

import (
	"context"
	"log"
	"strings"

	"cercano/source/server/internal/chatgptauth"
	"cercano/source/server/internal/cloudfactory"
	"cercano/source/server/pkg/config"
	"cercano/source/server/pkg/proto"
)

// StartChatGPTLogin implements proto.AgentServer.
func (s *Server) StartChatGPTLogin(req *proto.StartChatGPTLoginRequest, stream proto.Agent_StartChatGPTLoginServer) error {
	return s.runChatGPTLogin(req, stream, false, chatgptauth.Flow{})
}

func (s *Server) runChatGPTLogin(req *proto.StartChatGPTLoginRequest, stream proto.Agent_StartChatGPTLoginServer, reauthenticate bool, flow chatgptauth.Flow) error {
	ctx := stream.Context()
	st := s.cfgSvc.Secrets()
	if st == nil {
		return sendChatGPTLoginResult(stream, false, "", "", "keychain unavailable")
	}
	profile := strings.TrimSpace(req.GetProfileName())
	if profile == "" {
		profile = "chatgpt"
	}
	model := strings.TrimSpace(req.GetModel())
	if model == "" {
		model = config.Defaults().ModelProfiles.ResolveCloudModelForTier(config.CloudProfile{
			Flavor: cloudfactory.FlavorResponses,
			Route:  cloudfactory.RouteChatGPT,
		}, config.TierEveryday)
	}

	np := config.CloudProfile{Name: profile, Flavor: cloudfactory.FlavorResponses, Route: cloudfactory.RouteChatGPT, Model: model, ModelPinned: strings.TrimSpace(req.GetModel()) != ""}
	login, err := s.cfgSvc.BeginCloudLogin(ctx, np, req.GetSetActive(), reauthenticate, req.GetCreateOnly())
	if err != nil {
		return sendChatGPTLoginResult(stream, false, profile, "", loginStageFailure("ChatGPT", "profile setup", err))
	}
	defer login.Close()
	ctx = login.Context()

	// Start the device authorization and show the user the code + URL.
	pending, err := flow.Start(ctx)
	if err != nil {
		return sendChatGPTLoginResult(stream, false, "", "", loginStageFailure("ChatGPT", "authorization start", err))
	}
	if err := stream.Send(&proto.StartChatGPTLoginEvent{
		VerificationUrl: pending.VerificationURL,
		UserCode:        pending.UserCode,
	}); err != nil {
		return err
	}

	// Block until the user approves in their browser (or ctx cancels).
	ts, err := pending.Poll(ctx)
	if err != nil {
		return sendChatGPTLoginResult(stream, false, "", "", loginStageFailure("ChatGPT", "authorization completion", err))
	}

	encoded, err := ts.Encode()
	if err != nil {
		return sendChatGPTLoginResult(stream, false, profile, "", loginStageFailure("ChatGPT", "credential encoding", err))
	}
	result, err := login.Commit(encoded, ts.Identity)
	if err != nil {
		return sendChatGPTLoginResult(stream, false, profile, "", loginStageFailure("ChatGPT", "credential saving", err))
	}
	if result.Reauthenticated {
		// Credentials may be restored after reset or a failed startup build.
		// Rebind referenced accounts without changing their routing settings.
		if result.Active && s.providerSvc != nil && s.providerSvc.Cloud() == nil {
			if err := s.rebuildCloud(); err != nil {
				s.persistConfig()
				return sendChatGPTLoginResult(stream, false, profile, ts.AccountID, loginStageFailure("ChatGPT", "provider activation", err))
			}
		}
		s.persistConfig()
		return sendChatGPTLoginResult(stream, true, profile, ts.AccountID, "")
	}
	isActive := result.Active
	np = result.Profile
	if strings.TrimSpace(req.GetModel()) == "" {
		// The login attempt's context ends at Commit; the RPC stream's does not.
		np = s.defaultChatGPTTiersToLatest(stream.Context(), np, ts.Access, ts.AccountID)
	}

	if isActive {
		if err := s.rebuildCloud(); err != nil {
			s.persistConfig()
			return sendChatGPTLoginResult(stream, false, profile, ts.AccountID, loginStageFailure("ChatGPT", "provider activation", err))
		}
		s.broadcastConfigChanged("cloud_model", np.Model)
	}
	s.persistConfig()
	return sendChatGPTLoginResult(stream, true, profile, ts.AccountID, "")
}

// defaultChatGPTTiersToLatest points every cost tier the profile has not
// explicitly overridden at the newest model the account's codex catalog
// offers (its highest-priority picker entry), so a fresh sign-in starts on
// what the subscription actually serves rather than a baked default that can
// lag the account. Best effort: if the catalog is unreachable the baked
// vendor default stays in effect and the sign-in still succeeds.
func (s *Server) defaultChatGPTTiersToLatest(ctx context.Context, p config.CloudProfile, access, accountID string) config.CloudProfile {
	models, err := s.fetchChatGPTCatalog(ctx, access, accountID)
	if err != nil || len(models) == 0 {
		log.Printf("[chatgpt] keeping default models for %s: catalog unavailable: %v", p.Name, err)
		return p
	}
	latest := models[0].Slug
	updated := p
	err = s.cfgSvc.Mutate(func(c *config.Config) {
		for i := range c.CloudProfiles {
			prof := &c.CloudProfiles[i]
			if prof.Name != p.Name {
				continue
			}
			if prof.TierOverrides == nil {
				prof.TierOverrides = map[config.CostTier]string{}
			}
			for _, tier := range []config.CostTier{config.CostEconomy, config.CostStandard, config.CostPremium} {
				if prof.TierOverrides[tier] == "" {
					prof.TierOverrides[tier] = latest
				}
			}
			updated = prof.Clone()
		}
	})
	if err != nil {
		log.Printf("[chatgpt] keeping default models for %s: %v", p.Name, err)
		return p
	}
	return updated
}

// sendChatGPTLoginResult emits the terminal frame of the sign-in stream.
func sendChatGPTLoginResult(stream proto.Agent_StartChatGPTLoginServer, ok bool, profile, accountID, errMsg string) error {
	return stream.Send(&proto.StartChatGPTLoginEvent{
		Done:        true,
		Ok:          ok,
		ProfileName: profile,
		AccountId:   accountID,
		Error:       errMsg,
	})
}
