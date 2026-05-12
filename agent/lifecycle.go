package agent

import "context"

func OnRunnerStart(fn func(ctx context.Context, r *Runner) error) Option {
	return func(c *agentConfig) { c.onRunnerStart = append(c.onRunnerStart, fn) }
}

func OnRunnerEnd(fn func(ctx context.Context, r *Runner)) Option {
	return func(c *agentConfig) { c.onRunnerEnd = append(c.onRunnerEnd, fn) }
}

func OnEvent(filter EventFilter, fn func(context.Context, Event)) Option {
	return func(c *agentConfig) {
		c.onEventEntries = append(c.onEventEntries, onEventEntry{filter, fn})
	}
}
