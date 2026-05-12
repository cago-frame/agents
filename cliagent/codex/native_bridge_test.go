package codex

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/cago-frame/agents/cliagent/internal/runtime"
	"github.com/cago-frame/agents/provider"
)

func TestToNativeEvent_FullFields(t *testing.T) {
	in := runtime.Event{
		Kind:      runtime.EventPostToolUse,
		SessionID: "sid",
		RunID:     "rid",
		Cwd:       "/tmp",
		Text:      "hello",
		Prompt:    "p",
		Stop:      runtime.StopEndTurn,
		Err:       errors.New("e"),
		Raw:       json.RawMessage(`{"x":1}`),
		Tool: &runtime.ToolEvent{
			ID: "tid", Name: "bash", Input: json.RawMessage(`{"a":1}`),
			Response: json.RawMessage(`{"b":2}`),
			Err:      errors.New("te"), ParentID: "p", Source: runtime.ToolSourceMCP,
		},
		Message: &runtime.Message{
			Role:    runtime.RoleAssistant,
			Origin:  runtime.OriginModel,
			Text:    "ok",
			Persist: true,
		},
		Usage: provider.Usage{TotalTokens: 7},
	}
	out := toNativeEvent(in)
	if out.Kind != EventPostToolUse || out.SessionID != "sid" || out.RunID != "rid" {
		t.Errorf("scalar fields: %+v", out)
	}
	if out.Cwd != "/tmp" {
		t.Errorf("cwd: %+v", out)
	}
	if out.Tool == nil || out.Tool.Name != "bash" || out.Tool.Source != ToolSourceMCP {
		t.Errorf("tool: %+v", out.Tool)
	}
	if out.Message == nil || out.Message.Text != "ok" {
		t.Errorf("message: %+v", out.Message)
	}
	if out.Stop != StopEndTurn {
		t.Errorf("stop = %q", out.Stop)
	}
	if out.Usage.TotalTokens != 7 {
		t.Errorf("usage = %+v", out.Usage)
	}
}

func TestToNativeEvent_NoOptionalFields(t *testing.T) {
	out := toNativeEvent(runtime.Event{Kind: runtime.EventTextDelta, Text: "x"})
	if out.Kind != EventTextDelta || out.Text != "x" {
		t.Errorf("got %+v", out)
	}
	if out.Tool != nil || out.Message != nil {
		t.Errorf("expected nil pointers, got %+v", out)
	}
}

func TestToNativeMessages_NilEmpty(t *testing.T) {
	if got := toNativeMessages(nil); got != nil {
		t.Errorf("got %v, want nil", got)
	}
	got := toNativeMessages([]runtime.Message{
		{Role: runtime.RoleAssistant, Text: "a"},
		{Role: runtime.RoleAssistant, Text: "b", ToolCalls: []runtime.ToolCall{{ID: "t", Name: "x"}}},
	})
	if len(got) != 2 || got[0].Text != "a" || got[1].Kind != MessageKindToolCall {
		t.Errorf("got %+v", got)
	}
}

func TestToNativeResult_NilAndFull(t *testing.T) {
	if got := toNativeResult(nil); got != nil {
		t.Errorf("nil: got %+v", got)
	}
	res := toNativeResult(&runtime.Result{
		Text: "out",
		History: []runtime.Message{
			{Role: runtime.RoleAssistant, Text: "a"},
		},
		Usage: provider.Usage{TotalTokens: 9},
		Stop:  runtime.StopEndTurn,
		State: runtime.State{ThreadID: "t1", Values: map[string]any{"k": "v"}},
	})
	if res.Text != "out" || res.Stop != StopEndTurn || len(res.Messages) != 1 {
		t.Errorf("got %+v", res)
	}
	if res.State.ThreadID != "t1" || res.State.Values["k"] != "v" {
		t.Errorf("state: %+v", res.State)
	}
}

func TestCloneValues(t *testing.T) {
	if got := cloneValues(nil); got != nil {
		t.Errorf("nil → got %v", got)
	}
	src := map[string]any{"a": "1", "b": "2"}
	dst := cloneValues(src)
	if len(dst) != 2 || dst["a"] != "1" {
		t.Errorf("dst = %v", dst)
	}
	dst["a"] = "x"
	if src["a"] != "1" {
		t.Error("cloneValues should produce an independent copy")
	}
}

func TestWrapHookFunc_Translates(t *testing.T) {
	called := false
	fn := wrapHookFunc(func(_ context.Context, in HookInput) (*HookOutput, error) {
		called = true
		if in.Stage != StagePreToolUse {
			t.Errorf("stage = %q", in.Stage)
		}
		return &HookOutput{Decision: DecisionDeny, Reason: "no"}, nil
	})
	got, err := fn(context.Background(), runtime.HookInput{Stage: runtime.StagePreToolUse, ToolName: "bash"})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if !called {
		t.Error("inner not called")
	}
	if got.Decision != runtime.DecisionDeny {
		t.Errorf("got %+v", got)
	}
}

func TestWrapHookFunc_NilOutput(t *testing.T) {
	fn := wrapHookFunc(func(_ context.Context, _ HookInput) (*HookOutput, error) { return nil, nil })
	got, err := fn(context.Background(), runtime.HookInput{})
	if err != nil {
		t.Errorf("err: %v", err)
	}
	if got.Decision != "" || got.AdditionalContext != "" {
		t.Errorf("got = %+v, want zero", got)
	}
}
