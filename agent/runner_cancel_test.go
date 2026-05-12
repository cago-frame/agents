package agent_test

import (
	"context"
	"errors"
	"testing"
	"time"

	agent "github.com/cago-frame/agents/agent"
	"github.com/cago-frame/agents/provider"
	"github.com/cago-frame/agents/provider/providertest"
)

func TestRunner_Cancel_PartialPreserved(t *testing.T) {
	// Mock that emits 2 deltas then blocks (no FinishReason).
	prov := providertest.New().QueueStream(
		provider.StreamChunk{ContentDelta: "Once "},
		provider.StreamChunk{ContentDelta: "upon a"},
		// no FinishReason — simulates ongoing stream
	)
	a := agent.New(prov)
	conv := agent.NewConversation()
	r := a.Runner(conv)
	defer func() { _ = r.Close() }()

	events, err := r.Send(context.Background(), "tell story")
	if err != nil {
		t.Fatalf("send: %v", err)
	}

	// Consume the deltas, then cancel.
	deltaCount := 0
	drainDone := make(chan struct{})
	var sawCancelEvent bool
	go func() {
		defer close(drainDone)
		for ev := range events {
			switch ev.Kind {
			case agent.EventTextDelta:
				deltaCount++
				if deltaCount == 2 {
					if err := r.Cancel("test"); err != nil {
						t.Errorf("cancel: %v", err)
					}
				}
			case agent.EventCancelled:
				sawCancelEvent = true
			}
		}
	}()

	select {
	case <-drainDone:
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for events to drain")
	}

	if !sawCancelEvent {
		t.Fatalf("expected EventCancelled in stream")
	}
	last, _ := conv.MessageAt(conv.Len() - 1)
	if last.PartialReason != agent.PartialCancelled {
		t.Fatalf("PartialReason = %q, want %q", last.PartialReason, agent.PartialCancelled)
	}
	if got := textOfBlocks(last.Content); got != "Once upon a" {
		t.Fatalf("partial text = %q", got)
	}
}

// TestRunner_Error_PartialResumedInNextTurn verifies that an errored partial
// (provider chunk Err) is preserved in the conversation and replayed to the
// LLM on the next turn so the model can pick up where it left off. The error
// itself is only emitted as an Event and never enters conversation history.
func TestRunner_Error_PartialResumedInNextTurn(t *testing.T) {
	boom := errors.New("simulated provider failure")
	prov := providertest.New().
		QueueStream(
			provider.StreamChunk{ContentDelta: "In a galaxy "},
			provider.StreamChunk{ContentDelta: "far, far"},
			provider.StreamChunk{Err: boom},
		).
		QueueStream(
			provider.StreamChunk{ContentDelta: " away..."},
			provider.StreamChunk{FinishReason: provider.FinishStop},
		)
	a := agent.New(prov)
	conv := agent.NewConversation()
	r := a.Runner(conv)
	defer func() { _ = r.Close() }()

	events, _ := r.Send(context.Background(), "tell me a story")
	var sawErr error
	for ev := range events {
		if ev.Kind == agent.EventError {
			sawErr = ev.Error
		}
	}
	if !errors.Is(sawErr, boom) {
		t.Fatalf("EventError = %v, want %v", sawErr, boom)
	}

	// Partial saved with PartialErrored, no error text in conversation.
	last, _ := conv.MessageAt(conv.Len() - 1)
	if last.PartialReason != agent.PartialErrored {
		t.Fatalf("PartialReason = %q, want %q", last.PartialReason, agent.PartialErrored)
	}
	if got := textOfBlocks(last.Content); got != "In a galaxy far, far" {
		t.Fatalf("partial text = %q", got)
	}

	// Next turn must include the errored partial in the LLM request.
	events2, _ := r.Send(context.Background(), "continue please")
	for range events2 {
	}
	turn2 := prov.Received()[1]
	if len(turn2.Messages) != 3 {
		t.Fatalf("turn2 messages = %d, want 3", len(turn2.Messages))
	}
	if turn2.Messages[1].Role != provider.RoleAssistant || turn2.Messages[1].Content != "In a galaxy far, far" {
		t.Fatalf("errored partial not replayed: %+v", turn2.Messages[1])
	}
}

// TestRunner_Cancel_PartialResumedInNextTurn verifies the new default
// behavior: a canceled partial assistant message is replayed to the LLM
// in the subsequent turn so the model can pick up where it left off.
func TestRunner_Cancel_PartialResumedInNextTurn(t *testing.T) {
	prov := providertest.New().
		QueueStream(
			provider.StreamChunk{ContentDelta: "Once "},
			provider.StreamChunk{ContentDelta: "upon a"},
			// no FinishReason — caller will Cancel
		).
		QueueStream(
			provider.StreamChunk{ContentDelta: " time."},
			provider.StreamChunk{FinishReason: provider.FinishStop},
		)
	a := agent.New(prov)
	conv := agent.NewConversation()
	r := a.Runner(conv)
	defer func() { _ = r.Close() }()

	// Turn 1: cancel mid-stream.
	events, err := r.Send(context.Background(), "tell story")
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	deltaCount := 0
	for ev := range events {
		if ev.Kind == agent.EventTextDelta {
			deltaCount++
			if deltaCount == 2 {
				_ = r.Cancel("user stop")
			}
		}
	}

	// Turn 2: continue.
	events2, err := r.Send(context.Background(), "ok continue")
	if err != nil {
		t.Fatalf("send 2: %v", err)
	}
	for range events2 {
	}

	// Inspect what the provider received on turn 2: must include the
	// canceled partial as an assistant message.
	reqs := prov.Received()
	if len(reqs) != 2 {
		t.Fatalf("provider got %d requests, want 2", len(reqs))
	}
	turn2 := reqs[1]
	// Expect: [user "tell story", assistant "Once upon a", user "ok continue"].
	if len(turn2.Messages) != 3 {
		t.Fatalf("turn2 messages = %d, want 3 (%+v)", len(turn2.Messages), turn2.Messages)
	}
	if turn2.Messages[1].Role != provider.RoleAssistant {
		t.Fatalf("turn2 second message role = %v, want assistant", turn2.Messages[1].Role)
	}
	if turn2.Messages[1].Content != "Once upon a" {
		t.Fatalf("canceled partial content not replayed: %q", turn2.Messages[1].Content)
	}
}
