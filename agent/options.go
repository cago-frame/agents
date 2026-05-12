package agent

import "github.com/cago-frame/agents/provider"

type Option func(*agentConfig)

// Compose flattens a set of Options into one. Nested Compose unfolds naturally
// at apply time. Order is preserved across composition layers.
func Compose(opts ...Option) Option {
	return func(c *agentConfig) {
		for _, o := range opts {
			if o != nil {
				o(c)
			}
		}
	}
}

func System(prompt string) Option {
	return func(c *agentConfig) { c.system = prompt }
}

func AppendSystem(extra string) Option {
	return func(c *agentConfig) { c.extraSystem = append(c.extraSystem, extra) }
}

func Tools(tools ...Tool) Option {
	return func(c *agentConfig) { c.tools = append(c.tools, tools...) }
}

func Model(name string) Option {
	return func(c *agentConfig) { c.model = name }
}

// Thinking sets the provider-level thinking/reasoning configuration for every
// turn this Agent runs. Providers decide how to translate it.
func Thinking(cfg *provider.ThinkingConfig) Option {
	return func(c *agentConfig) {
		if cfg == nil {
			c.thinking = nil
			return
		}
		v := *cfg
		c.thinking = &v
	}
}

func MaxSteps(n int) Option {
	return func(c *agentConfig) { c.maxSteps = n }
}

// StripPartialReasons overrides DefaultStripPartialReasons for every turn this
// Agent runs. Trailing assistant messages whose PartialReason matches any of
// the given reasons are removed from each turn's LLM request.
//
// Calling with no arguments disables stripping entirely (every partial,
// including PartialStreaming, is replayed to the model).
//
// Example — restore the pre-2026-05 behavior of stripping every partial:
//
//	agent.StripPartialReasons(agent.PartialStreaming, agent.PartialCancelled, agent.PartialErrored)
func StripPartialReasons(reasons ...PartialState) Option {
	return func(c *agentConfig) {
		list := append([]PartialState(nil), reasons...)
		if list == nil {
			list = []PartialState{}
		}
		c.stripPartialReasonsOverride = &list
	}
}
