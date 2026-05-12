package agent_test

import (
	"errors"
	"testing"

	agent "github.com/cago-frame/agents/agent"
)

func TestConversation_NewWithID(t *testing.T) {
	c := agent.NewConversation(agent.WithConvID("c-123"))
	if c.ID() != "c-123" {
		t.Fatalf("id mismatch: %q", c.ID())
	}
	if c.Len() != 0 {
		t.Fatalf("expected empty, got %d", c.Len())
	}
}

func TestConversation_NewAutoID(t *testing.T) {
	c := agent.NewConversation()
	if c.ID() == "" {
		t.Fatalf("auto id should be non-empty")
	}
}

func TestConversation_Append(t *testing.T) {
	c := agent.NewConversation()
	idx := c.Append(agent.Message{Role: agent.RoleUser, Content: []agent.ContentBlock{agent.TextBlock{Text: "hi"}}})
	if idx != 0 {
		t.Fatalf("first append should return 0, got %d", idx)
	}
	if c.Len() != 1 {
		t.Fatalf("len should be 1")
	}
	msg, err := c.MessageAt(0)
	if err != nil {
		t.Fatalf("MessageAt(0): %v", err)
	}
	if msg.Role != agent.RoleUser {
		t.Fatalf("role mismatch")
	}
}

func TestConversation_MessageAt_OutOfRange(t *testing.T) {
	c := agent.NewConversation()
	_, err := c.MessageAt(5)
	if !errors.Is(err, agent.ErrIndexOutOfRange) {
		t.Fatalf("want ErrIndexOutOfRange, got %v", err)
	}
}

func TestConversation_Messages_ReturnsCopy(t *testing.T) {
	c := agent.NewConversation()
	c.Append(agent.Message{Role: agent.RoleUser})
	msgs := c.Messages()
	msgs[0].Role = agent.RoleAssistant // mutate caller's copy
	got, _ := c.MessageAt(0)
	if got.Role != agent.RoleUser {
		t.Fatalf("Messages() must return a copy that does not mutate internal state")
	}
}

func TestConversation_LoadConversation(t *testing.T) {
	msgs := []agent.Message{
		{Role: agent.RoleUser, Content: []agent.ContentBlock{agent.TextBlock{Text: "a"}}},
		{Role: agent.RoleAssistant, Content: []agent.ContentBlock{agent.TextBlock{Text: "b"}}},
	}
	c := agent.LoadConversation("c-load", msgs)
	if c.ID() != "c-load" {
		t.Fatalf("id mismatch")
	}
	if c.Len() != 2 {
		t.Fatalf("len = %d, want 2", c.Len())
	}
}

// ConversationReader interface satisfied by *Conversation
func TestConversation_SatisfiesReader(t *testing.T) {
	c := agent.NewConversation()
	var _ agent.ConversationReader = c
}

func TestConversation_Truncate(t *testing.T) {
	c := agent.NewConversation()
	for i := 0; i < 5; i++ {
		c.Append(agent.Message{Role: agent.RoleUser})
	}
	if err := c.Truncate(3); err != nil {
		t.Fatalf("Truncate(3): %v", err)
	}
	if c.Len() != 3 {
		t.Fatalf("len = %d, want 3", c.Len())
	}
}

func TestConversation_Truncate_OutOfRange(t *testing.T) {
	c := agent.NewConversation()
	c.Append(agent.Message{Role: agent.RoleUser})
	if err := c.Truncate(5); !errors.Is(err, agent.ErrIndexOutOfRange) {
		t.Fatalf("want ErrIndexOutOfRange, got %v", err)
	}
}

func TestConversation_Truncate_Negative(t *testing.T) {
	c := agent.NewConversation()
	if err := c.Truncate(-1); !errors.Is(err, agent.ErrIndexOutOfRange) {
		t.Fatalf("want ErrIndexOutOfRange, got %v", err)
	}
}

func TestConversation_Truncate_AtLen_NoOp(t *testing.T) {
	c := agent.NewConversation()
	c.Append(agent.Message{Role: agent.RoleUser})
	c.Append(agent.Message{Role: agent.RoleAssistant})
	if err := c.Truncate(2); err != nil {
		t.Fatalf("Truncate at Len() should be no-op: %v", err)
	}
	if c.Len() != 2 {
		t.Fatalf("len changed unexpectedly: %d", c.Len())
	}
}

func TestConversation_BranchFrom(t *testing.T) {
	parent := agent.NewConversation(agent.WithConvID("parent"))
	parent.Append(agent.Message{Role: agent.RoleUser, Content: []agent.ContentBlock{agent.TextBlock{Text: "u1"}}})
	parent.Append(agent.Message{Role: agent.RoleAssistant, Content: []agent.ContentBlock{agent.TextBlock{Text: "a1"}}})
	parent.Append(agent.Message{Role: agent.RoleUser, Content: []agent.ContentBlock{agent.TextBlock{Text: "u2"}}})

	child, err := parent.BranchFrom(2)
	if err != nil {
		t.Fatalf("BranchFrom(2): %v", err)
	}
	if child.Len() != 2 {
		t.Fatalf("child len = %d, want 2", child.Len())
	}
	if child.ID() == parent.ID() {
		t.Fatalf("child should have a different ID")
	}
	info, ok := child.BranchedFrom()
	if !ok {
		t.Fatalf("BranchedFrom() should return ok=true")
	}
	if info.ParentConvID != "parent" {
		t.Fatalf("ParentConvID = %q", info.ParentConvID)
	}
	if info.ParentIndex != 2 {
		t.Fatalf("ParentIndex = %d", info.ParentIndex)
	}

	// Mutating child must not affect parent.
	child.Append(agent.Message{Role: agent.RoleAssistant})
	if parent.Len() != 3 {
		t.Fatalf("parent should be untouched, got len %d", parent.Len())
	}
}

func TestConversation_BranchFrom_OutOfRange(t *testing.T) {
	c := agent.NewConversation()
	if _, err := c.BranchFrom(5); err == nil {
		t.Fatalf("BranchFrom past end should error")
	}
}

func TestConversation_WithBranchedFrom(t *testing.T) {
	c := agent.NewConversation(agent.WithBranchedFrom(agent.BranchInfo{
		ParentConvID: "parent",
		ParentIndex:  1,
	}))
	if c == nil {
		t.Fatalf("NewConversation with WithBranchedFrom returned nil")
	}
	info, ok := c.BranchedFrom()
	if !ok {
		t.Fatalf("BranchedFrom() should return ok=true after WithBranchedFrom")
	}
	if info.ParentConvID != "parent" {
		t.Fatalf("ParentConvID = %q, want parent", info.ParentConvID)
	}
	if info.ParentIndex != 1 {
		t.Fatalf("ParentIndex = %d, want 1", info.ParentIndex)
	}
}
