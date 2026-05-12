package agent_test

import (
	"testing"

	"github.com/cago-frame/agents/agent"
)

func TestRenderForDisplay_DisplayTextOverridesText(t *testing.T) {
	// The whole point of DisplayTextBlock: if it's present, the UI sees the
	// display form rather than the LLM-bound TextBlock.
	m := agent.Message{
		Role: agent.RoleUser,
		Content: []agent.ContentBlock{
			agent.TextBlock{Text: "hello server srv1, what's your status"},
			agent.DisplayTextBlock{Text: "@srv1 status"},
		},
	}
	dm := agent.RenderForDisplay(m)
	if len(dm.Segments) != 1 {
		t.Fatalf("segments = %d, want 1", len(dm.Segments))
	}
	dt, ok := dm.Segments[0].(agent.DisplayText)
	if !ok {
		t.Fatalf("seg[0] = %T, want DisplayText", dm.Segments[0])
	}
	if dt.Text != "@srv1 status" {
		t.Fatalf("UI text = %q, want raw @srv1 form", dt.Text)
	}
	if dt.SourceLLM {
		t.Fatal("SourceLLM should be false when DisplayTextBlock overrides")
	}
}

func TestRenderForDisplay_PlainTextWhenNoDisplay(t *testing.T) {
	m := agent.Message{
		Role: agent.RoleAssistant,
		Content: []agent.ContentBlock{
			agent.TextBlock{Text: "hi"},
		},
	}
	dm := agent.RenderForDisplay(m)
	if len(dm.Segments) != 1 {
		t.Fatalf("segments = %d, want 1", len(dm.Segments))
	}
	dt := dm.Segments[0].(agent.DisplayText)
	if dt.Text != "hi" || !dt.SourceLLM {
		t.Fatalf("expected LLM-sourced 'hi', got %+v", dt)
	}
}

func TestRenderForDisplay_PartialReasonSurfaced(t *testing.T) {
	m := agent.Message{
		Role:          agent.RoleAssistant,
		Content:       []agent.ContentBlock{agent.TextBlock{Text: "half-..."}},
		PartialReason: agent.PartialCancelled,
	}
	dm := agent.RenderForDisplay(m)
	if dm.Partial != agent.PartialCancelled {
		t.Fatalf("Partial = %q, want %q", dm.Partial, agent.PartialCancelled)
	}
}

func TestRenderForDisplay_ToolUsePairedInPlace(t *testing.T) {
	// When a ToolUseBlock and its ToolResultBlock live in the same Message
	// (as the runner produces for some providers), RenderForDisplay folds
	// them into a single DisplayToolCall.
	m := agent.Message{
		Role: agent.RoleAssistant,
		Content: []agent.ContentBlock{
			agent.ToolUseBlock{ID: "tu1", Name: "Bash", Input: map[string]any{"cmd": "ls"}},
			agent.ToolResultBlock{ToolUseID: "tu1", Content: []agent.ContentBlock{agent.TextBlock{Text: "file.txt"}}},
		},
	}
	dm := agent.RenderForDisplay(m)
	if len(dm.Segments) != 1 {
		t.Fatalf("segments = %d, want 1 fused tool call", len(dm.Segments))
	}
	tc := dm.Segments[0].(agent.DisplayToolCall)
	if tc.Status != agent.ToolStatusSuccess {
		t.Fatalf("Status = %v, want Success", tc.Status)
	}
	if len(tc.Result) != 1 {
		t.Fatalf("Result segments = %d, want 1", len(tc.Result))
	}
}

func TestRenderForDisplay_ToolUseStreamingShowsRawArgs(t *testing.T) {
	m := agent.Message{
		Role: agent.RoleAssistant,
		Content: []agent.ContentBlock{
			agent.ToolUseBlock{ID: "tu2", Name: "weather", RawArgs: `{"city":"Beij`, State: agent.ToolUseStreaming},
		},
	}
	dm := agent.RenderForDisplay(m)
	tc := dm.Segments[0].(agent.DisplayToolCall)
	if tc.Status != agent.ToolStatusPending {
		t.Fatalf("Status = %v, want Pending", tc.Status)
	}
	if tc.Args != `{"city":"Beij` {
		t.Fatalf("Args = %v, want raw partial JSON", tc.Args)
	}
}

