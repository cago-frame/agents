package agent_test

import (
	"errors"
	"testing"

	agent "github.com/cago-frame/agents/agent"
	"github.com/cago-frame/agents/provider/providertest"
)

func TestRunner_New(t *testing.T) {
	a := agent.New(providertest.New())
	conv := agent.NewConversation()
	r := a.Runner(conv)
	if r == nil {
		t.Fatalf("Runner is nil")
	}
	if r.Conversation() != conv {
		t.Fatalf("Runner.Conversation should return the bound conv")
	}
	if err := r.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}

func TestRunner_Close_Idempotent(t *testing.T) {
	a := agent.New(providertest.New())
	conv := agent.NewConversation()
	r := a.Runner(conv)
	if err := r.Close(); err != nil {
		t.Fatalf("first Close: %v", err)
	}
	if err := r.Close(); err != nil {
		t.Fatalf("second Close should be nil, got %v", err)
	}
}

func TestRunner_ConversationBusy(t *testing.T) {
	a := agent.New(providertest.New())
	conv := agent.NewConversation()
	r1 := a.Runner(conv)
	defer func() { _ = r1.Close() }()

	r2, err := a.TryRunner(conv)
	if err == nil {
		t.Fatalf("expected ErrConversationBusy, got nil; r2=%p", r2)
	}
	if !errors.Is(err, agent.ErrConversationBusy) {
		t.Fatalf("expected ErrConversationBusy, got %v", err)
	}
}

func TestRunner_AfterCloseReleasesLock(t *testing.T) {
	a := agent.New(providertest.New())
	conv := agent.NewConversation()
	r1 := a.Runner(conv)
	if err := r1.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	r2, err := a.TryRunner(conv)
	if err != nil {
		t.Fatalf("after close, second runner should succeed: %v", err)
	}
	_ = r2.Close()
}
