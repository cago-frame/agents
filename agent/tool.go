package agent

import (
	"context"
	"iter"
)

type Tool interface {
	Name() string
	Description() string
	Schema() Schema
	Call(ctx context.Context, input map[string]any) (*ToolResultBlock, error)
}

// SerialTool is an optional capability: when Serial() is true, any tool call
// in a turn that hits a SerialTool causes the entire turn's tool dispatch
// to run serially instead of in parallel.
type SerialTool interface {
	Tool
	Serial() bool
}

// StreamingTool is an optional capability: tools that emit progressive output
// (e.g. shelling a long command). The dispatcher emits EventToolDelta for
// each yielded ToolDelta; the final ToolDelta with Final != nil completes
// the call.
type StreamingTool interface {
	Tool
	CallStream(ctx context.Context, input map[string]any) iter.Seq[ToolDelta]
}

type ToolDelta struct {
	Text   string
	Stderr bool
	Final  *ToolResultBlock
}
