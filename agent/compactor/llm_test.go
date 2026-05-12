package compactor_test

import (
	"context"
	"testing"

	agent "github.com/cago-frame/agents/agent"
	"github.com/cago-frame/agents/agent/compactor"
	"github.com/cago-frame/agents/provider"
	"github.com/cago-frame/agents/provider/providertest"
)

func TestLLMSummarize_TriggerOnTokens(t *testing.T) {
	t.Parallel()
	prov := providertest.New()

	s := compactor.LLMSummarize(prov, compactor.TriggerOnTokens(100))
	conv := agent.NewConversation()
	if s.ShouldCompact(conv) {
		t.Fatal("ShouldCompact on empty conv = true; want false")
	}

	conv.Append(agent.Message{
		Role:    agent.RoleAssistant,
		Content: []agent.ContentBlock{agent.TextBlock{Text: "x"}},
		Usage:   &provider.Usage{PromptTokens: 150},
	})
	if !s.ShouldCompact(conv) {
		t.Fatal("ShouldCompact with 150 prompt tokens (threshold 100) = false; want true")
	}

	conv2 := agent.NewConversation()
	conv2.Append(agent.Message{
		Role:    agent.RoleAssistant,
		Content: []agent.ContentBlock{agent.TextBlock{Text: "x"}},
		Usage:   &provider.Usage{PromptTokens: 50},
	})
	if s.ShouldCompact(conv2) {
		t.Fatal("ShouldCompact with 50 prompt tokens (threshold 100) = true; want false")
	}
}

func TestLLMSummarize_CompactProducesSummary(t *testing.T) {
	t.Parallel()
	prov := providertest.New().QueueCompletion(
		&provider.CompletionResponse{
			Content:      "summary of the conversation",
			Role:         provider.RoleAssistant,
			FinishReason: provider.FinishStop,
			Usage:        provider.Usage{PromptTokens: 10, CompletionTokens: 5, TotalTokens: 15},
		},
	)
	s := compactor.LLMSummarize(prov, compactor.TriggerOnTokens(1))

	conv := agent.NewConversation()
	conv.Append(agent.Message{Role: agent.RoleUser, Content: []agent.ContentBlock{agent.TextBlock{Text: "hello"}}})
	conv.Append(agent.Message{Role: agent.RoleAssistant, Content: []agent.ContentBlock{agent.TextBlock{Text: "hi"}}})

	summary, err := s.Compact(context.Background(), conv)
	if err != nil {
		t.Fatalf("Compact: %v", err)
	}
	if summary.Role != agent.RoleSystem && summary.Role != agent.RoleAssistant {
		t.Fatalf("summary role = %q; want system or assistant", summary.Role)
	}
	if len(summary.Content) == 0 {
		t.Fatal("summary has no content")
	}
	// The compactor produces a SummaryBlock specifically so UIs can render
	// it with a "summarized" badge instead of mistaking it for ordinary
	// assistant text. The LLM still sees the same string because
	// SummaryBlock.Audience() == ToAll.
	sb, ok := summary.Content[0].(agent.SummaryBlock)
	if !ok {
		t.Fatalf("summary[0] = %T, want SummaryBlock", summary.Content[0])
	}
	if sb.Text != "summary of the conversation" {
		t.Fatalf("summary text = %q", sb.Text)
	}
}
