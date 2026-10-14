package broker

import (
	"context"
	"errors"
	"sort"
	"sync"
)

type ActiveTurn struct {
	ConversationID string
	Generation     uint64
}

// PrepareInterruption pauses only registration, not active work, while a host
// records interruption intent. Release restores normal registration on refusal;
// committing InterruptActiveTurns before release makes waiting registrations
// receive the existing rejected/canceled result. Never wait for commands here.
func (b *Broker) PrepareInterruption(ctx context.Context) ([]ActiveTurn, func(), error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if e := ctx.Err(); e != nil {
		return nil, nil, e
	}
	if b.shutdownInterrupted || b.forcePreparing != nil {
		return nil, nil, errors.New("broker interruption already preparing/committed")
	}
	token := make(chan struct{})
	b.forcePreparing = token
	active := make([]ActiveTurn, 0)
	for id, cs := range b.convs {
		if cs.handle != nil {
			active = append(active, ActiveTurn{id, cs.gen})
		}
	}
	sort.Slice(active, func(i, j int) bool { return active[i].ConversationID < active[j].ConversationID })
	var once sync.Once
	release := func() {
		once.Do(func() {
			b.mu.Lock()
			defer b.mu.Unlock()
			if b.forcePreparing == token {
				b.forcePreparing = nil
				close(token)
			}
		})
	}
	return active, release, nil
}
