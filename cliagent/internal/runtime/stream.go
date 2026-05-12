package runtime

import (
	"context"
	"maps"
	"sync"
	"sync/atomic"
)

// DefaultStreamBuffer is the default ring-buffer capacity for a Stream.
const DefaultStreamBuffer = 64

// Stream is the live handle for a single run. The cliagent launcher (process
// manager) creates a Stream via Session.AcquireStream, emits events into it
// via Emit, and consumers pull from it with Next/Event. Closing is symmetric:
// either side can call Close.
//
// Stream does NOT write back to Session history/state on its own — the
// launcher is expected to update Session directly when it emits relevant
// events. User-registered observers (via AddObserver) see every Event in
// emission order, sync by default or async via ObserverEntry.Async.
type Stream struct {
	ring *ringBuffer
	cur  Event

	observersMu sync.RWMutex
	observers   []ObserverEntry
	asyncCh     map[int]chan Event // index in observers → buffered chan for async observers
	asyncWG     sync.WaitGroup

	stateMu sync.RWMutex
	sessID  string
	state   State

	// injectFn is invoked by Steer/FollowUp. The cliagent launcher sets it.
	// kind is "user" for Steer, "follow_up" for FollowUp.
	injectFn func(ctx context.Context, kind string, payload string) error

	// permissionFn is invoked by RespondPermission. The headless launcher sets it
	// when --permission-prompt-tool stdio is active.
	permissionFn func(ctx context.Context, requestID string, allow bool) error

	// closeFn runs once on Close, before the ring is closed.
	closeFn func(ctx context.Context) error

	closeOnce sync.Once
	closed    chan struct{}
	// closing is set to true at the very start of Close, before any async
	// observer channels are closed. Emit checks it and bails out so it can't
	// race with `close(ch)` and panic with "send on closed channel". Mirrors
	// the legacy dispatcher pattern at agent/dispatcher.go:150.
	closing atomic.Bool

	resultMu  sync.Mutex
	result    *Result
	resultErr error
}

// NewStream creates a Stream with the given ring-buffer size. Pass 0 for the default.
func NewStream(buf int) *Stream {
	if buf <= 0 {
		buf = DefaultStreamBuffer
	}
	return &Stream{
		ring:    newRingBuffer(buf),
		asyncCh: make(map[int]chan Event),
		closed:  make(chan struct{}),
	}
}

// AddObserver registers a user observer. Sync observers run inline on Emit;
// async observers run in their own goroutine fed by a buffered channel.
// Returns the registration index (currently unused by callers).
func (s *Stream) AddObserver(o ObserverEntry) int {
	s.observersMu.Lock()
	defer s.observersMu.Unlock()
	idx := len(s.observers)
	s.observers = append(s.observers, o)
	if o.Async {
		bufSz := o.BufferSz
		if bufSz <= 0 {
			bufSz = 16
		}
		ch := make(chan Event, bufSz)
		s.asyncCh[idx] = ch
		s.asyncWG.Add(1)
		go func(fn Observer, ch chan Event) {
			defer s.asyncWG.Done()
			for ev := range ch {
				func() {
					defer func() { _ = recover() }()
					fn(context.Background(), ev)
				}()
			}
		}(o.Fn, ch)
	}
	return idx
}

// Emit pushes an event into the stream: invokes user observers (sync inline,
// async via channel send), then enqueues into the ring buffer.
//
// Sync observer panics are recovered and silently swallowed; async observer
// panics are recovered inside the consumer goroutine.
//
// If Close has begun (closing flag set), Emit becomes a no-op so it cannot
// race with close(ch) on async observer channels and panic with "send on
// closed channel". The full async-send loop holds observersMu for read so
// Close (which takes the write lock before closing channels) is fully
// serialized against in-flight Emits.
func (s *Stream) Emit(ev Event) {
	if s.closing.Load() {
		return
	}
	s.observersMu.RLock()
	obs := append([]ObserverEntry(nil), s.observers...)
	chans := make(map[int]chan Event, len(s.asyncCh))
	maps.Copy(chans, s.asyncCh)

	for i, o := range obs {
		if o.Async {
			ch := chans[i]
			if ch == nil {
				continue
			}
			select {
			case ch <- ev:
			default:
				// observer can't keep up; drop on the floor — caller is responsible
				// for sizing buffers if they care about completeness.
			}
		} else {
			func() {
				defer func() { _ = recover() }()
				o.Fn(context.Background(), ev)
			}()
		}
	}
	s.observersMu.RUnlock()

	// Update SessionID / State on the relevant kinds before pushing.
	if ev.SessionID != "" {
		s.stateMu.Lock()
		if s.sessID == "" {
			s.sessID = ev.SessionID
		}
		s.state.ThreadID = ev.SessionID
		s.stateMu.Unlock()
	}

	s.ring.Push(ev)
}

