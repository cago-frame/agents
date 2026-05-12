package agent_test

import (
	"context"
	"strings"
	"testing"

	agent "github.com/cago-frame/agents/agent"
	"github.com/cago-frame/agents/provider"
	"github.com/cago-frame/agents/provider/providertest"
)

func TestUserPromptSubmit_ModifiedText(t *testing.T) {
	prov := providertest.New().QueueStream(
		provider.StreamChunk{ContentDelta: "ok"},
		provider.StreamChunk{FinishReason: provider.FinishStop},
	)
	a := agent.New(prov,
		agent.UserPromptSubmit(func(ctx context.Context, in *agent.UserPromptInput) (*agent.UserPromptOutput, error) {
			return &agent.UserPromptOutput{ModifiedText: strings.ToUpper(in.Text)}, nil
		}),
	)
	conv := agent.NewConversation()
	r := a.Runner(conv)
	defer func() { _ = r.Close() }()
	events, _ := r.Send(context.Background(), "hello")
	for range events {
	}
	msg, _ := conv.MessageAt(0)
	if textOfBlocks(msg.Content) != "HELLO" {
		t.Fatalf("user msg should be uppercased, got %q", textOfBlocks(msg.Content))
	}
}

func TestUserPromptSubmit_Deny(t *testing.T) {
	prov := providertest.New() // no queued stream because deny short-circuits
	a := agent.New(prov,
		agent.UserPromptSubmit(func(ctx context.Context, in *agent.UserPromptInput) (*agent.UserPromptOutput, error) {
			return &agent.UserPromptOutput{Decision: agent.DecisionDeny, DenyReason: "no"}, nil
		}),
	)
	conv := agent.NewConversation()
	r := a.Runner(conv)
	defer func() { _ = r.Close() }()

	events, err := r.Send(context.Background(), "x")
	if err != nil {
		t.Fatalf("send setup: %v", err)
	}
	var stopReason agent.StopReason
	var sawTurnEnd bool
	for ev := range events {
		if ev.Kind == agent.EventTurnEnd {
			stopReason = ev.StopReason
			sawTurnEnd = true
		}
	}
	if !sawTurnEnd || stopReason != agent.StopHook {
		t.Fatalf("expected TurnEnd with StopHook, got sawTurnEnd=%v reason=%v", sawTurnEnd, stopReason)
	}
}
