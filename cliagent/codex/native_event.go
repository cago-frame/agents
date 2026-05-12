package codex

import (
	"encoding/json"

	"github.com/cago-frame/agents/provider"
)

// EventKind codex 事件离散种类。
type EventKind string

const (
	EventTextDelta     EventKind = "text_delta"
	EventThinkingDelta EventKind = "thinking_delta"
	EventMessageEnd    EventKind = "message_end"

	EventSessionStart     EventKind = "session_start"
	EventSessionEnd       EventKind = "session_end"
	EventUserPromptSubmit EventKind = "user_prompt_submit"
	EventPreToolUse       EventKind = "pre_tool_use"
	EventPostToolUse      EventKind = "post_tool_use"
	EventStop             EventKind = "stop"
	EventSubagentStop     EventKind = "subagent_stop"
	EventNotification     EventKind = "notification"

	EventUsage EventKind = "usage"

	EventDone  EventKind = "done"
	EventError EventKind = "error"
)

// StopReason run 终止原因。
type StopReason string

const (
	StopEndTurn   StopReason = "end_turn"
	StopMaxSteps  StopReason = "max_steps"
	StopHook      StopReason = "hook"
	StopError     StopReason = "error"
	StopCancelled StopReason = "canceled"
	StopClosed    StopReason = "closed"
)

// ToolSource 工具来源标注。
type ToolSource string

const (
	ToolSourceUnknown ToolSource = ""
	ToolSourceAPI     ToolSource = "api"
	ToolSourceMCP     ToolSource = "mcp"
	ToolSourceBuiltin ToolSource = "builtin"
)

// Event 是 codex.Stream 暴露的事件类型。值类型,Observer 与消费者拿到的副本
// 互相隔离。
type Event struct {
	Kind EventKind

	SessionID string
	RunID     string
	Cwd       string

	Text   string
	Prompt string

	Tool *ToolEvent

	Message *Message
	Usage   provider.Usage

	Stop StopReason
	Err  error
	Raw  json.RawMessage
}

// ToolEvent 是 PreToolUse / PostToolUse 事件携带的工具相关字段。
type ToolEvent struct {
	ID       string
	Name     string
	Input    json.RawMessage
	Response json.RawMessage
	Err      error
	ParentID string
	Source   ToolSource
}
