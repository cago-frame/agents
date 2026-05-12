package claudecode

import (
	"encoding/json"
	"time"

	"github.com/cago-frame/agents/provider"
)

// MessageKind 消息分类。值与 agent.MessageKind 对齐,允许后续独立演进。
type MessageKind string

const (
	MessageKindText              MessageKind = "text"
	MessageKindToolCall          MessageKind = "tool_call"
	MessageKindToolResult        MessageKind = "tool_result"
	MessageKindSystem            MessageKind = "system"
	MessageKindCompactionSummary MessageKind = "compaction_summary"
	MessageKindSteering          MessageKind = "steering"
	MessageKindFollowUp          MessageKind = "follow_up"
	MessageKindHookContext       MessageKind = "hook_context"
)

// MessageOrigin 消息来源端。
type MessageOrigin string

const (
	MessageOriginModel     MessageOrigin = "model"
	MessageOriginUser      MessageOrigin = "user"
	MessageOriginTool      MessageOrigin = "tool"
	MessageOriginHook      MessageOrigin = "hook"
	MessageOriginFramework MessageOrigin = "framework"
)

// MessageRole 与 chat 协议中的 role 等价。
type MessageRole string

const (
	RoleUser      MessageRole = "user"
	RoleAssistant MessageRole = "assistant"
	RoleSystem    MessageRole = "system"
	RoleTool      MessageRole = "tool"
)

// Message 是 claudecode 暴露的消息单位。
type Message struct {
	ID       string
	ParentID string

	Kind   MessageKind
	Origin MessageOrigin
	Role   MessageRole

	Text       string
	Thinking   []provider.ThinkingBlock
	ToolCall   *ToolCall
	ToolResult *ToolResult

	Persist bool
	Time    time.Time

	Raw json.RawMessage
}

// ToolCall 模型请求执行某个工具。
type ToolCall struct {
	ID       string
	Name     string
	Args     json.RawMessage
	ParentID string
}

// ToolResult 工具执行的返回值。
type ToolResult struct {
	Result   any
	Err      error
	Duration time.Duration
}
