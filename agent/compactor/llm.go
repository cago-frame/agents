package compactor

import (
	"context"
	"strings"

	agent "github.com/cago-frame/agents/agent"
	"github.com/cago-frame/agents/provider"
)

// DefaultSummaryPrompt is used when WithSummaryPrompt is not set.
const DefaultSummaryPrompt = "Summarize the conversation above in 3-5 sentences. " +
	"Preserve any user-stated goals, constraints, and decisions. Output only the summary text."

type llmConfig struct {
	tokenThreshold int
	model          string
	summaryPrompt  string
}

// LLMOption configures LLMSummarize.
type LLMOption func(*llmConfig)

// TriggerOnTokens fires compaction when the most recent assistant message's
// Usage.PromptTokens exceeds threshold. If no message has Usage set, never fires.
func TriggerOnTokens(threshold int) LLMOption {
	return func(c *llmConfig) { c.tokenThreshold = threshold }
}

// WithModel overrides the model name used for summarization. Default uses the
// provider's default model.
func WithModel(name string) LLMOption {
	return func(c *llmConfig) { c.model = name }
}

// WithSummaryPrompt overrides the user-facing prompt sent to the summarizer LLM.
func WithSummaryPrompt(p string) LLMOption {
	return func(c *llmConfig) { c.summaryPrompt = p }
}

type llmStrategy struct {
	prov provider.Provider
	cfg  llmConfig
}

// LLMSummarize returns a Strategy that asks an LLM to compress the conversation.
func LLMSummarize(prov provider.Provider, opts ...LLMOption) Strategy {
	cfg := llmConfig{summaryPrompt: DefaultSummaryPrompt}
	for _, o := range opts {
		o(&cfg)
	}
	return &llmStrategy{prov: prov, cfg: cfg}
}

func (s *llmStrategy) ShouldCompact(conv *agent.Conversation) bool {
	if s.cfg.tokenThreshold <= 0 {
		return false
	}
	msgs := conv.Messages()
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Usage != nil && msgs[i].Usage.PromptTokens >= s.cfg.tokenThreshold {
			return true
		}
	}
	return false
}

func (s *llmStrategy) Compact(ctx context.Context, conv *agent.Conversation) (agent.Message, error) {
	transcript := renderTranscript(conv.Messages())
	req := &provider.CompletionRequest{
		Model: s.cfg.model,
		Messages: []provider.Message{
			{Role: provider.RoleSystem, Content: "You are a conversation summarizer."},
			{Role: provider.RoleUser, Content: transcript + "\n\n" + s.cfg.summaryPrompt},
		},
	}
	resp, err := s.prov.ChatCompletion(ctx, req)
	if err != nil {
		return agent.Message{}, err
	}
	return agent.Message{
		Role:    agent.RoleSystem,
		Content: []agent.ContentBlock{agent.SummaryBlock{Text: resp.Content}},
	}, nil
}

// renderTranscript flattens a slice of agent.Message into a plain text
// transcript suitable for an LLM input. Tool calls and tool results are
// labeled. Thinking blocks are dropped (they're internal).
func renderTranscript(msgs []agent.Message) string {
	var b strings.Builder
	for _, m := range msgs {
		var role string
		switch m.Role {
		case agent.RoleUser:
			role = "User"
		case agent.RoleAssistant:
			role = "Assistant"
		case agent.RoleSystem:
			role = "System"
		case agent.RoleTool:
			role = "Tool"
		default:
			role = string(m.Role)
		}
		b.WriteString(role)
		b.WriteString(": ")
		for _, blk := range m.Content {
			switch v := blk.(type) {
			case agent.TextBlock:
				b.WriteString(v.Text)
			case agent.SummaryBlock:
				b.WriteString(v.Text)
			case agent.ToolUseBlock:
				b.WriteString("[tool_use ")
				b.WriteString(v.Name)
				b.WriteString("]")
			case agent.ToolResultBlock:
				for _, sub := range v.Content {
					if tb, ok := sub.(agent.TextBlock); ok {
						b.WriteString(tb.Text)
					}
				}
			}
		}
		b.WriteString("\n")
	}
	return b.String()
}
