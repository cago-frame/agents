package audit

import (
	"context"

	agent "github.com/cago-frame/agents/agent"
)

// Sink consumes audit records produced by the audit plugin.
// Real implementations should batch and buffer as appropriate.
type Sink interface {
	WriteAudit(ctx context.Context, record AuditRecord) error
}

// AuditRecord is the to-be-finalized payload schema. Fields will grow as
// requirements solidify; treat this as Phase 2c-frozen-only.
type AuditRecord struct {
	TurnID    string
	Kind      string // "pre_tool_use" | "post_tool_use" | ...
	ToolName  string
	ToolUseID string
	// Payload is provider-defined for now (input map / output result).
	Payload map[string]any
}

// Plugin returns a no-op agent.Option. Real implementation TBD; see package doc.
func Plugin(_ Sink) agent.Option {
	return agent.Compose()
}
