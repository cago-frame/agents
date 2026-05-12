package runtime

import (
	"context"
	"errors"
	"testing"
)

func TestSession_AcquireReleasesStream(t *testing.T) {
	s := NewSession("s1", nil)
	st, err := s.AcquireStream(8)
	if err != nil || st == nil {
		t.Fatalf("acquire1: err=%v st=%v", err, st)
	}
	if _, err := s.AcquireStream(8); !errors.Is(err, ErrSessionBusy) {
		t.Fatalf("expected ErrSessionBusy, got %v", err)
	}
	s.ReleaseStream(st)
	st2, err := s.AcquireStream(8)
	if err != nil || st2 == nil {
		t.Fatalf("acquire2: err=%v", err)
	}
}

func TestSession_FollowUpQueue(t *testing.T) {
	s := NewSession("s2", nil)
	if err := s.EnqueueFollowUp("a"); err != nil {
		t.Fatal(err)
	}
	if err := s.EnqueueFollowUp("b"); err != nil {
		t.Fatal(err)
	}
	if got, ok := s.DequeueFollowUp(); !ok || got != "a" {
		t.Errorf("got %q ok=%v", got, ok)
	}
	if got, ok := s.DequeueFollowUp(); !ok || got != "b" {
		t.Errorf("got %q ok=%v", got, ok)
	}
	if _, ok := s.DequeueFollowUp(); ok {
		t.Error("expected empty queue")
	}
}

func TestSession_HistoryAppend(t *testing.T) {
	s := NewSession("s3", nil)
	s.AppendHistory(Message{Role: RoleUser, Text: "hi"})
	s.AppendHistory(Message{Role: RoleAssistant, Text: "hello"})
	h := s.History()
	if len(h) != 2 || h[0].Text != "hi" || h[1].Text != "hello" {
		t.Errorf("history=%+v", h)
	}
}

func TestSession_PersistsToStore(t *testing.T) {
	store := NewMemoryStore()
	s := NewSession("s4", store)
	s.AppendHistory(Message{Role: RoleUser, Text: "alpha"})
	s.UpdateState(func(st *State) { st.ThreadID = "tid-1" })
	data, ok, err := store.Load(context.Background(), "s4")
	if err != nil || !ok {
		t.Fatalf("Load: ok=%v err=%v", ok, err)
	}
	if len(data.Messages) != 1 || data.Messages[0].Text != "alpha" {
		t.Errorf("messages=%+v", data.Messages)
	}
	if data.State.ThreadID != "tid-1" {
		t.Errorf("state=%+v", data.State)
	}
}

func TestSession_Clear_DeletesAll(t *testing.T) {
	store := NewMemoryStore()
	s := NewSession("s5", store)
	s.AppendHistory(Message{Role: RoleUser, Text: "x"})
	if err := s.Clear(context.Background()); err != nil {
		t.Fatal(err)
	}
	if h := s.History(); len(h) != 0 {
		t.Errorf("history not cleared: %+v", h)
	}
	if _, ok, _ := store.Load(context.Background(), "s5"); ok {
		t.Error("store entry not deleted")
	}
}

func TestSession_LoadFromStore_HydratesHistory(t *testing.T) {
	store := NewMemoryStore()
	if err := store.Save(context.Background(), "s6", SessionData{
		Messages: []Message{{Role: RoleAssistant, Text: "rebuild"}},
		State:    State{ThreadID: "rt"},
	}); err != nil {
		t.Fatal(err)
	}
	s := NewSession("s6", store)
	if err := s.LoadFromStore(context.Background()); err != nil {
		t.Fatal(err)
	}
	if h := s.History(); len(h) != 1 || h[0].Text != "rebuild" {
		t.Errorf("history=%+v", h)
	}
	if s.State().ThreadID != "rt" {
		t.Errorf("state=%+v", s.State())
	}
}

func TestSession_AcquireAfterClose_Errors(t *testing.T) {
	s := NewSession("s7", nil)
	_ = s.Close(context.Background())
	if _, err := s.AcquireStream(8); !errors.Is(err, ErrStreamClosed) {
		t.Errorf("expected ErrStreamClosed, got %v", err)
	}
}
