package agent_test

import (
	"testing"
	"time"

	agent "github.com/cago-frame/agents/agent"
)

func TestMessage_Construction(t *testing.T) {
	msg := agent.Message{
		Role: agent.RoleUser,
		Content: []agent.ContentBlock{
			agent.TextBlock{Text: "hello"},
		},
		CreatedAt: time.Unix(1700000000, 0),
	}
	if msg.Role != agent.RoleUser {
		t.Fatalf("role mismatch")
	}
	if len(msg.Content) != 1 {
		t.Fatalf("content len mismatch")
	}
	if msg.PartialReason != agent.PartialNone {
		t.Fatalf("PartialReason should default to PartialNone")
	}
}

func TestContentBlock_TypeNames(t *testing.T) {
	cases := []struct {
		block agent.ContentBlock
		want  string
	}{
		{agent.TextBlock{Text: "x"}, "text"},
		{agent.ImageBlock{MediaType: "image/png"}, "image"},
		{agent.ToolUseBlock{ID: "tu_1", Name: "Bash"}, "tool_use"},
		{agent.ToolResultBlock{ToolUseID: "tu_1"}, "tool_result"},
		{agent.ThinkingBlock{Text: "..."}, "thinking"},
		{agent.DisplayTextBlock{Text: "@u"}, "display_text"},
		{agent.SummaryBlock{Text: "s"}, "summary"},
	}
	for _, c := range cases {
		if got := c.block.Type(); got != c.want {
			t.Fatalf("Type = %q, want %q", got, c.want)
		}
	}
}

func TestPartialReason_Constants(t *testing.T) {
	if agent.PartialNone != "" {
		t.Fatalf("PartialNone should be empty string")
	}
	expectedSet := []agent.PartialState{
		agent.PartialStreaming,
		agent.PartialCancelled,
		agent.PartialErrored,
		agent.PartialTokenLimit,
		agent.PartialTimeout,
	}
	seen := map[agent.PartialState]bool{}
	for _, p := range expectedSet {
		if p == "" {
			t.Fatalf("non-PartialNone reason has empty value")
		}
		if seen[p] {
			t.Fatalf("duplicate PartialState value %q", p)
		}
		seen[p] = true
	}
}
