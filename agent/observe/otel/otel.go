package otel

import (
	agent "github.com/cago-frame/agents/agent"
)

// Plugin returns a no-op agent.Option. A real OpenTelemetry plugin should
// replace this — see package doc for the design checklist.
func Plugin() agent.Option {
	return agent.Compose()
}
