package agent

import (
	"iter"
	"sync"
)

type watchSubscription struct {
	ch     chan Change
	closed bool
	mu     sync.Mutex
}

func (s *watchSubscription) push(ch Change) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return
	}
	select {
	case s.ch <- ch:
	default:
		// best-effort fan-out; if a slow subscriber blocks, drop.
		// (Subscribers receive in order; if they fall behind they may miss
		// events. This trade-off is documented in the spec §4.2.)
	}
}

func (s *watchSubscription) close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.closed {
		s.closed = true
		close(s.ch)
	}
}

// Watch returns an iter.Seq[Change] that yields Conversation changes
// in monotonic Version order. Each call returns an independent subscription;
// `break`-ing out of range removes the subscription.
func (c *Conversation) Watch() iter.Seq[Change] {
	sub := &watchSubscription{ch: make(chan Change, 64)}

	c.watchMu.Lock()
	c.watchSubs = append(c.watchSubs, sub)
	c.watchMu.Unlock()

	return func(yield func(Change) bool) {
		defer c.removeSub(sub)
		for ch := range sub.ch {
			if !yield(ch) {
				return
			}
		}
	}
}

func (c *Conversation) removeSub(sub *watchSubscription) {
	c.watchMu.Lock()
	for i, s := range c.watchSubs {
		if s == sub {
			c.watchSubs = append(c.watchSubs[:i], c.watchSubs[i+1:]...)
			break
		}
	}
	c.watchMu.Unlock()
	sub.close()
}

// broadcast is called by Conversation mutations under the convention that
// the caller has already mutated state under c.mu.
func (c *Conversation) broadcast(change Change) {
	c.watchMu.Lock()
	c.version++
	change.Version = c.version
	subs := append([]*watchSubscription(nil), c.watchSubs...)
	c.watchMu.Unlock()

	for _, sub := range subs {
		sub.push(change)
	}
}
