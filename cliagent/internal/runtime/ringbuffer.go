package runtime

import (
	"context"
	"sync"
	"sync/atomic"
)

// ringBuffer is a fixed-capacity FIFO that drops the oldest on overflow.
// Push is non-blocking; Pop blocks until data, ctx cancel, or close.
// (Ported from agent/dispatcher.go.)
type ringBuffer struct {
	mu     sync.Mutex
	cond   *sync.Cond
	buf    []Event
	head   int
	size   int
	cap    int
	closed bool

	dropped atomic.Int64
}

func newRingBuffer(capacity int) *ringBuffer {
	if capacity <= 0 {
		capacity = 1
	}
	r := &ringBuffer{buf: make([]Event, capacity), cap: capacity}
	r.cond = sync.NewCond(&r.mu)
	return r
}

func (r *ringBuffer) Push(ev Event) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return
	}
	if r.size == r.cap {
		r.buf[r.head] = Event{}
		r.head = (r.head + 1) % r.cap
		r.size--
		r.dropped.Add(1)
	}
	idx := (r.head + r.size) % r.cap
	r.buf[idx] = ev
	r.size++
	r.cond.Signal()
}

func (r *ringBuffer) Pop(ctx context.Context) (Event, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()

	for r.size == 0 && !r.closed {
		if ctx != nil {
			if err := ctx.Err(); err != nil {
				return Event{}, false
			}
		}
		var stopWatch chan struct{}
		if ctx != nil {
			stopWatch = make(chan struct{})
			go func() {
				select {
				case <-ctx.Done():
					r.mu.Lock()
					r.cond.Broadcast()
					r.mu.Unlock()
				case <-stopWatch:
				}
			}()
		}
		r.cond.Wait()
		if stopWatch != nil {
			close(stopWatch)
		}
		if ctx != nil {
			if err := ctx.Err(); err != nil && r.size == 0 {
				return Event{}, false
			}
		}
	}
	if r.size == 0 && r.closed {
		return Event{}, false
	}
	ev := r.buf[r.head]
	r.buf[r.head] = Event{}
	r.head = (r.head + 1) % r.cap
	r.size--
	return ev, true
}

func (r *ringBuffer) Close() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return
	}
	r.closed = true
	r.cond.Broadcast()
}

func (r *ringBuffer) DroppedCount() int64 { return r.dropped.Load() }
