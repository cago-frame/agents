// Package clihook 实现 Claude Code 原生 hooks 协议（UDS + JSON）。
// 仅供 claudecode 父包内部使用。
package clihook

import (
	"context"
	"encoding/json"
)

// Stage Claude Code 原生 hook 的生命周期点。
// 对应 Claude Code settings.json 中 "hooks" 下的顶层 key。
type Stage string

const (
	PreToolUse       Stage = "PreToolUse"
	PostToolUse      Stage = "PostToolUse"
	UserPromptSubmit Stage = "UserPromptSubmit"
	Stop             Stage = "Stop"
	SubagentStop     Stage = "SubagentStop"
	SessionStart     Stage = "SessionStart"
	SessionEnd       Stage = "SessionEnd"
	Notification     Stage = "Notification"
)

// Input CLI 通过 stdin 传给 hook 命令的 JSON 载荷。字段按 stage 填充。
// 未列出的字段可从 Raw 自行解析。
type Input struct {
	SessionID      string `json:"session_id,omitempty"`
	TranscriptPath string `json:"transcript_path,omitempty"`
	Cwd            string `json:"cwd,omitempty"`
	HookEventName  string `json:"hook_event_name,omitempty"`

	// Pre/PostToolUse
	ToolName     string          `json:"tool_name,omitempty"`
	ToolInput    json.RawMessage `json:"tool_input,omitempty"`
	ToolResponse json.RawMessage `json:"tool_response,omitempty"`

	// UserPromptSubmit
	Prompt string `json:"prompt,omitempty"`

	// Raw 原始请求 body，未解析到字段可在这里取。
	Raw json.RawMessage `json:"-"`
}

// Output 返回给 CLI 的 JSON。所有字段可选，nil 或零值等价"放行"。
// 对 PostToolUse / UserPromptSubmit / SessionStart，`HookSpecificOutput.additionalContext`
// 会被 CLI 追加到下一轮 LLM 上下文。
type Output struct {
	// 顶层控制
	Continue       *bool  `json:"continue,omitempty"`
	StopReason     string `json:"stopReason,omitempty"`
	SuppressOutput bool   `json:"suppressOutput,omitempty"`

	// PreToolUse 专用：decision=approve|block, reason 会被写给 LLM
	Decision string `json:"decision,omitempty"`
	Reason   string `json:"reason,omitempty"`

	// stage 特定字段，例如 PostToolUse: {hookEventName, additionalContext}
	HookSpecificOutput map[string]any `json:"hookSpecificOutput,omitempty"`
}

// Func 用户注册的回调。返回 (nil, nil) 等价放行无修改。
type Func func(ctx context.Context, in Input) (*Output, error)

// Entry 内部存储结构。ID 由 server 分配。
type Entry struct {
	Stage   Stage
	Matcher string
	Fn      Func
	ID      string
	Shared  bool // RegisterHook(BeforeTool/AfterTool) 适配生成；避免重复注册
}
