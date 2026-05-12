package runtime

import (
	"encoding/json"

	"github.com/cago-frame/agents/provider"
)

type EventKind string

const (
	EventTextDelta         EventKind = "text_delta"
	EventThinkingDelta     EventKind = "thinking_delta"
	EventMessageEnd        EventKind = "message_end"
	EventSessionStart      EventKind = "session_start"
	EventSessionEnd        EventKind = "session_end"
	EventUserPromptSubmit  EventKind = "user_prompt_submit"
	EventPreToolUse        EventKind = "pre_tool_use"
	EventPostToolUse       EventKind = "post_tool_use"
	EventStop              EventKind = "stop"
	EventSubagentStop      EventKind = "subagent_stop"
	EventNotification      EventKind = "notification"
	EventPermissionRequest EventKind = "permission_request"
	EventUsage             EventKind = "usage"
	EventDone              EventKind = "done"
	EventError             EventKind = "error"
)

type StopReason string

const (
	StopEndTurn   StopReason = "end_turn"
	StopMaxSteps  StopReason = "max_steps"
	StopHook      StopReason = "hook"
	StopError     StopReason = "error"
	StopCancelled StopReason = "canceled"
	StopClosed    StopReason = "closed"
)

type ToolSource string

const (
	ToolSourceUnknown ToolSource = ""
	ToolSourceAPI     ToolSource = "api"
	ToolSourceMCP     ToolSource = "mcp"
	ToolSourceBuiltin ToolSource = "builtin"
)

// Event is the canonical internal event value carried through Stream.
// Each cliagent package converts to its own public Event at the boundary.
type Event struct {
	Kind EventKind

	SessionID string
	RunID     string
	Cwd       string

	Text   string
	Prompt string

	Tool    *ToolEvent
	Message *Message
	Usage   provider.Usage

	Stop StopReason
	Err  error
	Raw  json.RawMessage

	PermissionRequestID string
}

type ToolEvent struct {
	ID       string
	Name     string
	Input    json.RawMessage
	Response json.RawMessage
	Err      error
	ParentID string
	Source   ToolSource
}
