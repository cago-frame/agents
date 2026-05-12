package compactor

import (
	"context"
	"errors"

	agent "github.com/cago-frame/agents/agent"
)

// Strategy decides whether a Conversation needs compacting and produces the
// summary Message that will replace history.
type Strategy interface {
	ShouldCompact(conv *agent.Conversation) bool
	Compact(ctx context.Context, conv *agent.Conversation) (agent.Message, error)
}

// ErrNoopCompact is returned by Noop().Compact — the no-op strategy refuses
// to produce a summary because it is not designed to be invoked.
var ErrNoopCompact = errors.New("compactor: Noop strategy does not produce summaries")

type noop struct{}

func (noop) ShouldCompact(*agent.Conversation) bool { return false }

func (noop) Compact(context.Context, *agent.Conversation) (agent.Message, error) {
	return agent.Message{}, ErrNoopCompact
}

// Noop returns a Strategy that never compacts. Useful as a default or in tests.
func Noop() Strategy { return noop{} }
