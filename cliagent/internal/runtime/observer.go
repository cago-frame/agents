package runtime

import "context"

type Observer func(ctx context.Context, ev Event)

type ObserverEntry struct {
	Fn       Observer
	Async    bool
	BufferSz int
}

func Observe(fn Observer, opts ...ObserverOption) ObserverEntry {
	e := ObserverEntry{Fn: fn}
	for _, o := range opts {
		o(&e)
	}
	return e
}

type ObserverOption func(*ObserverEntry)

func Async() ObserverOption { return func(e *ObserverEntry) { e.Async = true } }
func AsyncBuffer(n int) ObserverOption {
	return func(e *ObserverEntry) { e.Async = true; e.BufferSz = n }
}