func TestRenderForDisplay_SkipsThinkingForUIByDefault(t *testing.T) {
	// ThinkingBlock.Audience() omits ToUI; UIs that want it must opt in
	// (e.g. by switching to a custom projection). The default RenderForDisplay
	// honors the Audience contract.
	m := agent.Message{
		Role: agent.RoleAssistant,
		Content: []agent.ContentBlock{
			agent.ThinkingBlock{Text: "let me think..."},
			agent.TextBlock{Text: "answer"},
		},
	}
	dm := agent.RenderForDisplay(m)
	if len(dm.Segments) != 1 {
		t.Fatalf("segments = %d, want 1 (thinking is hidden by default)", len(dm.Segments))
	}
	if _, ok := dm.Segments[0].(agent.DisplayText); !ok {
		t.Fatalf("seg[0] = %T, want DisplayText", dm.Segments[0])
	}
}

func TestRenderForDisplay_NoticeAndRef(t *testing.T) {
	m := agent.Message{
		Role: agent.RoleSystem,
		Content: []agent.ContentBlock{
			agent.NoticeBlock{Level: "info", Text: "history compacted"},
			agent.RefBlock{Kind: "file", ID: "main.go", Label: "main.go"},
		},
	}
	dm := agent.RenderForDisplay(m)
	if len(dm.Segments) != 2 {
		t.Fatalf("segments = %d, want 2", len(dm.Segments))
	}
	if _, ok := dm.Segments[0].(agent.DisplayNotice); !ok {
		t.Fatalf("seg[0] = %T, want DisplayNotice", dm.Segments[0])
	}
	if _, ok := dm.Segments[1].(agent.DisplayRef); !ok {
		t.Fatalf("seg[1] = %T, want DisplayRef", dm.Segments[1])
	}
}

func TestRenderConversationForDisplay_PairsToolResultAcrossMessages(t *testing.T) {
	// The runner produces:
	//   assistant: [ToolUseBlock(tu1)]
	//   tool:      [ToolResultBlock(tu1, ...)]
	// RenderConversationForDisplay should fuse them into the assistant
	// message's DisplayToolCall and drop the standalone tool message.
	msgs := []agent.Message{
		{Role: agent.RoleAssistant, Content: []agent.ContentBlock{
			agent.ToolUseBlock{ID: "tu1", Name: "Bash", Input: map[string]any{"cmd": "ls"}},
		}},
		{Role: agent.RoleTool, Content: []agent.ContentBlock{
			agent.ToolResultBlock{ToolUseID: "tu1", Content: []agent.ContentBlock{agent.TextBlock{Text: "ok"}}},
		}},
	}
	out := agent.RenderConversationForDisplay(msgs)
	if len(out) != 1 {
		t.Fatalf("rendered messages = %d, want 1 (tool message folded in)", len(out))
	}
	tc := out[0].Segments[0].(agent.DisplayToolCall)
	if tc.Status != agent.ToolStatusSuccess {
		t.Fatalf("Status = %v, want Success", tc.Status)
	}
	if len(tc.Result) != 1 {
		t.Fatalf("Result = %d, want 1", len(tc.Result))
	}
}

func TestRenderForDisplay_SummaryBlockSurfacedSeparately(t *testing.T) {
	// SummaryBlock is ToAll — visible to UI but as its own segment so UIs
	// can render it with a "summarized" badge instead of mistaking it for
	// regular assistant text.
	m := agent.Message{
		Role: agent.RoleSystem,
		Content: []agent.ContentBlock{
			agent.SummaryBlock{Text: "earlier: discussed X, Y, Z"},
		},
	}
	dm := agent.RenderForDisplay(m)
	if len(dm.Segments) != 1 {
		t.Fatalf("segments = %d", len(dm.Segments))
	}
	ds, ok := dm.Segments[0].(agent.DisplaySummary)
	if !ok {
		t.Fatalf("seg[0] = %T, want DisplaySummary", dm.Segments[0])
	}
	if ds.Text == "" {
		t.Fatal("DisplaySummary text empty")
	}
}
