package runtime

import (
	"context"
	"sync"
)

// Session holds the persistent state of a multi-turn conversation. The cliagent
// launcher acquires a Stream from a Session for each turn and releases it
// when the turn ends.
//
// One active Stream at a time. Concurrent AcquireStream calls return ErrSessionBusy.
type Session struct {
	id    string
	store Store

	mu      sync.Mutex
	history []Message
	state   State
	closed  bool
	queue   []string // follow-up prompts; FIFO consumed by the launcher
	active  *Stream  // nil when no turn is running
}

// NewSession creates a Session with the given id and Store. If store is nil,
// a fresh in-memory store is used.
func NewSession(id string, store Store) *Session {
	if store == nil {
		store = NewMemoryStore()
	}
	return &Session{id: id, store: store}
}

// ID returns the session identifier.
func (s *Session) ID() string { return s.id }

// History returns a deep-cloned snapshot of the persisted message history.
func (s *Session) History() []Message {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Message(nil), s.history...)
}

// State returns a clone of the current state.
func (s *Session) State() State {
	s.mu.Lock()
	defer s.mu.Unlock()
	return State{ThreadID: s.state.ThreadID, Values: CloneValues(s.state.Values)}
}

// AppendHistory appends a message to the persisted history and triggers a Save.
// Called by the cliagent launcher when an emit-worthy event finalizes.
func (s *Session) AppendHistory(m Message) {
	s.mu.Lock()
	s.history = append(s.history, m)
	s.persistLocked()
	s.mu.Unlock()
}

// UpdateState applies fn to the live state under the session lock and
// re-persists. fn may mutate state directly; it must not call Session methods
// from within (deadlock).
func (s *Session) UpdateState(fn func(*State)) {
	s.mu.Lock()
	fn(&s.state)
	s.persistLocked()
	s.mu.Unlock()
}

// Clear drops history + state + follow-up queue and removes the persisted snapshot.
func (s *Session) Clear(ctx context.Context) error {
	s.mu.Lock()
	s.history = nil
	s.state = State{}
	s.queue = nil
	s.mu.Unlock()
	return s.store.Delete(ctx, s.id)
}

// Close marks the session closed. Idempotent. The active stream (if any) is not
// forcibly closed — caller is responsible for tearing down the run first.
func (s *Session) Close(_ context.Context) error {
	s.mu.Lock()
	s.closed = true
	s.mu.Unlock()
	return nil
}

// AcquireStream returns a fresh Stream for one turn. Returns ErrSessionBusy if
// a turn is already running. The returned Stream is also stored as the active
// stream and exposed by ActiveStream.
func (s *Session) AcquireStream(buf int) (*Stream, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil, ErrStreamClosed
	}
	if s.active != nil {
		return nil, ErrSessionBusy
	}
	st := NewStream(buf)
	if s.state.ThreadID != "" {
		st.SetState(s.state.ThreadID, s.state.Values)
	}
	s.active = st
	return st, nil
}

// ReleaseStream clears the active stream pointer. Called by the launcher when
// the turn has ended (regardless of stop reason).
func (s *Session) ReleaseStream(stream *Stream) {
	s.mu.Lock()
	if s.active == stream {
		s.active = nil
	}
	s.mu.Unlock()
}

// ActiveStream returns the currently active stream, or nil.
func (s *Session) ActiveStream() *Stream {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.active
}

// EnqueueFollowUp appends to the follow-up queue. Returns ErrFollowUpUnavailable
// if the session is closed.
func (s *Session) EnqueueFollowUp(text string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return ErrFollowUpUnavailable
	}
	s.queue = append(s.queue, text)
	return nil
}

// DequeueFollowUp pops the next queued follow-up. Returns ("", false) if empty.
func (s *Session) DequeueFollowUp() (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.queue) == 0 {
		return "", false
	}
	next := s.queue[0]
	s.queue = s.queue[1:]
	return next, true
}

// LoadFromStore re-hydrates history + state from the underlying store.
// No-op if the id is missing.
func (s *Session) LoadFromStore(ctx context.Context) error {
	data, ok, err := s.store.Load(ctx, s.id)
	if err != nil {
		return err
	}
	if !ok {
		return nil
	}
	s.mu.Lock()
	s.history = append([]Message(nil), data.Messages...)
	s.state = State{ThreadID: data.State.ThreadID, Values: CloneValues(data.State.Values)}
	s.mu.Unlock()
	return nil
}

func (s *Session) persistLocked() {
	// best-effort, no error handling beyond Store's contract; the in-memory
	// default never errors. A real Store can log via observer wiring later.
	//
	// Note: this calls store.Save while still holding s.mu. That's intentional
	// for the in-memory default (cheap, never blocks). Any real Store
	// implementation must either (a) accept being invoked under the session
	// lock or (b) queue/dispatch the Save asynchronously to avoid stalling
	// other Session methods that share s.mu.
	_ = s.store.Save(context.Background(), s.id, SessionData{
		Messages: append([]Message(nil), s.history...),
		State:    State{ThreadID: s.state.ThreadID, Values: CloneValues(s.state.Values)},
	})
}
