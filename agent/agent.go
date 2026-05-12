package agent

import (
	"context"

	"github.com/cago-frame/agents/provider"
)

type Agent struct {
	cfg *agentConfig
}

type agentConfig struct {
	prov                        provider.Provider
	system                      string
	extraSystem                 []string
	tools                       []Tool
	model                       string
	maxSteps                    int
	toolMiddleware              []hookEntry[ToolMiddleware]
	userPromptHooks             []UserPromptHook
	turnEndHooks                []TurnEndHook
	onRunnerStart               []func(context.Context, *Runner) error
	onRunnerEnd                 []func(context.Context, *Runner)
	onEventEntries              []onEventEntry
	stripPartialReasonsOverride *[]PartialState
	retryPolicy                 *RetryPolicy
}

type hookEntry[T any] struct {
	matcher string
	fn      T
}

type onEventEntry struct {
	filter EventFilter
	fn     func(context.Context, Event)
}

func New(prov provider.Provider, opts ...Option) *Agent {
	cfg := &agentConfig{
		prov:     prov,
		maxSteps: 30,
	}
	for _, o := range opts {
		o(cfg)
	}
	return &Agent{cfg: cfg}
}

func (a *Agent) Provider() provider.Provider { return a.cfg.prov }

func (a *Agent) Tools() []Tool {
	out := make([]Tool, len(a.cfg.tools))
	copy(out, a.cfg.tools)
	return out
}

func (a *Agent) Runner(conv *Conversation) *Runner {
	r, err := a.TryRunner(conv)
	if err != nil {
		// Panic preserves the "Runner short-lived, you must own conv" invariant
		// for callers who haven't checked the lock explicitly.
		panic(err)
	}
	return r
}

func (a *Agent) TryRunner(conv *Conversation) (*Runner, error) {
	if err := conv.acquireRunner(); err != nil {
		return nil, err
	}
	r := &Runner{agent: a, conv: conv, partialIndex: -1}
	r.dispatcher = newDispatcher(r)
	return r, nil
}
