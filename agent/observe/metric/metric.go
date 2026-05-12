package metric

import (
	agent "github.com/cago-frame/agents/agent"
)

// Plugin returns a no-op agent.Option. Real implementation TBD; see package doc.
func Plugin() agent.Option {
	return agent.Compose()
}
