package claudecode

import "github.com/cago-frame/agents/cliagent/internal/runtime"

// sessionOptCfg holds per-session knobs handed to runtime.NewSession.
type sessionOptCfg struct {
	id    string
	store runtime.Store
}

// SessionOption customizes a Session created via Runner.Session.
type SessionOption func(*sessionOptCfg)

// SessionID assigns a deterministic session identifier; otherwise a random one is generated.
func SessionID(s string) SessionOption {
	return func(c *sessionOptCfg) { c.id = s }
}

// SessionStore plugs a custom runtime.Store; nil falls back to in-memory.
func SessionStore(s runtime.Store) SessionOption {
	return func(c *sessionOptCfg) { c.store = s }
}
