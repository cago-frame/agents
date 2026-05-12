package agent_test

import (
	"testing"

	"github.com/cago-frame/agents/agent"
	"github.com/cago-frame/agents/agent/blocks"
)

// These tests pin the cross-projection invariants that motivated the
// audience refactor. If a future change drifts one of them, the failure
// names the contract directly instead of forcing a debugger walk through
// BuildRequest / RenderForDisplay to figure out what regressed.

func TestProjections_UIOnlyBlockNeverReachesLLM(t *testing.T) {
	// Any block whose Audience excludes blocks.ToLLM must disappear from
	// BuildRequest's output, regardless of which message contains it.
	msgs := []agent.Message{
		{Role: agent.RoleUser, Content: []agent.ContentBlock{
			agent.TextBlock{Text: "expanded body"},
			agent.DisplayTextBlock{Text: "@you raw"},
			agent.RefBlock{Kind: "file", ID: "x.go"},
			agent.NoticeBlock{Level: "info", Text: "n/a"},
		}},
	}
	req := agent.BuildRequest(agent.RequestSpec{Messages: msgs})
	if got := req.Messages[0].Content; got != "expanded body" {
		t.Fatalf("LLM saw UI-only data; req content = %q", got)
	}
}

func TestProjections_LLMOnlyBlockHiddenFromUI(t *testing.T) {
	// ThinkingBlock excludes ToUI by default. RenderForDisplay must drop it
	// so UIs don't accidentally render internal chain-of-thought.
	m := agent.Message{
		Role: agent.RoleAssistant,
		Content: []agent.ContentBlock{
			agent.ThinkingBlock{Text: "internal reasoning"},
			agent.TextBlock{Text: "answer"},
		},
	}
	dm := agent.RenderForDisplay(m)
	for _, seg := range dm.Segments {
		if _, ok := seg.(agent.DisplayThinking); ok {
			t.Fatal("ThinkingBlock should be hidden from default UI projection")
		}
	}
}

func TestProjections_BlockEncodingRoundTripsEveryBuiltInBlock(t *testing.T) {
	// blocks.Encode/Decode remains available as a typed JSON helper even
	// though the agent package no longer owns a Store abstraction.
	all := []agent.ContentBlock{
		agent.TextBlock{Text: "t"},
		agent.DisplayTextBlock{Text: "d"},
		agent.ThinkingBlock{Text: "k"},
		agent.RefBlock{Kind: "x", ID: "y"},
		agent.NoticeBlock{Level: "info", Text: "n"},
		agent.SummaryBlock{Text: "s"},
		agent.ToolUseBlock{ID: "tu", Name: "Bash", Input: map[string]any{"a": 1}},
	}
	for _, b := range all {
		sb, err := blocks.Encode(b)
		if err != nil {
			t.Errorf("Encode(%s): %v", b.Type(), err)
			continue
		}
		out, err := blocks.Decode(sb)
		if err != nil {
			t.Errorf("Decode(%s): %v", b.Type(), err)
			continue
		}
		if out.Type() != b.Type() {
			t.Errorf("round-trip changed type: %s → %s", b.Type(), out.Type())
		}
	}
}

func TestProjections_AudienceMaskCannotBeZero(t *testing.T) {
	// A block with zero Audience would silently disappear from every
	// projection — almost certainly a programmer error. Pin this so new
	// block types declare their consumers explicitly.
	all := []agent.ContentBlock{
		agent.TextBlock{},
		agent.ImageBlock{},
		agent.ToolUseBlock{},
		agent.ToolResultBlock{},
		agent.ThinkingBlock{},
		agent.DisplayTextBlock{},
		agent.RefBlock{},
		agent.NoticeBlock{},
		agent.SummaryBlock{},
	}
	for _, b := range all {
		if b.Audience() == 0 {
			t.Errorf("%s has zero Audience() — would be invisible to all consumers", b.Type())
		}
	}
}
