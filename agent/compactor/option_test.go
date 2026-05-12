package compactor_test

import (
	"context"
	"testing"

	agent "github.com/cago-frame/agents/agent"
	"github.com/cago-frame/agents/agent/compactor"
	"github.com/cago-frame/agents/provider"
	"github.com/cago-frame/agents/provider/providertest"
)

func TestWithStrategy_TriggersAndEmitsCompacted(t *testing.T) {
	t.Parallel()

	prov := providertest.New().
		QueueStream(
			provider.StreamChunk{ContentDelta: "first answer"},
			provider.StreamChunk{Usage: &provider.Usage{PromptTokens: 200, CompletionTokens: 5, TotalTokens: 205}, FinishReason: provider.FinishStop},
		).
		QueueCompletion(&provider.CompletionResponse{
			Content:      "the user asked a question and i answered",
			Role:         provider.RoleAssistant,
			FinishReason: provider.FinishStop,
			Usage:        provider.Usage{PromptTokens: 50, CompletionTokens: 10, TotalTokens: 60},
		})

	a := agent.New(prov,
		compactor.WithStrategy(compactor.LLMSummarize(prov, compactor.TriggerOnTokens(100))),
	)
	conv := agent.NewConversation()
	r := a.Runner(conv)
	defer func() { _ = r.Close() }()

	events, _ := r.Send(context.Background(), "hello")
	var sawCompacted bool
	for ev := range events {
		if ev.Kind == agent.EventCompacted {
			sawCompacted = true
		}
	}
	if !sawCompacted {
		t.Fatal("EventCompacted not observed; compactor did not fire")
	}
	if conv.Len() != 1 {
		t.Fatalf("conv len after compaction = %d, want 1 (summary only)", conv.Len())
	}
	got := conv.Messages()[0]
	sb, ok := got.Content[0].(agent.SummaryBlock)
	if !ok {
		t.Fatalf("summary block type = %T, want SummaryBlock", got.Content[0])
	}
	if sb.Text != "the user asked a question and i answered" {
		t.Fatalf("summary text mismatch: %q", sb.Text)
	}
}
