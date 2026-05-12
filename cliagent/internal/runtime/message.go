package runtime

import (
	"encoding/json"
	"time"

	"github.com/cago-frame/agents/provider"
)

type MessageRole string

const (
	RoleUser      MessageRole = "user"
	RoleAssistant MessageRole = "assistant"
	RoleSystem    MessageRole = "system"
	RoleTool      MessageRole = "tool"
)

type MessageOrigin string

const (
	OriginUser    MessageOrigin = "user"
	OriginModel   MessageOrigin = "model"
	OriginTool    MessageOrigin = "tool"
	OriginSystem  MessageOrigin = "system"
	OriginCLIHook MessageOrigin = "cli_hook"
)

type Message struct {
	Role       MessageRole
	Origin     MessageOrigin
	Text       string
	ToolCalls  []ToolCall
	ToolResult *ToolResult
	Usage      *provider.Usage
	Persist    bool
	CreatedAt  time.Time
	Raw        json.RawMessage
}

type ToolCall struct {
	ID    string
	Name  string
	Input json.RawMessage
}

type ToolResult struct {
	ToolCallID string
	Output     string
	Err        string
	Raw        json.RawMessage
}
