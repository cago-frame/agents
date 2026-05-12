package coding_test

import (
	"context"
	"strings"
	"testing"

	"github.com/cago-frame/agents/agent"
	"github.com/cago-frame/agents/app/coding"
	"github.com/cago-frame/agents/provider"
	"github.com/cago-frame/agents/provider/providertest"
	"github.com/cago-frame/agents/tool/subagent"
	"github.com/cago-frame/agents/tool/websearch"
)

func TestSystem_New_Defaults(t *testing.T) {
	mock := providertest.New()
	sys, err := coding.New(t.Context(), mock, ".")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = sys.Close(context.Background()) })

	if sys.Agent() == nil {
		t.Fatal("Agent() returned nil")
	}
}

func TestSystem_Close_Idempotent(t *testing.T) {
	mock := providertest.New()
	sys, err := coding.New(t.Context(), mock, ".")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := sys.Close(context.Background()); err != nil {
		t.Fatalf("Close 1: %v", err)
	}
	if err := sys.Close(context.Background()); err != nil {
		t.Fatalf("Close 2 (idempotent): %v", err)
	}
}

// Parent dispatches type=general-purpose; with WithoutGeneralPurpose() the dispatch tool
// should return an ErrorResult-shaped ToolResultBlock whose text contains "unknown type".
func TestSystem_WithoutGeneralPurpose(t *testing.T) {
	mock := providertest.New()
	mock.QueueStream(
		provider.StreamChunk{ToolCallDelta: &provider.ToolCallDelta{Index: 0, ID: "g1", Name: "dispatch_subagent"}},
		provider.StreamChunk{ToolCallDelta: &provider.ToolCallDelta{Index: 0, ArgsDelta: `{"type":"general-purpose","prompt":"do it"}`}},
		provider.StreamChunk{FinishReason: provider.FinishToolCalls},
	)
	mock.QueueStream(
		provider.StreamChunk{ContentDelta: "ack"},
		provider.StreamChunk{FinishReason: provider.FinishStop},
	)

	sys, err := coding.New(t.Context(), mock, ".", coding.WithoutGeneralPurpose())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = sys.Close(context.Background()) })

	a := sys.Agent()
	conv := agent.NewConversation()
	r := a.Runner(conv)
	defer func() { _ = r.Close() }()

	events, err := r.Send(context.Background(), "go")
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	var toolResult string
	var isError bool
	for ev := range events {
		if ev.Kind == agent.EventPostToolUse && ev.Tool != nil && ev.Tool.ToolUseID == "g1" {
			if ev.Tool.Output != nil {
				isError = ev.Tool.Output.IsError
				if len(ev.Tool.Output.Content) > 0 {
					if tb, ok := ev.Tool.Output.Content[0].(agent.TextBlock); ok {
						toolResult = tb.Text
					}
				}
			}
		}
	}
	if !isError {
		t.Errorf("expected IsError=true for unknown subagent type, got false")
	}
	if !strings.Contains(toolResult, "unknown type") {
		t.Errorf("toolResult = %q, want contains \"unknown type\"", toolResult)
	}
}

// Parent dispatch -> explore returns -> parent summarizes. All agents share one mock
// provider; queue order is execution order:
//  1. parent T1 toolcall (dispatch explore)
//  2. child (explore) returns "found at greetings.go:12"
//  3. parent T2 "ok"
func TestSystem_DispatchToExplore_EndToEnd(t *testing.T) {
	mock := providertest.New()
	mock.QueueStream(
		provider.StreamChunk{ToolCallDelta: &provider.ToolCallDelta{Index: 0, ID: "e1", Name: "dispatch_subagent"}},
		provider.StreamChunk{ToolCallDelta: &provider.ToolCallDelta{Index: 0, ArgsDelta: `{"type":"explore","prompt":"find foo"}`}},
		provider.StreamChunk{FinishReason: provider.FinishToolCalls},
	)
	mock.QueueStream(
		provider.StreamChunk{ContentDelta: "found at greetings.go:12"},
		provider.StreamChunk{FinishReason: provider.FinishStop},
	)
	mock.QueueStream(
		provider.StreamChunk{ContentDelta: "ok"},
		provider.StreamChunk{FinishReason: provider.FinishStop},
	)

	sys, err := coding.New(t.Context(), mock, ".",
		coding.WithModel("p"),
		coding.WithoutGeneralPurpose(), // simplify queue: drop GP default child
	)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = sys.Close(context.Background()) })

	a := sys.Agent()
	conv := agent.NewConversation()
	r := a.Runner(conv)
	defer func() { _ = r.Close() }()

	events, err := r.Send(context.Background(), "go")
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	var toolResult string
	for ev := range events {
		if ev.Kind == agent.EventPostToolUse && ev.Tool != nil && ev.Tool.ToolUseID == "e1" {
			if ev.Tool.Output != nil && len(ev.Tool.Output.Content) > 0 {
				if tb, ok := ev.Tool.Output.Content[0].(agent.TextBlock); ok {
					toolResult = tb.Text
				}
			}
		}
	}
	if !strings.Contains(toolResult, "greetings.go:12") {
		t.Errorf("toolResult = %q, want contains greetings.go:12", toolResult)
	}
}

