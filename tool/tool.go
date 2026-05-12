// Package tool defines the Tool interface (== agent.Tool) and the
// RawTool builder used by every tool/<sub> subpackage.
//
// Legacy consumers (agent/, mcp/, cliagent/*) wrap returned tools via
// tool/legacy.Adapt(...) to obtain the old json.RawMessage Schema +
// (any, error) Call shape. That shim disappears in Phase 6.
package tool

import (
	"context"

	agent "github.com/cago-frame/agents/agent"
)

type Tool = agent.Tool
type SerialTool = agent.SerialTool

type RawTool struct {
	NameStr   string
	DescStr   string
	SchemaVal agent.Schema
	IsSerial  bool
	Handler   func(ctx context.Context, input map[string]any) (*agent.ToolResultBlock, error)

	PromptSnippetStr    string
	PromptGuidelinesArr []string
}

func (r *RawTool) Name() string         { return r.NameStr }
func (r *RawTool) Description() string  { return r.DescStr }
func (r *RawTool) Schema() agent.Schema { return r.SchemaVal }
func (r *RawTool) Serial() bool         { return r.IsSerial }
func (r *RawTool) Call(ctx context.Context, in map[string]any) (*agent.ToolResultBlock, error) {
	return r.Handler(ctx, in)
}
func (r *RawTool) PromptSnippet() string { return r.PromptSnippetStr }
func (r *RawTool) PromptGuidelines() []string {
	if len(r.PromptGuidelinesArr) == 0 {
		return nil
	}
	return append([]string(nil), r.PromptGuidelinesArr...)
}

// Option 构造时可选项。
type Option func(*RawTool)

// WithSerial 标记该工具与其他工具不能并行执行。
func WithSerial() Option {
	return func(r *RawTool) { r.IsSerial = true }
}

// WithPromptMeta sets RawTool.PromptSnippetStr / PromptGuidelinesArr. Used by
// each tool subpackage's New() so upper layers (e.g., app/coding's
// BuildSystemPrompt) can collect the per-tool snippet + guidelines.
func WithPromptMeta(snippet string, guidelines []string) Option {
	return func(r *RawTool) {
		r.PromptSnippetStr = snippet
		r.PromptGuidelinesArr = append([]string(nil), guidelines...)
	}
}
