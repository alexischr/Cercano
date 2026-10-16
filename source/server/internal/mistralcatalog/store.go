package mistralcatalog

import (
	"context"
	"net/http"
	"sort"
	"sync"
	"time"

	"cercano/source/server/internal/catalog"
)

// Status is credential-free and distinguishes failed discovery from an empty list.
type Status struct {
	Profile, Error string
	RefreshedAt    time.Time
}
type entry struct {
	account Account
	models  []catalog.Model
	status  Status
	next    time.Time
	pending chan struct{}
}

// Store holds one provider catalog with account-specific observations. Replacing
// an entry on credential changes makes any in-flight response obsolete.
type Store struct {
	mu      sync.Mutex
	entries map[string]*entry
	client  *http.Client
	slots   chan struct{}
	ttl     time.Duration
}

func New(client *http.Client) *Store {
	return &Store{entries: map[string]*entry{}, client: client, slots: make(chan struct{}, 4), ttl: 15 * time.Minute}
}

// Sync must be called with the complete configured account set, under the
// configuration owner's serialization. Account values must never be logged.
func (s *Store) Sync(accounts []Account) {
	s.mu.Lock()
	defer s.mu.Unlock()
	keep := map[string]bool{}
	for _, a := range accounts {
		if a.Name == "" {
			continue
		}
		keep[a.Name] = true
		old := s.entries[a.Name]
		if old == nil || old.account != a {
			s.entries[a.Name] = &entry{account: a, status: Status{Profile: a.Name}}
		}
	}
	for name := range s.entries {
		if !keep[name] {
			delete(s.entries, name)
		}
	}
}

// Invalidate removes access immediately, including when a key is removed.
func (s *Store) Invalidate(profile string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.entries, profile)
}

// Refresh invalidates freshness, not account ownership. A failed refresh removes
// that account's available entries instead of asserting stale private access.
func (s *Store) Refresh() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, e := range s.entries {
		e.next = time.Time{}
	}
}

func (s *Store) refresh(ctx context.Context, name string) {
	s.mu.Lock()
	e := s.entries[name]
	if e == nil || time.Now().Before(e.next) {
		s.mu.Unlock()
		return
	}
	if e.pending != nil {
		done := e.pending
		s.mu.Unlock()
		select {
		case <-done:
		case <-ctx.Done():
		}
		return
	}
	e.pending = make(chan struct{})
	done := e.pending
	account := e.account
	s.mu.Unlock()
	select {
	case s.slots <- struct{}{}:
	case <-ctx.Done():
		s.mu.Lock()
		if s.entries[name] == e {
			e.models = nil
			e.status.Error = ctx.Err().Error()
		}
		e.pending = nil
		close(done)
		s.mu.Unlock()
		return
	}
	models, err := Fetch(ctx, s.client, account)
	<-s.slots
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.entries[name] == e {
		e.models = models
		e.status.Error = ""
		e.next = time.Now().Add(s.ttl)
		if err != nil {
			e.models = nil
			e.status.Error = err.Error()
			e.next = time.Now().Add(5 * time.Second)
		} else {
			e.status.RefreshedAt = time.Now().UTC()
		}
	}
	e.pending = nil
	close(done)
}

// List returns independent copies. An empty profile requests the explicit
// combined view; a named profile never exposes another account's model names.
func (s *Store) List(ctx context.Context, profile string) ([]catalog.Model, []Status) {
	s.mu.Lock()
	names := make([]string, 0, len(s.entries))
	for name := range s.entries {
		if profile == "" || profile == name {
			names = append(names, name)
		}
	}
	s.mu.Unlock()
	sort.Strings(names)
	var wg sync.WaitGroup
	for _, name := range names {
		wg.Add(1)
		go func(name string) { defer wg.Done(); s.refresh(ctx, name) }(name)
	}
	wg.Wait()
	s.mu.Lock()
	defer s.mu.Unlock()
	byID := map[string]catalog.Model{}
	statuses := make([]Status, 0, len(names))
	for _, name := range names {
		e := s.entries[name]
		if e == nil {
			continue
		}
		statuses = append(statuses, e.status)
		for _, m := range e.models {
			combined, ok := byID[m.ID]
			if !ok {
				combined = catalog.Model{Source: SourceName, ID: m.ID, Kind: catalog.KindUnknown}
			}
			md := cloneMetadata(m.Metadata)
			combined.Observations = append(combined.Observations, catalog.AccountObservation{Profile: name, BaseURL: e.account.BaseURL, Metadata: md})
			if profile != "" {
				combined.Metadata = cloneMetadata(md)
				combined.Publisher = m.Publisher
				combined.Kind = m.Kind
				combined.ContextLength = md.ContextLength
				combined.SupportsTools = md.Tools == catalog.CapabilitySupported
				combined.SupportsVision = md.Vision == catalog.CapabilitySupported
				combined.Deprecated = md.Deprecated || (!md.RetirementAt.IsZero() && !time.Now().Before(md.RetirementAt))
				combined.ReplacedBy = md.ReplacedBy
			}
			byID[m.ID] = combined
		}
	}
	ids := make([]string, 0, len(byID))
	for id := range byID {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	out := make([]catalog.Model, 0, len(ids))
	for _, id := range ids {
		out = append(out, byID[id])
	}
	if profile != "" && len(statuses) == 0 {
		statuses = append(statuses, Status{Profile: profile, Error: "Mistral account unavailable"})
	}
	return out, statuses
}

func cloneMetadata(m catalog.Metadata) catalog.Metadata {
	m.Aliases = append([]string(nil), m.Aliases...)
	if m.PriceIn != nil {
		v := *m.PriceIn
		m.PriceIn = &v
	}
	if m.PriceOut != nil {
		v := *m.PriceOut
		m.PriceOut = &v
	}
	return m
}

// RefreshProfile expires only the intended account, without touching peers.
func (s *Store) RefreshProfile(profile string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if e := s.entries[profile]; e != nil {
		e.next = time.Time{}
	}
}
