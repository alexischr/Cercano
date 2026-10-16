// Package mistralcatalog discovers models available to configured Mistral accounts.
// A shared list records access; metadata remains scoped to the observing account.
package mistralcatalog

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"cercano/source/server/internal/catalog"
)

const SourceName = "mistral"
const BaseURL = "https://api.mistral.ai/v1"
const maxResponseBytes = 8 << 20

// Account is internal only: never serialize or log it.
type Account struct{ Name, BaseURL, Key string }

type wireModel struct {
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	Publisher    string   `json:"owned_by"`
	Context      int      `json:"max_context_length"`
	Aliases      []string `json:"aliases"`
	Deprecation  string   `json:"deprecation"`
	Replacement  string   `json:"deprecation_replacement_model"`
	Capabilities struct {
		Chat      *bool `json:"completion_chat"`
		Tools     *bool `json:"function_calling"`
		Vision    *bool `json:"vision"`
		Reasoning *bool `json:"reasoning"`
	} `json:"capabilities"`
}

func capability(v *bool) catalog.Capability {
	if v == nil {
		return catalog.CapabilityUnknown
	}
	if *v {
		return catalog.CapabilitySupported
	}
	return catalog.CapabilityUnsupported
}

// Fetch uses a copied client so callers cannot accidentally allow bearer-token
// redirects. Error text deliberately excludes URLs, bodies and transport errors.
func Fetch(ctx context.Context, client *http.Client, account Account) ([]catalog.Model, error) {
	if strings.TrimSpace(account.Key) == "" {
		return nil, errors.New("Mistral account has no API key")
	}
	u, err := url.Parse(account.BaseURL)
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Scheme != "https" && u.Scheme != "http") {
		return nil, errors.New("invalid Mistral endpoint")
	}
	// HTTP is reserved for local test/development endpoints.
	if u.Scheme == "http" && u.Hostname() != "127.0.0.1" && u.Hostname() != "localhost" && u.Hostname() != "::1" {
		return nil, errors.New("Mistral endpoint requires HTTPS")
	}
	u.Path = strings.TrimRight(u.Path, "/") + "/models"
	u.RawPath = ""
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, errors.New("invalid Mistral request")
	}
	req.Header.Set("Authorization", "Bearer "+account.Key)
	req.Header.Set("Accept", "application/json")
	c := http.Client{}
	if client != nil {
		c = *client
	}
	c.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	resp, err := c.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, errors.New("Mistral catalog request failed")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Mistral catalog HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
	if err != nil {
		return nil, errors.New("Mistral catalog response read failed")
	}
	if len(body) > maxResponseBytes {
		return nil, errors.New("Mistral catalog response too large")
	}
	var response struct {
		Data json.RawMessage `json:"data"`
	}
	if json.Unmarshal(body, &response) != nil || len(response.Data) == 0 || string(response.Data) == "null" {
		return nil, errors.New("invalid Mistral catalog response")
	}
	var wire []wireModel
	if json.Unmarshal(response.Data, &wire) != nil || len(wire) > 10000 {
		return nil, errors.New("invalid Mistral model list")
	}
	out := make([]catalog.Model, 0, len(wire))
	seen := map[string]bool{}
	for _, w := range wire {
		if strings.TrimSpace(w.ID) == "" || seen[w.ID] {
			return nil, errors.New("Mistral model list has missing or duplicate IDs")
		}
		seen[w.ID] = true
		metadata := catalog.Metadata{DisplayName: w.Name, ContextLength: w.Context, Aliases: w.Aliases, ReplacedBy: w.Replacement, Chat: capability(w.Capabilities.Chat), Tools: capability(w.Capabilities.Tools), Vision: capability(w.Capabilities.Vision), Reasoning: capability(w.Capabilities.Reasoning)}
		if w.Context < 0 {
			return nil, errors.New("invalid Mistral context length")
		}
		if w.Deprecation != "" {
			metadata.RetirementAt, err = time.Parse("2006-01-02", w.Deprecation)
			if err != nil {
				return nil, errors.New("invalid Mistral retirement date")
			}
		}
		kind := catalog.KindUnknown
		if metadata.Chat == catalog.CapabilitySupported {
			kind = catalog.KindTextGeneration
		}
		out = append(out, catalog.Model{Source: SourceName, ID: w.ID, Publisher: w.Publisher, Kind: kind, Metadata: metadata})
	}
	return out, nil
}
