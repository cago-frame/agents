package filemu_test

import (
	"sync"
	"sync/atomic"
	"testing"

	"github.com/cago-frame/agents/tool/internal/filemu"
)

func TestSerializesSamePath(t *testing.T) {
	var p filemu.Pool
	var inFlight int32
	var maxOverlap int32
	var wg sync.WaitGroup
	for range 50 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = p.With("/x", func() error {
				cur := atomic.AddInt32(&inFlight, 1)
				for {
					m := atomic.LoadInt32(&maxOverlap)
					if cur <= m || atomic.CompareAndSwapInt32(&maxOverlap, m, cur) {
						break
					}
				}
				atomic.AddInt32(&inFlight, -1)
				return nil
			})
		}()
	}
	wg.Wait()
	if maxOverlap != 1 {
		t.Fatalf("expected serialized, got max overlap %d", maxOverlap)
	}
}

func TestDifferentPathsParallel(t *testing.T) {
	var p filemu.Pool
	var ranA, ranB bool
	a := make(chan struct{})
	b := make(chan struct{})
	done := make(chan struct{}, 2)
	go func() {
		_ = p.With("/a", func() error {
			ranA = true
			close(a)
			<-b
			return nil
		})
		done <- struct{}{}
	}()
	go func() {
		<-a
		_ = p.With("/b", func() error {
			ranB = true
			close(b)
			return nil
		})
		done <- struct{}{}
	}()
	<-done
	<-done
	if !ranA || !ranB {
		t.Fatalf("both should have run")
	}
}
