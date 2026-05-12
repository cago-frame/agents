package agent_test

import (
	"testing"

	agent "github.com/cago-frame/agents/agent"
)

func TestEventFilter_AnyEvent(t *testing.T) {
	if !agent.AnyEvent.Match(agent.Event{Kind: agent.EventTextDelta}) {
		t.Fatalf("AnyEvent should match everything")
	}
	if !agent.AnyEvent.Match(agent.Event{Kind: agent.EventDone}) {
		t.Fatalf("AnyEvent should match Done")
	}
}

func TestEventFilter_OnlyKinds(t *testing.T) {
	f := agent.OnlyKinds(agent.EventTextDelta, agent.EventTurnEnd)
	if !f.Match(agent.Event{Kind: agent.EventTextDelta}) {
		t.Fatalf("should match text delta")
	}
	if !f.Match(agent.Event{Kind: agent.EventTurnEnd}) {
		t.Fatalf("should match turn end")
	}
	if f.Match(agent.Event{Kind: agent.EventError}) {
		t.Fatalf("should not match error")
	}
}

func TestEventFilter_OnlyTool(t *testing.T) {
	f := agent.OnlyTool("Bash")
	if !f.Match(agent.Event{Kind: agent.EventPreToolUse, Tool: &agent.ToolEvent{Name: "Bash"}}) {
		t.Fatalf("should match Bash pre-tool-use")
	}
	if f.Match(agent.Event{Kind: agent.EventPreToolUse, Tool: &agent.ToolEvent{Name: "Write"}}) {
		t.Fatalf("should not match Write")
	}
	if f.Match(agent.Event{Kind: agent.EventTextDelta}) {
		t.Fatalf("should not match non-tool event")
	}
}
