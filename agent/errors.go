package agent

import (
	"errors"
	"fmt"
)

var (
	ErrConversationBusy   = errors.New("agent: conversation owned by active runner")
	ErrRunnerClosed       = errors.New("agent: runner closed")
	ErrSteerNoActiveTurn  = errors.New("agent: no active turn to steer")
	ErrIndexOutOfRange    = errors.New("agent: index out of range")
	ErrCannotResend       = errors.New("agent: cannot resend, last message is not user")
	ErrToolNotFound       = errors.New("agent: tool not found")
	ErrIncompatibleOption = errors.New("agent: incompatible option combination")
)

type ProviderError struct {
	Op     string
	Status int
	Cause  error
}

func (e *ProviderError) Error() string {
	if e.Status != 0 {
		return fmt.Sprintf("agent: provider %s failed (status=%d): %v", e.Op, e.Status, e.Cause)
	}
	return fmt.Sprintf("agent: provider %s failed: %v", e.Op, e.Cause)
}

func (e *ProviderError) Unwrap() error { return e.Cause }

type ToolError struct {
	ToolName  string
	ToolUseID string
	Cause     error
}

func (e *ToolError) Error() string {
	return fmt.Sprintf("agent: tool %q (%s) failed: %v", e.ToolName, e.ToolUseID, e.Cause)
}

func (e *ToolError) Unwrap() error { return e.Cause }

// HookStage identifies which lifecycle hook produced an error.
type HookStage string

const (
	HookStagePreToolUse       HookStage = "pre_tool_use"
	HookStagePostToolUse      HookStage = "post_tool_use"
	HookStageUserPromptSubmit HookStage = "user_prompt_submit"
	HookStageTurnEnd          HookStage = "turn_end"
)

// HookError wraps the error returned from a user-supplied hook function so
// callers consuming EventError can identify which stage / tool the failure
// originated from. Use errors.As to extract.
type HookError struct {
	Stage HookStage
	Tool  string // empty for non-tool hooks
	Cause error
}

func (e *HookError) Error() string {
	if e.Tool != "" {
		return fmt.Sprintf("agent: hook %s on tool %q failed: %v", e.Stage, e.Tool, e.Cause)
	}
	return fmt.Sprintf("agent: hook %s failed: %v", e.Stage, e.Cause)
}

func (e *HookError) Unwrap() error { return e.Cause }
