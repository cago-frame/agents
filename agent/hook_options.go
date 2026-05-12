package agent

// Use registers a tool middleware. The matcher is a regex matched against the
// tool name (empty / "*" matches everything). Middleware runs in registration
// order; the chain terminates in the actual Tool.Call invocation. Middleware
// may call c.Next() to invoke the rest of the chain (gin-style around
// semantics) and inspect or rewrite c.Output afterwards.
//
// Replaces the legacy PreToolUse + PostToolUse split — a single middleware
// can express both phases via c.Next(), keeping closure-local state across
// before/after without an external sync.Map.
func Use(matcher string, mw ToolMiddleware) Option {
	return func(c *agentConfig) {
		c.toolMiddleware = append(c.toolMiddleware, hookEntry[ToolMiddleware]{matcher, mw})
	}
}

// UserPromptSubmit registers a hook invoked at the start of each Send/Resend
// turn. Not part of the tool middleware chain — kept as a distinct lifecycle
// callback because the input shape (text + Conv) doesn't share semantics with
// per-tool dispatch.
func UserPromptSubmit(fn UserPromptHook) Option {
	return func(c *agentConfig) { c.userPromptHooks = append(c.userPromptHooks, fn) }
}

// TurnEnd registers a hook invoked at the end of every turn (notification
// only; commonly used to attach compaction). Like UserPromptSubmit, this is
// a turn-level lifecycle callback — not a tool middleware.
func TurnEnd(fn TurnEndHook) Option {
	return func(c *agentConfig) { c.turnEndHooks = append(c.turnEndHooks, fn) }
}
