package agent_test

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"testing"
	"time"

	agent "github.com/cago-frame/agents/agent"
	"github.com/cago-frame/agents/provider"
	"github.com/cago-frame/agents/provider/providertest"
)

// blockingTool runs until its ctx ends, like a long remote command that honors
// cancellation, and reports the cancellation as its error.
type blockingTool struct{ started chan struct{} }

func (blockingTool) Name() string         { return "Slow" }
func (blockingTool) Description() string  { return "runs until canceled" }
func (blockingTool) Schema() agent.Schema { return agent.Schema{Type: "object"} }
func (b blockingTool) Call(ctx context.Context, _ map[string]any) (*agent.ToolResultBlock, error) {
	close(b.started)
	<-ctx.Done()
	return nil, ctx.Err()
}

func drainKinds(t *testing.T, events func(func(agent.Event) bool), onEvent func(agent.Event)) []agent.EventKind {
	t.Helper()
	var kinds []agent.EventKind
	done := make(chan struct{})
	go func() {
		defer close(done)
		for ev := range events {
			kinds = append(kinds, ev.Kind)
			if onEvent != nil {
				onEvent(ev)
			}
		}
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatalf("turn did not end")
	}
	return kinds
}

// TestRunner_Cancel_DuringToolDispatch: a Cancel that lands while a tool is
// running ends the turn as canceled once the tool returns. The runner must not
// fold still-queued steers into the canceled turn nor ask the provider again —
// the next request would carry the canceled ctx and surface as EventError,
// so consumers would see a failure instead of the stop they asked for.
func TestRunner_Cancel_DuringToolDispatch(t *testing.T) {
	args, _ := json.Marshal(map[string]any{})
	prov := providertest.New().
		QueueStream(
			provider.StreamChunk{ToolCallDelta: &provider.ToolCallDelta{Index: 0, ID: "tu_1", Name: "Slow", ArgsDelta: string(args)}},
			provider.StreamChunk{FinishReason: provider.FinishToolCalls},
		).
		QueueStream(
			provider.StreamChunk{ContentDelta: "should not be requested"},
			provider.StreamChunk{FinishReason: provider.FinishStop},
		)
	tool := blockingTool{started: make(chan struct{})}
	a := agent.New(prov, agent.Tools(tool))
	conv := agent.NewConversation()
	r := a.Runner(conv)
	defer func() { _ = r.Close() }()

	events, err := r.Send(context.Background(), "go")
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	go func() {
		<-tool.started
		if err := r.Steer(context.Background(), "queued while the tool ran"); err != nil {
			t.Errorf("steer: %v", err)
		}
		if err := r.Cancel("user_stop"); err != nil {
			t.Errorf("cancel: %v", err)
		}
	}()
	kinds := drainKinds(t, events, nil)

	if slices.Contains(kinds, agent.EventError) || slices.Contains(kinds, agent.EventSteerConsumed) {
		t.Fatalf("events = %v, want no EventError / EventSteerConsumed", kinds)
	}
	if !slices.Contains(kinds, agent.EventCancelled) || kinds[len(kinds)-1] != agent.EventDone {
		t.Fatalf("events = %v, want EventCancelled ... EventDone", kinds)
	}
	if n := len(prov.Received()); n != 1 {
		t.Fatalf("provider requests = %d, want 1 (no request after the cancel)", n)
	}
	// The tool_use the model asked for still has its result, so the next turn stays valid.
	if last, _ := conv.MessageAt(conv.Len() - 1); last.Role != agent.RoleTool {
		t.Fatalf("last message role = %v, want the tool result", last.Role)
	}
}

// TestRunner_Cancel_BeforeChatStreamEndsAsCancelled: a Cancel that lands after
// the safe point but before the provider call makes ChatStream fail with the
// canceled ctx. That failure is the cancel, not a provider error.
func TestRunner_Cancel_BeforeChatStreamEndsAsCancelled(t *testing.T) {
	args, _ := json.Marshal(map[string]any{"text": "hi"})
	prov := providertest.New().
		QueueStream(
			provider.StreamChunk{ToolCallDelta: &provider.ToolCallDelta{Index: 0, ID: "tu_1", Name: "Echo", ArgsDelta: string(args)}},
			provider.StreamChunk{FinishReason: provider.FinishToolCalls},
		).
		// What a real HTTP provider returns when the request starts on a canceled ctx.
		QueueError(fmt.Errorf("Post \"http://llm/v1/chat/completions\": %w", context.Canceled))
	a := agent.New(prov, agent.Tools(echoTool{}))
	r := a.Runner(agent.NewConversation())
	defer func() { _ = r.Close() }()

	events, err := r.Send(context.Background(), "go")
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	kinds := drainKinds(t, events, func(ev agent.Event) {
		switch ev.Kind {
		case agent.EventPostToolUse:
			if err := r.Steer(context.Background(), "follow-up"); err != nil {
				t.Errorf("steer: %v", err)
			}
		case agent.EventSteerConsumed:
			// Past the post-dispatch check, right before the next ChatStream.
			if err := r.Cancel("user_stop"); err != nil {
				t.Errorf("cancel: %v", err)
			}
		}
	})

	if slices.Contains(kinds, agent.EventError) {
		t.Fatalf("events = %v, want no EventError", kinds)
	}
	if !slices.Contains(kinds, agent.EventCancelled) || kinds[len(kinds)-1] != agent.EventDone {
		t.Fatalf("events = %v, want EventCancelled ... EventDone", kinds)
	}
}
