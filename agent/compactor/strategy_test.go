package compactor_test

import (
	"context"
	"testing"

	agent "github.com/cago-frame/agents/agent"
	"github.com/cago-frame/agents/agent/compactor"
)

func TestNoop_NeverCompacts(t *testing.T) {
	s := compactor.Noop()
	conv := agent.NewConversation()
	conv.Append(agent.Message{Role: agent.RoleUser, Content: []agent.ContentBlock{agent.TextBlock{Text: "x"}}})
	if s.ShouldCompact(conv) {
		t.Fatal("Noop.ShouldCompact must always return false")
	}
	_, err := s.Compact(context.Background(), conv)
	_ = err
}
