package agent

import (
	"context"
	"sync"
)

// dispatcher is the per-Runner event fan-out hub.
type dispatcher struct {
	runner      *Runner
	mu          sync.Mutex
	runnerSubs  []dispatcherEntry
	ring        []Event
	ringHead    int
	ringSize    int
	ringFull    bool
	ringDropped int64
}

type dispatcherEntry struct {
	filter EventFilter
	fn     func(context.Context, Event)
}

func newDispatcher(r *Runner) *dispatcher {
	return &dispatcher{runner: r, ringSize: 64, ring: make([]Event, 64)}
}

func (d *dispatcher) addRunnerObserver(filter EventFilter, fn func(context.Context, Event)) Unsubscribe {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.runnerSubs = append(d.runnerSubs, dispatcherEntry{filter, fn})
	idx := len(d.runnerSubs) - 1
	return func() {
		d.mu.Lock()
		defer d.mu.Unlock()
		if idx < len(d.runnerSubs) {
			d.runnerSubs = append(d.runnerSubs[:idx], d.runnerSubs[idx+1:]...)
		}
	}
}

// emit is called from the loop goroutine. It:
// 1. writes back to Conversation (caller's responsibility before calling emit)
// 2. fires Agent.OnEvent observers (registered at Agent construction)
// 3. fires Runner.OnEvent observers (registered after Runner construction)
// 4. pushes Event into the ring buffer for iter.Seq consumers
func (d *dispatcher) emit(ctx context.Context, ev Event) {
	// Step 2: Agent observers (snapshot first to allow concurrent re-registration safely).
	for _, e := range d.runner.agent.cfg.onEventEntries {
		if e.filter.Match(ev) {
			safeOnEvent(ctx, e.fn, ev)
		}
	}
	// Step 3: Runner observers.
	d.mu.Lock()
	runnerSubs := append([]dispatcherEntry(nil), d.runnerSubs...)
	d.mu.Unlock()
	for _, e := range runnerSubs {
		if e.filter.Match(ev) {
			safeOnEvent(ctx, e.fn, ev)
		}
	}
	// Step 4: ring buffer (best effort, drops oldest if full).
	d.mu.Lock()
	if d.ringFull {
		d.ringDropped++
	}
	d.ring[d.ringHead] = ev
	d.ringHead = (d.ringHead + 1) % d.ringSize
	if d.ringHead == 0 {
		d.ringFull = true
	}
	d.mu.Unlock()
}

func safeOnEvent(ctx context.Context, fn func(context.Context, Event), ev Event) {
	defer func() { _ = recover() }()
	fn(ctx, ev)
}

// Unsubscribe is a function that unregisters an observer when called.
type Unsubscribe func()
