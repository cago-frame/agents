package blocks_test

import (
	"testing"

	"github.com/cago-frame/agents/agent/blocks"
)

func TestAudience_Defaults(t *testing.T) {
	cases := []struct {
		block blocks.ContentBlock
		want  blocks.AudienceMask
	}{
		{blocks.TextBlock{Text: "x"}, blocks.ToAll},
		{blocks.ImageBlock{MediaType: "image/png"}, blocks.ToAll},
		{blocks.ToolUseBlock{ID: "tu", Name: "Bash"}, blocks.ToAll},
		{blocks.ToolResultBlock{ToolUseID: "tu"}, blocks.ToAll},
		{blocks.SummaryBlock{Text: "s"}, blocks.ToAll},
		{blocks.ThinkingBlock{Text: "t"}, blocks.ToLLM},
		{blocks.DisplayTextBlock{Text: "raw"}, blocks.ToUI},
		{blocks.RefBlock{Kind: "file", ID: "main.go"}, blocks.ToUI},
		{blocks.NoticeBlock{Level: "info", Text: "compacted"}, blocks.ToUI},
	}
	for _, c := range cases {
		if got := c.block.Audience(); got != c.want {
			t.Errorf("%s.Audience() = %b, want %b", c.block.Type(), got, c.want)
		}
	}
}

func TestAudience_DisplayBlockExcludedFromLLM(t *testing.T) {
	a := blocks.DisplayTextBlock{}.Audience()
	if a.Has(blocks.ToLLM) {
		t.Fatal("DisplayTextBlock must not target the LLM")
	}
	if !a.Has(blocks.ToUI) {
		t.Fatal("DisplayTextBlock must target the UI")
	}
}

func TestAudience_ThinkingNotForUIByDefault(t *testing.T) {
	if (blocks.ThinkingBlock{}).Audience().Has(blocks.ToUI) {
		t.Fatal("ThinkingBlock default audience should exclude ToUI; UIs opt in explicitly")
	}
}

func TestToolUseState_ZeroIsReady(t *testing.T) {
	var s blocks.ToolUseState
	if s != blocks.ToolUseReady {
		t.Fatalf("zero ToolUseState should be ToolUseReady, got %v", s)
	}
}

func TestTypeNames_Stable(t *testing.T) {
	cases := []struct {
		block blocks.ContentBlock
		want  string
	}{
		{blocks.TextBlock{}, "text"},
		{blocks.ImageBlock{}, "image"},
		{blocks.ToolUseBlock{}, "tool_use"},
		{blocks.ToolResultBlock{}, "tool_result"},
		{blocks.ThinkingBlock{}, "thinking"},
		{blocks.DisplayTextBlock{}, "display_text"},
		{blocks.RefBlock{}, "ref"},
		{blocks.NoticeBlock{}, "notice"},
		{blocks.SummaryBlock{}, "summary"},
	}
	for _, c := range cases {
		if got := c.block.Type(); got != c.want {
			t.Errorf("Type() = %q, want %q", got, c.want)
		}
	}
}
