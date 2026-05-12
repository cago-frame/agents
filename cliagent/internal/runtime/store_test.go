package runtime

import (
	"context"
	"testing"
)

func TestMemoryStore_RoundTrip(t *testing.T) {
	s := NewMemoryStore()
	ctx := context.Background()
	if _, ok, err := s.Load(ctx, "x"); ok || err != nil {
		t.Fatalf("expected miss, got ok=%v err=%v", ok, err)
	}
	if err := s.Save(ctx, "x", SessionData{Messages: []Message{{Role: RoleUser, Text: "hi"}}}); err != nil {
		t.Fatalf("save: %v", err)
	}
	data, ok, err := s.Load(ctx, "x")
	if !ok || err != nil {
		t.Fatalf("load: ok=%v err=%v", ok, err)
	}
	if len(data.Messages) != 1 || data.Messages[0].Text != "hi" {
		t.Errorf("data=%+v", data)
	}
	if err := s.Delete(ctx, "x"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, ok, _ := s.Load(ctx, "x"); ok {
		t.Fatal("expected miss after delete")
	}
}

func TestMemoryStore_DeepCopiesMessages(t *testing.T) {
	s := NewMemoryStore()
	ctx := context.Background()
	msgs := []Message{{Role: RoleUser, Text: "alpha"}}
	if err := s.Save(ctx, "x", SessionData{Messages: msgs}); err != nil {
		t.Fatal(err)
	}
	msgs[0].Text = "MUTATED"
	data, _, _ := s.Load(ctx, "x")
	if data.Messages[0].Text != "alpha" {
		t.Errorf("mutation leaked: %q", data.Messages[0].Text)
	}
}

func TestMemoryStore_DeepCopiesStateValues(t *testing.T) {
	s := NewMemoryStore()
	ctx := context.Background()
	vals := map[string]any{"k": "v"}
	if err := s.Save(ctx, "x", SessionData{State: State{ThreadID: "t1", Values: vals}}); err != nil {
		t.Fatal(err)
	}
	vals["k"] = "MUTATED"
	data, _, _ := s.Load(ctx, "x")
	if data.State.Values["k"] != "v" {
		t.Errorf("state mutation leaked: %v", data.State.Values["k"])
	}
}
