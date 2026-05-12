package codex

import (
	"context"
	"encoding/json"
	"time"

	"github.com/cago-frame/agents/cliagent/internal/runtime"
)

// HookStage 是 codex hook 触发的生命周期位点。
type HookStage string

const (
	StagePreToolUse       HookStage = "PreToolUse"
	StagePostToolUse      HookStage = "PostToolUse"
	StageUserPromptSubmit HookStage = "UserPromptSubmit"
	StageStop             HookStage = "Stop"
	StageSubagentStop     HookStage = "SubagentStop"
	StageSessionStart     HookStage = "SessionStart"
	StageSessionEnd       HookStage = "SessionEnd"
	StageNotification     HookStage = "Notification"
)

// HookFunc 是 codex hook 回调签名。
type HookFunc func(ctx context.Context, in HookInput) (*HookOutput, error)

// HookInput 是 hook 拿到的运行时上下文。所有字段都是 best effort —— backend
// 能填的填,填不了的留零值。
type HookInput struct {
	Stage HookStage

	SessionID string
	RunID     string
	Cwd       string

	Prompt   string
	Messages []Message

	ToolName     string
	ToolInput    json.RawMessage
	ToolResponse json.RawMessage

	TranscriptPath string
	Raw            json.RawMessage
}

// HookDecision 控制类决策枚举。
type HookDecision string

const (
	DecisionNone    HookDecision = ""
	DecisionApprove HookDecision = "approve"
	DecisionDeny    HookDecision = "deny"
)

// HookOutput 是 hook 对当前 run 的反馈。所有字段 best effort;具体 stage 上
// 哪个字段起作用见 IsFieldApplicable。
type HookOutput struct {
	Continue       *bool
	StopReason     string
	SuppressOutput bool

	Decision HookDecision
	Reason   string

	AdditionalContext string

	Raw map[string]any
}

// HookOutputField 是 IsFieldApplicable 的字段枚举。
type HookOutputField string

const (
	FieldDecisionDeny      HookOutputField = "decision_deny"
	FieldDecisionApprove   HookOutputField = "decision_approve"
	FieldStopRun           HookOutputField = "stop_run"
	FieldAdditionalContext HookOutputField = "additional_context"
	FieldSuppressOutput    HookOutputField = "suppress_output"
)

// IsFieldApplicable 返回 stage 上 field 是否生效。
func IsFieldApplicable(stage HookStage, field HookOutputField) bool {
	type key struct {
		s HookStage
		f HookOutputField
	}
	matrix := map[key]bool{
		{StagePreToolUse, FieldDecisionDeny}:            true,
		{StageUserPromptSubmit, FieldDecisionDeny}:      true,
		{StagePreToolUse, FieldDecisionApprove}:         true,
		{StagePreToolUse, FieldStopRun}:                 true,
		{StagePostToolUse, FieldStopRun}:                true,
		{StageUserPromptSubmit, FieldStopRun}:           true,
		{StageSessionStart, FieldStopRun}:               true,
		{StageSubagentStop, FieldStopRun}:               true,
		{StagePreToolUse, FieldAdditionalContext}:       true,
		{StagePostToolUse, FieldAdditionalContext}:      true,
		{StageUserPromptSubmit, FieldAdditionalContext}: true,
		{StageSessionStart, FieldAdditionalContext}:     true,
		{StagePostToolUse, FieldSuppressOutput}:         true,
	}
	return matrix[key{stage, field}]
}

// hookSpec 是内部存储:Runner.New 收集所有 hook 注册,在编译 runtime hook 链时翻译。
type hookSpec struct {
	Stage   HookStage
	Matcher string
	Fn      HookFunc
	Timeout time.Duration
}

// wrapHookFunc bridges a codex-native HookFunc to a runtime.HookFunc.
func wrapHookFunc(fn HookFunc) runtime.HookFunc {
	return func(ctx context.Context, rin runtime.HookInput) (runtime.HookOutput, error) {
		in := HookInput{
			Stage:        HookStage(stageRuntimeToCodex(rin.Stage)),
			Cwd:          rin.Cwd,
			Prompt:       rin.Prompt,
			ToolName:     rin.ToolName,
			ToolInput:    rin.Input,
			ToolResponse: rin.Output,
			Raw:          rin.Raw,
		}
		out, err := fn(ctx, in)
		if err != nil {
			return runtime.HookOutput{}, err
		}
		if out == nil {
			return runtime.HookOutput{}, nil
		}
		rout := runtime.HookOutput{
			Reason:            out.Reason,
			AdditionalContext: out.AdditionalContext,
			SuppressOutput:    out.SuppressOutput,
		}
		switch out.Decision {
		case DecisionApprove:
			rout.Decision = runtime.DecisionApprove
		case DecisionDeny:
			rout.Decision = runtime.DecisionDeny
		}
		if out.Continue != nil && !*out.Continue {
			rout.StopRun = true
			if out.StopReason != "" {
				rout.Reason = out.StopReason
			}
		}
		return rout, nil
	}
}

// stageRuntimeToCodex converts a runtime.HookStage to the codex public HookStage names.
func stageRuntimeToCodex(s runtime.HookStage) string {
	switch s {
	case runtime.StagePreToolUse:
		return string(StagePreToolUse)
	case runtime.StagePostToolUse:
		return string(StagePostToolUse)
	case runtime.StageUserPromptSubmit:
		return string(StageUserPromptSubmit)
	case runtime.StageStop:
		return string(StageStop)
	case runtime.StageSubagentStop:
		return string(StageSubagentStop)
	case runtime.StageSessionStart:
		return string(StageSessionStart)
	case runtime.StageSessionEnd:
		return string(StageSessionEnd)
	case runtime.StageNotification:
		return string(StageNotification)
	}
	return string(s)
}

// stageCodexToRuntime converts a codex-public HookStage to runtime.HookStage.
func stageCodexToRuntime(s HookStage) runtime.HookStage {
	switch s {
	case StagePreToolUse:
		return runtime.StagePreToolUse
	case StagePostToolUse:
		return runtime.StagePostToolUse
	case StageUserPromptSubmit:
		return runtime.StageUserPromptSubmit
	case StageStop:
		return runtime.StageStop
	case StageSubagentStop:
		return runtime.StageSubagentStop
	case StageSessionStart:
		return runtime.StageSessionStart
	case StageSessionEnd:
		return runtime.StageSessionEnd
	case StageNotification:
		return runtime.StageNotification
	}
	return runtime.HookStage(s)
}
