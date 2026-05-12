package agent_test

import (
	"testing"

	agent "github.com/cago-frame/agents/agent"
	"github.com/cago-frame/agents/provider"
)

func TestBuildRequest_System(t *testing.T) {
	msgs := []agent.Message{
		{Role: agent.RoleUser, Content: []agent.ContentBlock{agent.TextBlock{Text: "hi"}}},
	}
	req := agent.BuildRequest(agent.RequestSpec{
		System:   "you are helpful",
		Messages: msgs,
		Model:    "gpt-4o-mini",
	})
	if req.Model != "gpt-4o-mini" {
		t.Fatalf("model")
	}
	if len(req.Messages) != 2 {
		t.Fatalf("expected system + user, got %d", len(req.Messages))
	}
	if req.Messages[0].Role != provider.RoleSystem {
		t.Fatalf("first must be system")
	}
}

func TestBuildRequest_ThinkingConfig(t *testing.T) {
	req := agent.BuildRequest(agent.RequestSpec{
		Messages: []agent.Message{
			{Role: agent.RoleUser, Content: []agent.ContentBlock{agent.TextBlock{Text: "hi"}}},
		},
		Thinking: &provider.ThinkingConfig{Effort: provider.ThinkingHigh},
	})
	if req.Thinking == nil || req.Thinking.Effort != provider.ThinkingHigh {
		t.Fatalf("Thinking = %#v, want high", req.Thinking)
	}
}

func TestBuildRequest_DefaultKeepsCancelledAndErroredPartials(t *testing.T) {
	// A canceled partial must reach the next request so the LLM can see
	// what it had already produced and continue from there.
	msgs := []agent.Message{
		{Role: agent.RoleUser, Content: []agent.ContentBlock{agent.TextBlock{Text: "hi"}}},
		{Role: agent.RoleAssistant, Content: []agent.ContentBlock{agent.TextBlock{Text: "Once upon a"}}, PartialReason: agent.PartialCancelled},
	}
	req := agent.BuildRequest(agent.RequestSpec{Messages: msgs})
	if len(req.Messages) != 2 {
		t.Fatalf("canceled partial should be kept, got %d msgs", len(req.Messages))
	}
	if req.Messages[1].Role != provider.RoleAssistant {
		t.Fatalf("second message must be assistant, got %v", req.Messages[1].Role)
	}
	if req.Messages[1].Content != "Once upon a" {
		t.Fatalf("partial text lost: %q", req.Messages[1].Content)
	}

	// Same for errored partials.
	msgs[1].PartialReason = agent.PartialErrored
	req = agent.BuildRequest(agent.RequestSpec{Messages: msgs})
	if len(req.Messages) != 2 {
		t.Fatalf("errored partial should be kept, got %d msgs", len(req.Messages))
	}
}

func TestBuildRequest_DefaultStripsPartialStreaming(t *testing.T) {
	// Streaming-state partials mean a previous runner crashed without
	// finalizing — those must be dropped.
	msgs := []agent.Message{
		{Role: agent.RoleUser, Content: []agent.ContentBlock{agent.TextBlock{Text: "hi"}}},
		{Role: agent.RoleAssistant, Content: []agent.ContentBlock{agent.TextBlock{Text: "uncommit"}}, PartialReason: agent.PartialStreaming},
	}
	req := agent.BuildRequest(agent.RequestSpec{Messages: msgs})
	if len(req.Messages) != 1 {
		t.Fatalf("PartialStreaming should be stripped by default, got %d msgs", len(req.Messages))
	}
}

func TestBuildRequest_StripOverride(t *testing.T) {
	msgs := []agent.Message{
		{Role: agent.RoleUser, Content: []agent.ContentBlock{agent.TextBlock{Text: "hi"}}},
		{Role: agent.RoleAssistant, Content: []agent.ContentBlock{agent.TextBlock{Text: "half"}}, PartialReason: agent.PartialCancelled},
	}
	strip := []agent.PartialState{agent.PartialStreaming, agent.PartialCancelled, agent.PartialErrored}
	req := agent.BuildRequest(agent.RequestSpec{Messages: msgs, StripPartialReasons: &strip})
	if len(req.Messages) != 1 {
		t.Fatalf("explicit override should strip canceled, got %d msgs", len(req.Messages))
	}

	// Non-nil empty slice = keep everything (including PartialStreaming).
	msgs[1].PartialReason = agent.PartialStreaming
	keep := []agent.PartialState{}
	req = agent.BuildRequest(agent.RequestSpec{Messages: msgs, StripPartialReasons: &keep})
	if len(req.Messages) != 2 {
		t.Fatalf("empty strip slice should keep all partials, got %d msgs", len(req.Messages))
	}
}

func TestBuildRequest_ToolCallsRoundTrip(t *testing.T) {
	msgs := []agent.Message{
		{Role: agent.RoleAssistant, Content: []agent.ContentBlock{
			agent.ToolUseBlock{ID: "tu1", Name: "Bash", Input: map[string]any{"cmd": "ls"}},
		}},
		{Role: agent.RoleTool, Content: []agent.ContentBlock{
			agent.ToolResultBlock{ToolUseID: "tu1", Content: []agent.ContentBlock{agent.TextBlock{Text: "out"}}},
		}},
	}
	req := agent.BuildRequest(agent.RequestSpec{Messages: msgs})
	if len(req.Messages) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(req.Messages))
	}
	if len(req.Messages[0].ToolCalls) != 1 {
		t.Fatalf("assistant msg should have 1 tool call")
	}
	if req.Messages[1].Role != provider.RoleTool {
		t.Fatalf("second must be role=tool")
	}
}

