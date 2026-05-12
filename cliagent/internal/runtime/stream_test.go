package runtime

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestStream_EmitNext(t *testing.T) {
	s := NewStream(8)
	go func() {
		s.Emit(Event{Kind: EventTextDelta, Text: "hello"})
		s.Emit(Event{Kind: EventDone})
		_ = s.Close(context.Background())
	}()
	if !s.Next() {
		t.Fatal("expected first event")
	}
	if s.Event().Text != "hello" {
		t.Errorf("text=%q", s.Event().Text)
	}
	if !s.Next() {
		t.Fatal("expected done")
	}
	if s.Event().Kind != EventDone {
		t.Errorf("kind=%s", s.Event().Kind)
	}
	if s.Next() {
		t.Fatal("expected end of stream")
	}
}

func TestStream_DropOldest(t *testing.T) {
	s := NewStream(2)
	s.Emit(Event{Kind: EventTextDelta, Text: "a"})
	s.Emit(Event{Kind: EventTextDelta, Text: "b"})
	s.Emit(Event{Kind: EventTextDelta, Text: "c"}) // drops "a"
	_ = s.Close(context.Background())
	var got []string
	for s.Next() {
		got = append(got, s.Event().Text)
	}
	if len(got) != 2 || got[0] != "b" || got[1] != "c" {
		t.Errorf("got=%v", got)
	}
	if s.DroppedCount() != 1 {
		t.Errorf("DroppedCount=%d want 1", s.DroppedCount())
	}
}

func TestStream_CloseUnblocksNext(t *testing.T) {
	s := NewStream(4)
	done := make(chan bool, 1)
	go func() { done <- s.Next() }()
	time.Sleep(20 * time.Millisecond)
	_ = s.Close(context.Background())
	select {
	case ok := <-done:
		if ok {
			t.Errorf("Next returned true after close")
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("Next didn't unblock on close")
	}
}

func TestStream_Steer_NoLauncher(t *testing.T) {
	s := NewStream(4)
	err := s.Steer(context.Background(), "hi")
	if err != ErrSteeringUnavailable {
		t.Errorf("err=%v want ErrSteeringUnavailable", err)
	}
}

func TestStream_Steer_CallsInjectFn(t *testing.T) {
	s := NewStream(4)
	var got string
	s.SetInjectFn(func(_ context.Context, kind, payload string) error {
		if kind == "user" {
			got = payload
		}
		return nil
	})
	if err := s.Steer(context.Background(), "ping"); err != nil {
		t.Fatal(err)
	}
	if got != "ping" {
		t.Errorf("got=%q", got)
	}
}

func TestStream_FollowUp_CallsInjectFn(t *testing.T) {
	s := NewStream(4)
	var got string
	s.SetInjectFn(func(_ context.Context, kind, payload string) error {
		if kind == "follow_up" {
			got = payload
		}
		return nil
	})
	if err := s.FollowUp(context.Background(), "next"); err != nil {
		t.Fatal(err)
	}
	if got != "next" {
		t.Errorf("got=%q", got)
	}
}

func TestStream_CloseFn_Invoked(t *testing.T) {
	s := NewStream(4)
	var called atomic.Bool
	s.SetCloseFn(func(_ context.Context) error {
		called.Store(true)
		return nil
	})
	if err := s.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !called.Load() {
		t.Error("closeFn not called")
	}
	// Idempotent: second close doesn't fire again.
	called.Store(false)
	_ = s.Close(context.Background())
	if called.Load() {
		t.Error("closeFn called twice")
	}
}

func TestStream_SyncObserverSeesEvents(t *testing.T) {
	s := NewStream(4)
	var seen []EventKind
	s.AddObserver(Observe(func(_ context.Context, ev Event) {
		seen = append(seen, ev.Kind)
	}))
	s.Emit(Event{Kind: EventTextDelta})
	s.Emit(Event{Kind: EventDone})
	if len(seen) != 2 || seen[0] != EventTextDelta || seen[1] != EventDone {
		t.Errorf("seen=%v", seen)
	}
}

func TestStream_AsyncObserverSeesEvents(t *testing.T) {
	s := NewStream(4)
	var counter atomic.Int32
	done := make(chan struct{})
	s.AddObserver(Observe(func(_ context.Context, _ Event) {
		if counter.Add(1) == 2 {
			close(done)
		}
	}, Async()))
	s.Emit(Event{Kind: EventTextDelta})
	s.Emit(Event{Kind: EventDone})
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("async observer didn't see both events")
	}
	_ = s.Close(context.Background())
}

func TestStream_State_TrackedFromEvent(t *testing.T) {
	s := NewStream(4)
	s.Emit(Event{Kind: EventSessionStart, SessionID: "abc"})
	if s.SessionID() != "abc" {
		t.Errorf("SessionID=%q", s.SessionID())
	}
	if s.State().ThreadID != "abc" {
		t.Errorf("State.ThreadID=%q", s.State().ThreadID)
	}
	_ = s.Close(context.Background())
}

func TestStream_EmitDuringClose_NoPanic(t *testing.T) {
	// Stress-test concurrent Emit + Close. Without the closing-flag guard,
	// this would panic with "send on closed channel".
	for i := 0; i < 50; i++ {
		s := NewStream(8)
		s.AddObserver(Observe(func(_ context.Context, _ Event) {}, Async()))
		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			for j := 0; j < 1000; j++ {
				s.Emit(Event{Kind: EventTextDelta, Text: "x"})
			}
		}()
		go func() {
			defer wg.Done()
			time.Sleep(time.Microsecond)
			_ = s.Close(context.Background())
		}()
		wg.Wait()
	}
}

func TestStream_Result_RoundTrip(t *testing.T) {
	s := NewStream(4)
	s.SetResult(&Result{Text: "final"}, nil)
	res, err := s.Result()
	if err != nil {
		t.Fatal(err)
	}
	if res == nil || res.Text != "final" {
		t.Errorf("res=%+v", res)
	}
}
