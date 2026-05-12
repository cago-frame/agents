package agent_test

import (
	"context"
	"testing"

	agent "github.com/cago-frame/agents/agent"
	"github.com/cago-frame/agents/provider/providertest"
)

func TestPreparedRequest_DryRun(t *testing.T) {
	a := agent.New(providertest.New(), agent.System("sysprompt"), agent.Model("gpt-4"))
	conv := agent.NewConversation()
	conv.Append(agent.Message{Role: agent.RoleUser, Content: []agent.ContentBlock{agent.TextBlock{Text: "old"}}})
	r := a.Runner(conv)
	defer func() { _ = r.Close() }()
	req, err := r.PreparedRequest(context.Background(), "new")
	if err != nil {
		t.Fatalf("prepared: %v", err)
	}
	if req.Model != "gpt-4" {
		t.Fatalf("model")
	}
	if len(req.Messages) < 2 {
		t.Fatalf("expected sys + user(s), got %d", len(req.Messages))
	}
	// PreparedRequest must NOT have appended a new user to conv.
	if conv.Len() != 1 {
		t.Fatalf("conv should be unchanged, got len %d", conv.Len())
	}
}
