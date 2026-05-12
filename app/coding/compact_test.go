package coding

import (
	"context"
	"strings"
	"testing"

	agent "github.com/cago-frame/agents/agent"
	"github.com/cago-frame/agents/provider"
	"github.com/cago-frame/agents/provider/providertest"
)

func TestCompact_ManualHappyPath(t *testing.T) {
	mock := providertest.New().Queue(&provider.CompletionResponse{
		Role:    provider.RoleAssistant,
		Content: "# Goal\nfix bug\n# Progress\nread file.go\n# Next Steps\nedit",
	})
	cmp := newCompactor(mock, "mock-model", nil, 0)
	conv := buildConv(20)
	res, err := cmp.compact(context.Background(), conv, CompactOptions{KeepRecent: 6, Trigger: "manual"})
	if err != nil {
		t.Fatalf("compact: %v", err)
	}
	if res == nil {
		t.Fatalf("nil result")
	}
	if res.Before != 20 {
		t.Errorf("Before=%d", res.Before)
	}
	if res.After != 1 { // Truncate(0) + one summary
		t.Errorf("After=%d want 1", res.After)
	}
	msgs := conv.Messages()
	if !isCompactionSummary(msgs[0]) {
		t.Fatalf("first message is not a compaction summary: %+v", msgs[0])
	}
	if !strings.Contains(textOf(msgs[0]), "# Goal") {
		t.Errorf("summary text missing # Goal: %q", textOf(msgs[0]))
	}
}

func TestCompact_DoesNotSplitToolPair(t *testing.T) {
	conv := agent.NewConversation()
	conv.Append(agent.Message{Role: agent.RoleUser, Content: text("1")})
	conv.Append(agent.Message{Role: agent.RoleAssistant, Content: text("2")})
	conv.Append(agent.Message{Role: agent.RoleAssistant, Content: []agent.ContentBlock{
		agent.ToolUseBlock{ID: "tu", Name: "noop"},
	}})
	conv.Append(agent.Message{Role: agent.RoleTool, Content: []agent.ContentBlock{
		agent.ToolResultBlock{ToolUseID: "tu", Content: text("ok")},
	}})
	conv.Append(agent.Message{Role: agent.RoleAssistant, Content: text("3")})
	conv.Append(agent.Message{Role: agent.RoleUser, Content: text("4")})
	conv.Append(agent.Message{Role: agent.RoleAssistant, Content: text("5")})

	mock := providertest.New().Queue(&provider.CompletionResponse{
		Role:    provider.RoleAssistant,
		Content: "summary",
	})
	cmp := newCompactor(mock, "m", nil, 0)
	res, err := cmp.compact(context.Background(), conv, CompactOptions{KeepRecent: 4})
	if err != nil {
		t.Fatalf("compact: %v", err)
	}
	if res == nil {
		t.Fatal("nil result")
	}
	if res.Before != 7 {
		t.Errorf("Before=%d want 7", res.Before)
	}
	// After: Truncate(0) + summary = 1.
	if res.After != 1 {
		t.Errorf("After=%d want 1", res.After)
	}
	// Sanity: the summary's body should mention the tool-call/result pair was preserved (i.e. older
	// included the assistant message that owns the tool call). We only check no panic + non-empty.
	if textOf(conv.Messages()[0]) == "" {
		t.Fatal("empty summary text")
	}
}

func TestCompact_TooShortHistory_NoOp(t *testing.T) {
	conv := buildConv(3)
	mock := providertest.New()
	cmp := newCompactor(mock, "m", nil, 0)
	res, err := cmp.compact(context.Background(), conv, CompactOptions{KeepRecent: 6})
	if err != nil {
		t.Fatalf("compact: %v", err)
	}
	if res != nil {
		t.Fatalf("expected nil result for too-short history, got %+v", res)
	}
	if conv.Len() != 3 {
		t.Fatalf("conversation mutated: %d", conv.Len())
	}
}

func TestCompact_ShouldCompact_ThresholdGate(t *testing.T) {
	s := newCompactStrategy(providertest.New(), "m", nil, 100)
	// Build an 8-message conv so the post-KeepRecent peel-back gate (keepRecent=6 default,
	// startKeep = 8 - 6 = 2 > 0) doesn't suppress the token-hit signal. Place the
	// usage marker on the first (older-portion) message so iteration finds it.
	conv := agent.NewConversation()
	conv.Append(agent.Message{Role: agent.RoleUser, Content: text("u0"), Usage: &provider.Usage{PromptTokens: 50}})
	for i := 1; i < 8; i++ {
		role := agent.RoleUser
		if i%2 == 1 {
			role = agent.RoleAssistant
		}
		conv.Append(agent.Message{Role: role, Content: text("m" + intToA(i))})
	}
	if s.ShouldCompact(conv) {
		t.Fatalf("ShouldCompact true for usage below threshold")
	}
	// Bump the older message's usage above threshold by appending a fresh older marker.
	// Easiest: rebuild a fresh 8-message conv with the high-usage marker in the older slice.
	conv = agent.NewConversation()
	conv.Append(agent.Message{Role: agent.RoleUser, Content: text("u0"), Usage: &provider.Usage{PromptTokens: 200}})
	for i := 1; i < 8; i++ {
		role := agent.RoleUser
		if i%2 == 1 {
			role = agent.RoleAssistant
		}
		conv.Append(agent.Message{Role: role, Content: text("m" + intToA(i))})
	}
	if !s.ShouldCompact(conv) {
		t.Fatalf("ShouldCompact false for usage above threshold")
	}
}

func TestCompact_ShouldCompact_ZeroThreshold_NeverFires(t *testing.T) {
	s := newCompactStrategy(providertest.New(), "m", nil, 0)
	conv := agent.NewConversation()
	conv.Append(agent.Message{Role: agent.RoleAssistant, Content: text("hi"), Usage: &provider.Usage{PromptTokens: 999_999}})
	if s.ShouldCompact(conv) {
		t.Fatalf("ShouldCompact must never fire when threshold=0")
	}
}

func text(s string) []agent.ContentBlock {
	return []agent.ContentBlock{agent.TextBlock{Text: s}}
}

func buildConv(n int) *agent.Conversation {
	conv := agent.NewConversation()
	for i := 0; i < n; i++ {
		role := agent.RoleUser
		if i%2 == 1 {
			role = agent.RoleAssistant
		}
		conv.Append(agent.Message{Role: role, Content: text("msg " + intToA(i))})
	}
	return conv
}

func intToA(n int) string { return string(rune('a' + n%26)) }
