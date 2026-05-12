package approve

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"iter"
	"sync"
	"time"
)

// ErrUnknownID is returned by Approve / Deny when the given ID is not in the queue.
var ErrUnknownID = errors.New("approve: unknown pending id")

// ErrClosed is returned by Approve / Deny / Hook calls after Close.
var ErrClosed = errors.New("approve: approver closed")

// Pending is a single tool-use awaiting decision.
type Pending struct {
	ID          string
	ToolName    string
	ToolUseID   string
	Input       map[string]any
	SubmittedAt time.Time
}

// Approver is a synchronous decision queue. It is safe for concurrent use.
type Approver struct {
	mu      sync.Mutex
	pending map[string]*entry
	queueCh chan Pending
	doneCh  chan struct{} // closed once by Close(); never written to
	closed  bool
}

type entry struct {
	p     Pending
	resCh chan decision
}

type decision struct {
	approve bool
	reason  string
}

// New returns a fresh Approver.
func New() *Approver {
	return &Approver{
		pending: make(map[string]*entry),
		queueCh: make(chan Pending, 16),
		doneCh:  make(chan struct{}),
	}
}

// submit enqueues a Pending and blocks the caller's goroutine on the resCh.
func (a *Approver) submit(p Pending) (decision, error) {
	a.mu.Lock()
	if a.closed {
		a.mu.Unlock()
		return decision{}, ErrClosed
	}
	e := &entry{p: p, resCh: make(chan decision, 1)}
	a.pending[p.ID] = e
	a.mu.Unlock()

	// Send to queueCh or bail if Close() fires first.
	select {
	case a.queueCh <- p:
	case <-a.doneCh:
		// Close() already denied e.resCh; drain and return.
		select {
		case d := <-e.resCh:
			return d, nil
		default:
		}
		return decision{approve: false, reason: "approver closed"}, nil
	}

	// Wait for the decision or Close().
	select {
	case d := <-e.resCh:
		return d, nil
	case <-a.doneCh:
		// Close() will have sent to e.resCh before closing doneCh,
		// so drain resCh first to avoid missing the decision.
		select {
		case d := <-e.resCh:
			return d, nil
		default:
			return decision{approve: false, reason: "approver closed"}, nil
		}
	}
}

// Pending returns an iter.Seq[Pending] of all incoming pending items.
// The iterator terminates when Close() is called.
func (a *Approver) Pending() iter.Seq[Pending] {
	return func(yield func(Pending) bool) {
		for {
			select {
			case p, ok := <-a.queueCh:
				if !ok {
					return
				}
				if !yield(p) {
					return
				}
			case <-a.doneCh:
				// Drain any items already buffered before stopping.
				for {
					select {
					case p := <-a.queueCh:
						if !yield(p) {
							return
						}
					default:
						return
					}
				}
			}
		}
	}
}

// Approve resolves the Pending with the given ID as Approved.
func (a *Approver) Approve(id string) error {
	return a.resolve(id, decision{approve: true})
}

// Deny resolves the Pending with the given ID as Denied with the given reason.
func (a *Approver) Deny(id string, reason string) error {
	return a.resolve(id, decision{approve: false, reason: reason})
}

func (a *Approver) resolve(id string, d decision) error {
	a.mu.Lock()
	if a.closed {
		a.mu.Unlock()
		return ErrClosed
	}
	e, ok := a.pending[id]
	if !ok {
		a.mu.Unlock()
		return ErrUnknownID
	}
	delete(a.pending, id)
	a.mu.Unlock()

	e.resCh <- d
	return nil
}

// Close marks the Approver closed and denies all pending entries with reason
// "approver closed". The Pending iter terminates.
func (a *Approver) Close() error {
	a.mu.Lock()
	if a.closed {
		a.mu.Unlock()
		return nil
	}
	a.closed = true
	for _, e := range a.pending {
		e.resCh <- decision{approve: false, reason: "approver closed"}
	}
	a.pending = nil
	close(a.doneCh)
	a.mu.Unlock()
	return nil
}

// newID returns a short random opaque id.
func newID() string {
	var buf [8]byte
	_, _ = rand.Read(buf[:])
	return "pa-" + hex.EncodeToString(buf[:])
}
