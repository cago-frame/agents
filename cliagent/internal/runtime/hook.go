package runtime

import (
	"context"
	"encoding/json"
)

type HookStage string

const (
	StagePreToolUse       HookStage = "pre_tool_use"
	StagePostToolUse      HookStage = "post_tool_use"
	StageUserPromptSubmit HookStage = "user_prompt_submit"
	StageStop             HookStage = "stop"
	StageSubagentStop     HookStage = "subagent_stop"
	StageSessionStart     HookStage = "session_start"
	StageSessionEnd       HookStage = "session_end"
	StageNotification     HookStage = "notification"
)

type HookDecision string

const (
	DecisionUnset   HookDecision = ""
	DecisionApprove HookDecision = "approve"
	DecisionDeny    HookDecision = "deny"
)

type HookInput struct {
	Stage    HookStage
	ToolName string
	ToolID   string
	Input    json.RawMessage
	Output   json.RawMessage
	Prompt   string
	Reason   string
	Cwd      string
	Raw      json.RawMessage
}

type HookOutput struct {
	Decision          HookDecision
	Reason            string
	StopRun           bool
	AdditionalContext string
	SuppressOutput    bool
}

type HookFunc func(ctx context.Context, in HookInput) (HookOutput, error)

type Hook struct {
	Stage   HookStage
	Matcher string
	Fn      HookFunc
}
