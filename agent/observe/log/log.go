package log

import (
	"context"

	"go.uber.org/zap"

	agent "github.com/cago-frame/agents/agent"
)

type config struct {
	logger *zap.Logger
	deltas bool
}

// Option configures the log plugin.
type Option func(*config)

// WithLogger sets the zap logger. Default is a no-op logger.
func WithLogger(l *zap.Logger) Option {
	return func(c *config) {
		if l != nil {
			c.logger = l
		}
	}
}

// WithDeltas enables Debug-level logging of TextDelta / ThinkingDelta /
// ToolDelta events. Off by default to keep production logs quiet.
func WithDeltas() Option {
	return func(c *config) { c.deltas = true }
}

// Plugin returns an agent.Option that registers OnEvent callbacks emitting
// structured zap log entries for the agent's lifecycle.
func Plugin(opts ...Option) agent.Option {
	cfg := config{logger: zap.NewNop()}
	for _, o := range opts {
		o(&cfg)
	}

	return agent.Compose(
		agent.OnEvent(agent.AnyEvent, func(ctx context.Context, ev agent.Event) {
			handle(cfg, ev)
		}),
	)
}

func handle(cfg config, ev agent.Event) {
	l := cfg.logger
	switch ev.Kind {
	case agent.EventTextDelta:
		if cfg.deltas {
			l.Debug("text_delta", zap.String("turn_id", ev.TurnID), zap.String("delta", ev.Delta))
		}
	case agent.EventThinkingDelta:
		if cfg.deltas {
			l.Debug("thinking_delta", zap.String("turn_id", ev.TurnID), zap.String("delta", ev.Delta))
		}
	case agent.EventToolDelta:
		if cfg.deltas {
			l.Debug("tool_delta",
				zap.String("turn_id", ev.TurnID),
				zap.String("tool_use_id", toolID(ev)),
				zap.String("delta", ev.Delta),
			)
		}
	case agent.EventMessageEnd:
		l.Info("message_end",
			zap.String("turn_id", ev.TurnID),
			zap.Int("stop_reason", int(ev.StopReason)),
		)
	case agent.EventPreToolUse:
		l.Info("pre_tool_use",
			zap.String("turn_id", ev.TurnID),
			zap.String("tool_name", toolName(ev)),
			zap.String("tool_use_id", toolID(ev)),
		)
	case agent.EventPostToolUse:
		l.Info("post_tool_use",
			zap.String("turn_id", ev.TurnID),
			zap.String("tool_name", toolName(ev)),
			zap.String("tool_use_id", toolID(ev)),
		)
	case agent.EventTurnEnd:
		l.Info("turn_end",
			zap.String("turn_id", ev.TurnID),
			zap.Int("stop_reason", int(ev.StopReason)),
		)
	case agent.EventCompacted:
		l.Info("compacted", zap.String("turn_id", ev.TurnID))
	case agent.EventError:
		l.Warn("error", zap.String("turn_id", ev.TurnID), zap.Error(ev.Error))
	case agent.EventCancelled:
		l.Warn("canceled",
			zap.String("turn_id", ev.TurnID),
			zap.String("partial_reason", ev.PartialReason),
		)
	}
}

func toolName(ev agent.Event) string {
	if ev.Tool == nil {
		return ""
	}
	return ev.Tool.Name
}

func toolID(ev agent.Event) string {
	if ev.Tool == nil {
		return ""
	}
	return ev.Tool.ToolUseID
}
