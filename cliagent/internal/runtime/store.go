package runtime

import (
	"context"
	"sync"
)

// SessionData is the persisted snapshot of a session.
type SessionData struct {
	Messages []Message
	State    State
}

// Store persists session data. Save must deep-copy; the caller may keep references.
type Store interface {
	Save(ctx context.Context, id string, data SessionData) error
	// Load returns (data, true, nil) on hit, (zero, false, nil) on miss, or (zero, false, err) on real error.
	Load(ctx context.Context, id string) (SessionData, bool, error)
	Delete(ctx context.Context, id string) error
}

// MemoryStore is the in-memory Store implementation. Suitable for tests
// and short-lived sessions.
type MemoryStore struct {
	mu   sync.RWMutex
	data map[string]SessionData
}

// NewMemoryStore creates an empty MemoryStore.
func NewMemoryStore() *MemoryStore { return &MemoryStore{data: make(map[string]SessionData)} }

// Save deep-copies messages + state.Values before storing.
func (s *MemoryStore) Save(_ context.Context, id string, data SessionData) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data[id] = SessionData{
		Messages: append([]Message(nil), data.Messages...),
		State:    State{ThreadID: data.State.ThreadID, Values: CloneValues(data.State.Values)},
	}
	return nil
}

// Load returns (data, true, nil) on hit, (zero, false, nil) on miss.
func (s *MemoryStore) Load(_ context.Context, id string) (SessionData, bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	d, ok := s.data[id]
	if !ok {
		return SessionData{}, false, nil
	}
	return SessionData{
		Messages: append([]Message(nil), d.Messages...),
		State:    State{ThreadID: d.State.ThreadID, Values: CloneValues(d.State.Values)},
	}, true, nil
}

// Delete removes the entry. Missing id is not an error.
func (s *MemoryStore) Delete(_ context.Context, id string) error {
	s.mu.Lock()
	delete(s.data, id)
	s.mu.Unlock()
	return nil
}
