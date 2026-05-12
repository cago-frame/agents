package agent_test

import (
	"testing"

	agent "github.com/cago-frame/agents/agent"
)

func TestEventKind_Distinct(t *testing.T) {
	seen := map[agent.EventKind]bool{}
	for _, k := range []agent.EventKind{
		agent.EventTextDelta, agent.EventThinkingDelta, agent.EventMessageEnd,
		agent.EventPreToolUse, agent.EventPostToolUse, agent.EventToolDelta,
		agent.EventTurnEnd, agent.EventError, agent.EventCancelled,
		agent.EventCompacted, agent.EventDone,
	} {
		if seen[k] {
			t.Fatalf("duplicate EventKind value")
		}
		seen[k] = true
	}
}

func TestStopReason_Distinct(t *testing.T) {
	seen := map[agent.StopReason]bool{}
	for _, r := range []agent.StopReason{
		agent.StopEndTurn, agent.StopMaxSteps, agent.StopHook, agent.StopError, agent.StopCancelled,
	} {
		if seen[r] {
			t.Fatalf("duplicate StopReason")
		}
		seen[r] = true
	}
}

func TestEvent_ToolField(t *testing.T) {
	ev := agent.Event{
		Kind:   agent.EventPreToolUse,
		TurnID: "t1",
		Tool:   &agent.ToolEvent{Name: "Bash", ToolUseID: "tu1", Input: map[string]any{"cmd": "ls"}},
	}
	if ev.Tool.Name != "Bash" {
		t.Fatalf("tool name mismatch")
	}
}

func TestEventKind_String(t *testing.T) {
	cases := []struct {
		kind agent.EventKind
		want string
	}{
		{agent.EventTextDelta, "text_delta"},
		{agent.EventThinkingDelta, "thinking_delta"},
		{agent.EventMessageEnd, "message_end"},
		{agent.EventPreToolUse, "pre_tool_use"},
		{agent.EventPostToolUse, "post_tool_use"},
		{agent.EventToolDelta, "tool_delta"},
		{agent.EventTurnEnd, "turn_end"},
		{agent.EventError, "error"},
		{agent.EventCancelled, "canceled"},
		{agent.EventCompacted, "compacted"},
		{agent.EventDone, "done"},
	}
	for _, tc := range cases {
		if got := tc.kind.String(); got != tc.want {
			t.Errorf("EventKind(%d).String() = %q, want %q", tc.kind, got, tc.want)
		}
	}

	if got := agent.EventKind(999).String(); got != "unknown" {
		t.Errorf("unknown EventKind.String() = %q, want unknown", got)
	}
}
