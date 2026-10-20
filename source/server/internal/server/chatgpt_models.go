package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"cercano/source/server/internal/chatgptauth"
	"cercano/source/server/internal/llm/responses"
	"cercano/source/server/pkg/proto"
)

// codexCatalogClientVersion is sent as client_version on the codex backend's
// /models request. The backend scopes its catalog by Codex CLI release, so
// this tracks a recent openai/codex release rather than Cercano's own version.
const codexCatalogClientVersion = "0.163.0"

// codexModel is the subset of the codex backend's ModelInfo
// (codex-rs/protocol/src/openai_models.rs) the settings picker uses.
type codexModel struct {
	Slug        string `json:"slug"`
	DisplayName string `json:"display_name"`
	Visibility  string `json:"visibility"` // "list" | "hide" | "none"
	Priority    int    `json:"priority"`   // lower sorts first
}

// listChatGPTModels serves the settings picker the account-scoped catalog for
// a signed-in ChatGPT profile, using its stored (auto-refreshing) bearer.
func (s *Server) listChatGPTModels(ctx context.Context, profile string) *proto.ListCloudProfileModelsResponse {
	access, accountID, err := s.cfgSvc.Credentials().ChatGPT(profile, chatgptauth.Flow{}).Token(ctx)
	if err != nil {
		return &proto.ListCloudProfileModelsResponse{Error: err.Error()}
	}
	models, err := s.fetchChatGPTCatalog(ctx, access, accountID)
	if err != nil {
		return &proto.ListCloudProfileModelsResponse{Error: err.Error()}
	}
	out := &proto.ListCloudProfileModelsResponse{}
	for _, m := range models {
		name := m.DisplayName
		if name == "" {
			name = m.Slug
		}
		out.Models = append(out.Models, &proto.CloudModelInfo{Id: m.Slug, DisplayName: name})
	}
	return out
}

// fetchChatGPTCatalog fetches GET <codex base>/models?client_version=… with a
// subscription bearer and ChatGPT-Account-Id — the account-scoped catalog
// Codex itself renders — and returns the picker-visible entries, newest
// (highest priority) first. Bounded like the other catalog fetches (6s
// timeout, 16 MiB body cap), and redirects are refused so the bearer can
// never follow one off-host.
func (s *Server) fetchChatGPTCatalog(ctx context.Context, access, accountID string) ([]codexModel, error) {
	base := s.chatgptCatalogBase
	if base == "" {
		base = responses.CodexBaseURL
	}
	modelsURL := strings.TrimRight(base, "/") + "/models?client_version=" + url.QueryEscape(codexCatalogClientVersion)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, modelsURL, nil)
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Authorization", "Bearer "+access)
	if accountID != "" {
		httpReq.Header.Set("ChatGPT-Account-Id", accountID)
	}
	httpReq.Header.Set("originator", "cercano")
	httpReq.Header.Set("User-Agent", "cercano")
	client := &http.Client{
		Timeout: 6 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return fmt.Errorf("codex /models refused redirect (bearer token must not follow off-host)")
		},
	}
	resp, err := client.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("ChatGPT model catalog returned %d", resp.StatusCode)
	}
	var parsed struct {
		Models []codexModel `json:"models"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, fmt.Errorf("parse: %w", err)
	}
	return chatgptPickerModels(parsed.Models), nil
}

// chatgptPickerModels mirrors Codex's own picker: only entries the backend
// marks visibility "list", ordered by priority with the backend's order kept
// for ties. Hidden or unknown-visibility entries are dropped; a pinned model
// the catalog omits is still shown by the client as "not in current catalog".
func chatgptPickerModels(models []codexModel) []codexModel {
	out := make([]codexModel, 0, len(models))
	for _, m := range models {
		if m.Visibility == "list" && m.Slug != "" {
			out = append(out, m)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Priority < out[j].Priority })
	return out
}
