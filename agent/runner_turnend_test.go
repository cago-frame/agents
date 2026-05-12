package agent_test

import (
	"context"
	"testing"

	agent "github.com/cago-frame/agents/agent"
	"github.com/cago-frame/agents/provider"
	"github.com/cago-frame/agents/provider/providertest"
)

func TestTurnEnd_HookFires(t *testing.T) {
	prov := providertest.New().QueueStream(
		provider.StreamChunk{ContentDelta: "x"},
		provider.StreamChunk{FinishReason: provider.FinishStop},
	)
	var called bool
	var receivedStop agent.StopReason
	a := agent.New(prov,
		agent.TurnEnd(func(ctx context.Context, in *agent.TurnEndInput) (*agent.TurnEndOutput, error) {
			called = true
			receivedStop = in.StopReason
			return &agent.TurnEndOutput{}, nil
		}),
	)
	conv := agent.NewConversation()
	r := a.Runner(conv)
	defer func() { _ = r.Close() }()
	events, _ := r.Send(context.Background(), "hi")
	for range events {
	}
	if !called {
		t.Fatalf("TurnEnd hook should have fired")
	}
	if receivedStop != agent.StopEndTurn {
		t.Fatalf("stop reason mismatch, got %v", receivedStop)
	}
}

func TestTurnEndHook_EmittedEvents(t *testing.T) {
	t.Parallel()
	prov := providertest.New().QueueStream(
		provider.StreamChunk{ContentDelta: "hi"},
		provider.StreamChunk{FinishReason: provider.FinishStop},
	)

	customEv := agent.Event{Kind: agent.EventCompacted, Delta: "compacted summary"}
	a := agent.New(prov,
		agent.TurnEnd(func(ctx context.Context, in *agent.TurnEndInput) (*agent.TurnEndOutput, error) {
			return &agent.TurnEndOutput{EmittedEvents: []agent.Event{customEv}}, nil
		}),
	)
	conv := agent.NewConversation()
	r := a.Runner(conv)
	defer func() { _ = r.Close() }()

	events, _ := r.Send(context.Background(), "go")
	var sawCompacted bool
	var sawTurnEnd bool
	turnEndIndex := -1
	compactedIndex := -1
	idx := 0
	for ev := range events {
		switch ev.Kind {
		case agent.EventTurnEnd:
			sawTurnEnd = true
			turnEndIndex = idx
		case agent.EventCompacted:
			sawCompacted = true
			compactedIndex = idx
			if ev.Delta != "compacted summary" {
				t.Fatalf("EventCompacted Delta = %q", ev.Delta)
			}
		}
		idx++
	}
	if !sawCompacted {
		t.Fatal("EventCompacted not emitted")
	}
	if !sawTurnEnd {
		t.Fatal("EventTurnEnd not emitted")
	}
	if compactedIndex <= turnEndIndex {
		t.Fatalf("EventCompacted (idx=%d) should come AFTER EventTurnEnd (idx=%d)", compactedIndex, turnEndIndex)
	}
}