func TestBuildRequest_ImageBlockPopulatesMultiContent(t *testing.T) {
	conv := []agent.Message{
		{Role: agent.RoleUser, Content: []agent.ContentBlock{
			agent.TextBlock{Text: "what is this?"},
			agent.ImageBlock{
				MediaType: "image/png",
				Source:    agent.BlobSource{Inline: []byte{0x89, 0x50, 0x4e, 0x47}},
			},
		}},
	}
	req := agent.BuildRequest(agent.RequestSpec{Messages: conv})
	if len(req.Messages) != 1 {
		t.Fatalf("messages len = %d", len(req.Messages))
	}
	m := req.Messages[0]
	if m.Content != "" {
		t.Fatalf("Content should be empty when MultiContent populated; got %q", m.Content)
	}
	if len(m.MultiContent) != 2 {
		t.Fatalf("MultiContent len = %d, want 2", len(m.MultiContent))
	}
	if m.MultiContent[0].Type != provider.MessagePartText || m.MultiContent[0].Text != "what is this?" {
		t.Fatalf("part 0 = %+v", m.MultiContent[0])
	}
	if m.MultiContent[1].Type != provider.MessagePartImage || m.MultiContent[1].Image == nil {
		t.Fatalf("part 1 = %+v", m.MultiContent[1])
	}
	if m.MultiContent[1].Image.MediaType != "image/png" {
		t.Fatalf("image media type = %q", m.MultiContent[1].Image.MediaType)
	}
	if string(m.MultiContent[1].Image.Inline) != string([]byte{0x89, 0x50, 0x4e, 0x47}) {
		t.Fatalf("image inline bytes mismatch")
	}
}

func TestBuildRequest_NoImageBlockKeepsContent(t *testing.T) {
	conv := []agent.Message{
		{Role: agent.RoleUser, Content: []agent.ContentBlock{agent.TextBlock{Text: "plain"}}},
	}
	req := agent.BuildRequest(agent.RequestSpec{Messages: conv})
	if req.Messages[0].Content != "plain" {
		t.Fatalf("Content = %q, want plain", req.Messages[0].Content)
	}
	if len(req.Messages[0].MultiContent) != 0 {
		t.Fatalf("MultiContent should be empty for text-only; got %d parts", len(req.Messages[0].MultiContent))
	}
}

func TestBuildRequest_StripsNonLLMAudienceBlocks(t *testing.T) {
	// Any block whose Audience() omits blocks.ToLLM disappears from the
	// LLM-facing request. DisplayTextBlock is the canonical example;
	// RefBlock / NoticeBlock follow the same rule. Adding a new UI-only
	// block type costs nothing here.
	msgs := []agent.Message{
		{Role: agent.RoleUser, Content: []agent.ContentBlock{
			agent.TextBlock{Text: "hello @srv1"},
			agent.DisplayTextBlock{Text: "@srv1 (raw form for UI only)"},
			agent.RefBlock{Kind: "file", ID: "main.go"},
			agent.NoticeBlock{Level: "info", Text: "auto-compacted earlier"},
		}},
	}
	req := agent.BuildRequest(agent.RequestSpec{Messages: msgs})
	if len(req.Messages) != 1 {
		t.Fatalf("got %d msgs, want 1", len(req.Messages))
	}
	got := req.Messages[0].Content
	if got != "hello @srv1" {
		t.Fatalf("Content = %q, want %q (non-LLM blocks should be stripped)", got, "hello @srv1")
	}
}

func TestBuildRequest_SummaryBlockReplacesText(t *testing.T) {
	// SummaryBlock is ToAll — the LLM sees the summary text just like a
	// regular TextBlock, so compaction's output round-trips through
	// BuildRequest without any special-case in BuildRequest itself.
	msgs := []agent.Message{
		{Role: agent.RoleSystem, Content: []agent.ContentBlock{
			agent.SummaryBlock{Text: "prior turns: user asked X, assistant did Y"},
		}},
	}
	req := agent.BuildRequest(agent.RequestSpec{Messages: msgs})
	if len(req.Messages) != 1 {
		t.Fatalf("got %d msgs, want 1", len(req.Messages))
	}
	if got := req.Messages[0].Content; got != "prior turns: user asked X, assistant did Y" {
		t.Fatalf("Content = %q, want summary text", got)
	}
}

func TestBuildRequest_DropsStreamingToolUse(t *testing.T) {
	// A ToolUseBlock with State != ToolUseReady has no usable Input; sending
	// it to the LLM as an unbalanced tool_call would break the request.
	msgs := []agent.Message{
		{Role: agent.RoleAssistant, Content: []agent.ContentBlock{
			agent.TextBlock{Text: "calling..."},
			agent.ToolUseBlock{ID: "tu_x", Name: "Bash", RawArgs: `{"partial`, State: agent.ToolUseStreaming},
		}},
	}
	req := agent.BuildRequest(agent.RequestSpec{Messages: msgs})
	if len(req.Messages[0].ToolCalls) != 0 {
		t.Fatalf("streaming ToolUseBlock should be stripped from the LLM request, got %d tool_calls", len(req.Messages[0].ToolCalls))
	}
	if req.Messages[0].Content != "calling..." {
		t.Fatalf("text portion should survive, got %q", req.Messages[0].Content)
	}
}