func TestSystem_WithSearch_NewSucceeds(t *testing.T) {
	mock := providertest.New()
	mp := &websearch.MockProvider{}
	sys, err := coding.New(t.Context(), mock, ".",
		coding.WithSearch(mp),
		coding.WithoutGeneralPurpose(),
	)
	if err != nil {
		t.Fatalf("New with WithSearch: %v", err)
	}
	t.Cleanup(func() { _ = sys.Close(context.Background()) })
	if sys.Agent() == nil {
		t.Fatal("Agent nil")
	}
}

func TestSystem_WithExtraSubagents_AddsNewType(t *testing.T) {
	mock := providertest.New()
	customMock := providertest.New()

	custom := subagent.Entry{
		Type:        "custom",
		Description: "custom subagent",
		Agent:       agent.New(customMock),
	}

	sys, err := coding.New(t.Context(), mock, ".",
		coding.WithExtraSubagents(custom),
		coding.WithoutGeneralPurpose(),
	)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = sys.Close(context.Background()) })

	mock.QueueStream(
		provider.StreamChunk{ToolCallDelta: &provider.ToolCallDelta{Index: 0, ID: "c1", Name: "dispatch_subagent"}},
		provider.StreamChunk{ToolCallDelta: &provider.ToolCallDelta{Index: 0, ArgsDelta: `{"type":"custom","prompt":"do it"}`}},
		provider.StreamChunk{FinishReason: provider.FinishToolCalls},
	)
	mock.QueueStream(
		provider.StreamChunk{ContentDelta: "ok"},
		provider.StreamChunk{FinishReason: provider.FinishStop},
	)
	customMock.QueueStream(
		provider.StreamChunk{ContentDelta: "custom result"},
		provider.StreamChunk{FinishReason: provider.FinishStop},
	)

	a := sys.Agent()
	conv := agent.NewConversation()
	r := a.Runner(conv)
	defer func() { _ = r.Close() }()

	events, err := r.Send(context.Background(), "go")
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	var toolResult string
	for ev := range events {
		if ev.Kind == agent.EventPostToolUse && ev.Tool != nil && ev.Tool.ToolUseID == "c1" {
			if ev.Tool.Output != nil && len(ev.Tool.Output.Content) > 0 {
				if tb, ok := ev.Tool.Output.Content[0].(agent.TextBlock); ok {
					toolResult = tb.Text
				}
			}
		}
	}
	if !strings.Contains(toolResult, "custom result") {
		t.Errorf("toolResult = %q, want contains custom result", toolResult)
	}
}

func TestSystem_WithExtraSubagents_ReplacesSameType(t *testing.T) {
	mock := providertest.New()
	replacementMock := providertest.New()

	replacement := subagent.Entry{
		Type:        "explore",
		Description: "replacement explore",
		Agent:       agent.New(replacementMock),
	}

	sys, err := coding.New(t.Context(), mock, ".",
		coding.WithExtraSubagents(replacement),
		coding.WithoutGeneralPurpose(),
	)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = sys.Close(context.Background()) })

	mock.QueueStream(
		provider.StreamChunk{ToolCallDelta: &provider.ToolCallDelta{Index: 0, ID: "r1", Name: "dispatch_subagent"}},
		provider.StreamChunk{ToolCallDelta: &provider.ToolCallDelta{Index: 0, ArgsDelta: `{"type":"explore","prompt":"go"}`}},
		provider.StreamChunk{FinishReason: provider.FinishToolCalls},
	)
	mock.QueueStream(
		provider.StreamChunk{ContentDelta: "done"},
		provider.StreamChunk{FinishReason: provider.FinishStop},
	)
	replacementMock.QueueStream(
		provider.StreamChunk{ContentDelta: "replacement result"},
		provider.StreamChunk{FinishReason: provider.FinishStop},
	)

	a := sys.Agent()
	conv := agent.NewConversation()
	r := a.Runner(conv)
	defer func() { _ = r.Close() }()

	events, err := r.Send(context.Background(), "x")
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	var toolResult string
	for ev := range events {
		if ev.Kind == agent.EventPostToolUse && ev.Tool != nil && ev.Tool.ToolUseID == "r1" {
			if ev.Tool.Output != nil && len(ev.Tool.Output.Content) > 0 {
				if tb, ok := ev.Tool.Output.Content[0].(agent.TextBlock); ok {
					toolResult = tb.Text
				}
			}
		}
	}
	if !strings.Contains(toolResult, "replacement result") {
		t.Errorf("toolResult = %q, want contains replacement result", toolResult)
	}
}