// Next blocks until the next event is available, returns false when the stream
// has been closed and drained.
func (s *Stream) Next() bool {
	ev, ok := s.ring.Pop(context.Background())
	if !ok {
		return false
	}
	s.cur = ev
	return true
}

// Event returns the current event (must be called after Next returns true).
func (s *Stream) Event() Event { return s.cur }

// SessionID returns the session id captured from the first event with one,
// or "" if none has been seen yet.
func (s *Stream) SessionID() string {
	s.stateMu.RLock()
	defer s.stateMu.RUnlock()
	return s.sessID
}

// State returns a clone of the current observed state.
func (s *Stream) State() State {
	s.stateMu.RLock()
	defer s.stateMu.RUnlock()
	return State{ThreadID: s.state.ThreadID, Values: CloneValues(s.state.Values)}
}

// SetState lets the launcher update the stream's ThreadID / Values snapshot
// before emitting events.
func (s *Stream) SetState(thread string, values map[string]any) {
	s.stateMu.Lock()
	defer s.stateMu.Unlock()
	if thread != "" {
		s.state.ThreadID = thread
		if s.sessID == "" {
			s.sessID = thread
		}
	}
	if values != nil {
		s.state.Values = CloneValues(values)
	}
}

// Steer injects a "user" message into the in-progress run.
func (s *Stream) Steer(ctx context.Context, text string) error {
	if s.injectFn == nil {
		return ErrSteeringUnavailable
	}
	return s.injectFn(ctx, "user", text)
}

// FollowUp queues the next-turn user message. Returns ErrFollowUpUnavailable if the launcher didn't wire one.
func (s *Stream) FollowUp(ctx context.Context, text string) error {
	if s.injectFn == nil {
		return ErrFollowUpUnavailable
	}
	return s.injectFn(ctx, "follow_up", text)
}

// SetInjectFn lets the cliagent launcher provide the steer/follow_up bridge.
func (s *Stream) SetInjectFn(fn func(ctx context.Context, kind, payload string) error) {
	s.injectFn = fn
}

// SetPermissionFn lets the cliagent launcher provide the permission response bridge.
func (s *Stream) SetPermissionFn(fn func(ctx context.Context, requestID string, allow bool) error) {
	s.permissionFn = fn
}

// RespondPermission sends a permission decision back to the CLI process.
// Returns ErrPermissionResponse if no permission handler is wired.
func (s *Stream) RespondPermission(ctx context.Context, requestID string, allow bool) error {
	if s.permissionFn == nil {
		return ErrPermissionResponse
	}
	return s.permissionFn(ctx, requestID, allow)
}

// SetCloseFn lets the cliagent launcher provide a teardown hook.
func (s *Stream) SetCloseFn(fn func(ctx context.Context) error) {
	s.closeFn = fn
}

// SetResult is called by the launcher to publish the aggregated Result.
// (Typically emitted alongside or just before EventDone.)
func (s *Stream) SetResult(r *Result, err error) {
	s.resultMu.Lock()
	s.result = r
	s.resultErr = err
	s.resultMu.Unlock()
}

// Result returns the aggregated result. If unset (e.g. closed before EventDone),
// returns (nil, nil).
func (s *Stream) Result() (*Result, error) {
	s.resultMu.Lock()
	defer s.resultMu.Unlock()
	return s.result, s.resultErr
}

// Close drains the ring buffer, invokes closeFn, and waits for async observers.
// Idempotent.
func (s *Stream) Close(ctx context.Context) error {
	var firstErr error
	s.closeOnce.Do(func() {
		// Flip the closing flag first so any concurrent Emit bails out before
		// touching async channels. Pair this with the observersMu.Lock below:
		// in-flight Emits hold observersMu for read across their async-send
		// loop, so taking the write lock here waits for them to finish before
		// we close any channel.
		s.closing.Store(true)
		if s.closeFn != nil {
			if err := s.closeFn(ctx); err != nil {
				firstErr = err
			}
		}
		s.ring.Close()

		// Close async observer channels and wait for their consumers to drain.
		s.observersMu.Lock()
		for _, ch := range s.asyncCh {
			close(ch)
		}
		s.asyncCh = nil
		s.observersMu.Unlock()
		s.asyncWG.Wait()

		close(s.closed)
	})
	return firstErr
}

// Closed returns a channel that is closed when the stream's Close has finished.
func (s *Stream) Closed() <-chan struct{} { return s.closed }

// DroppedCount returns how many events were dropped because the ring was full.
func (s *Stream) DroppedCount() int64 { return s.ring.DroppedCount() }
