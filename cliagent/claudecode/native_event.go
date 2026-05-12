package claudecode

import (
	"encoding/json"

	"github.com/cago-frame/agents/provider"
)

// 本文件定义 claudecode 包对外暴露的 native 事件类型。和 agent.Event 在字段上
// 重合,但是独立 type —— 这是 step 4c 解耦的一部分:claudecode 不再依赖 agent.Event
// 作为公共协议,允许两边各自演进。
//
// 内部 Runner 仍通过 agent 包(unexported)backend 协议执行,但在 Stream 边界
// 把 agent.Event 翻译成 claudecode.Event。bridge 翻译位于 native_bridge.go。

// EventKind claudecode 事件离散种类。值与 agent.EventKind 暂时对齐(允许日后分化)。
type EventKind string

const (
	EventTextDelta     EventKind = "text_delta"
	EventThinkingDelta EventKind = "thinking_delta"
	EventMessageEnd    EventKind = "message_end"

	EventSessionStart      EventKind = "session_start"
	EventSessionEnd        EventKind = "session_end"
	EventUserPromptSubmit  EventKind = "user_prompt_submit"
	EventPreToolUse        EventKind = "pre_tool_use"
	EventPostToolUse       EventKind = "post_tool_use"
	EventStop              EventKind = "stop"
	EventSubagentStop      EventKind = "subagent_stop"
	EventNotification      EventKind = "notification"
	EventPermissionRequest EventKind = "permission_request"

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

// Event 是 claudecode.Stream 暴露的事件类型。值类型,Observer 与消费者拿到的副本
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

	PermissionRequestID string
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
