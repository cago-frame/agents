package agent_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	agent "github.com/cago-frame/agents/agent"
	"github.com/cago-frame/agents/provider"
	"github.com/cago-frame/agents/provider/providertest"
)

func TestResend_RegeneratesFromExistingHistory(t *testing.T) {
	prov := providertest.New().
		QueueStream(provider.StreamChunk{ContentDelta: "first"}, provider.StreamChunk{FinishReason: provider.FinishStop}).
		QueueStream(provider.StreamChunk{ContentDelta: "second"}, provider.StreamChunk{FinishReason: provider.FinishStop})

	a := agent.New(prov)
	conv := agent.NewConversation()
	r := a.Runner(conv)
	events, err := r.Send(context.Background(), "hi")
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	for range events {
	}
	_ = r.Close()

	if conv.Len() != 2 {
		t.Fatalf("after first turn, conv len = %d", conv.Len())
	}

	// Truncate to drop the assistant, then Resend.
	if err := conv.Truncate(1); err != nil {
		t.Fatalf("truncate: %v", err)
	}
	r2 := a.Runner(conv)
	defer func() { _ = r2.Close() }()
	events2, err := r2.Resend(context.Background())
	if err != nil {
		t.Fatalf("resend: %v", err)
	}
	var deltas []string
	for ev := range events2 {
		if ev.Kind == agent.EventTextDelta {
			deltas = append(deltas, ev.Delta)
		}
	}
	if len(deltas) != 1 || deltas[0] != "second" {
		t.Fatalf("expected second turn output, got %v", deltas)
	}
	// Conv should now be: user, assistant("second")
	if conv.Len() != 2 {
		t.Fatalf("after resend len = %d", conv.Len())
	}
}

func TestResend_RequiresUserAtTail(t *testing.T) {
	a := agent.New(providertest.New())
	conv := agent.NewConversation()
	conv.Append(agent.Message{Role: agent.RoleAssistant})
	r := a.Runner(conv)
	defer func() { _ = r.Close() }()
	_, err := r.Resend(context.Background())
	if !errors.Is(err, agent.ErrCannotResend) {
		t.Fatalf("want ErrCannotResend, got %v", err)
	}
}

func TestResend_OnEmptyConvFails(t *testing.T) {
	a := agent.New(providertest.New())
	conv := agent.NewConversation()
	r := a.Runner(conv)
	defer func() { _ = r.Close() }()
	_, err := r.Resend(context.Background())
	if !errors.Is(err, agent.ErrCannotResend) {
		t.Fatalf("want ErrCannotResend, got %v", err)
	}
}

func TestResend_ClosedRunnerFails(t *testing.T) {
	a := agent.New(providertest.New())
	conv := agent.NewConversation()
	conv.Append(agent.Message{Role: agent.RoleUser, Content: []agent.ContentBlock{agent.TextBlock{Text: "hi"}}})
	r := a.Runner(conv)
	_ = r.Close()
	_, err := r.Resend(context.Background())
	if !errors.Is(err, agent.ErrRunnerClosed) {
		t.Fatalf("want ErrRunnerClosed, got %v", err)
	}
}

func TestResend_ContextCancelledFails(t *testing.T) {
	a := agent.New(providertest.New())
	conv := agent.NewConversation()
	conv.Append(agent.Message{Role: agent.RoleUser, Content: []agent.ContentBlock{agent.TextBlock{Text: "hi"}}})
	r := a.Runner(conv)
	defer func() { _ = r.Close() }()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := r.Resend(ctx)
	if err == nil {
		t.Fatalf("want context error, got nil")
	}
}

func TestResend_EmitsDoneEvent(t *testing.T) {
	prov := providertest.New().
		QueueStream(provider.StreamChunk{ContentDelta: "resp"}, provider.StreamChunk{FinishReason: provider.FinishStop})

	a := agent.New(prov)
	conv := agent.NewConversation()
	conv.Append(agent.Message{Role: agent.RoleUser, Content: []agent.ContentBlock{agent.TextBlock{Text: "hi"}}})
	r := a.Runner(conv)
	defer func() { _ = r.Close() }()

	events, err := r.Resend(context.Background())
	if err != nil {
		t.Fatalf("resend: %v", err)
	}
	var sawTurnEnd, sawDone bool
	var text strings.Builder
	for ev := range events {
		switch ev.Kind {
		case agent.EventTextDelta:
			text.WriteString(ev.Delta)
		case agent.EventTurnEnd:
			sawTurnEnd = true
		case agent.EventDone:
			sawDone = true
		}
	}
	if !sawTurnEnd || !sawDone {
		t.Fatalf("missing TurnEnd or Done")
	}
	if text.String() != "resp" {
		t.Fatalf("text = %q, want %q", text.String(), "resp")
	}
}
