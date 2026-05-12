package compactor

import (
	"context"

	agent "github.com/cago-frame/agents/agent"
)

// WithStrategy returns an agent.Option that registers a TurnEnd hook. After
// each turn, the hook calls Strategy.ShouldCompact; if true, it Compacts the
// conversation, replaces history with the single summary, and emits
// EventCompacted via TurnEndOutput.EmittedEvents.
func WithStrategy(s Strategy) agent.Option {
	return agent.TurnEnd(func(ctx context.Context, in *agent.TurnEndInput) (*agent.TurnEndOutput, error) {
		conv, ok := in.Conv.(*agent.Conversation)
		if !ok {
			return &agent.TurnEndOutput{}, nil
		}
		if !s.ShouldCompact(conv) {
			return &agent.TurnEndOutput{}, nil
		}
		summary, err := s.Compact(ctx, conv)
		if err != nil {
			return nil, err
		}
		_ = conv.Truncate(0)
		conv.Append(summary)
		return &agent.TurnEndOutput{
			EmittedEvents: []agent.Event{
				{Kind: agent.EventCompacted, Delta: textOf(summary)},
			},
		}, nil
	})
}

func textOf(m agent.Message) string {
	for _, b := range m.Content {
		switch v := b.(type) {
		case agent.SummaryBlock:
			return v.Text
		case agent.TextBlock:
			return v.Text
		}
	}
	return ""
}
