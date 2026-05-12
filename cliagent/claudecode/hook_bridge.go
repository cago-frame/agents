package claudecode

import (
	"context"

	"github.com/cago-frame/agents/cliagent/claudecode/internal/clihook"
	"github.com/cago-frame/agents/cliagent/internal/runtime"
)

// hookBridge wraps a clihook.Server with adapter functions translating between
// clihook protocol and runtime.Hook semantics.
type hookBridge struct {
	srv      *clihook.Server
	registry []runtime.Hook
}

// newHookBridge creates a hookBridge for the given hooks, registering each one
// as a clihook.Entry. Unknown stages are silently skipped.
func newHookBridge(hooks []runtime.Hook) (*hookBridge, error) {
	hb := &hookBridge{
		srv:      clihook.NewServer(),
		registry: append([]runtime.Hook(nil), hooks...),
	}
	for _, h := range hooks {
		stage, ok := stageToCLIStage(h.Stage)
		if !ok {
			continue // unknown stage: skip
		}
		hb.srv.AddHook(clihook.Entry{
			Stage:   stage,
			Matcher: h.Matcher,
			Fn:      hb.makeAdapter(h),
		})
	}
	return hb, nil
}

// makeAdapter returns a clihook.Func that translates clihook.Input → runtime.HookInput,
// calls runtime.RunHookChain on a single-element slice, and translates HookOutput
// back to clihook.Output.
func (hb *hookBridge) makeAdapter(h runtime.Hook) clihook.Func {
	return func(ctx context.Context, in clihook.Input) (*clihook.Output, error) {
		hin := runtime.HookInput{
			Stage:    h.Stage,
			ToolName: in.ToolName,
			Input:    in.ToolInput,
			Output:   in.ToolResponse,
			Prompt:   in.Prompt,
			Cwd:      in.Cwd,
			Raw:      in.Raw,
		}
		out, err := runtime.RunHookChain(ctx, []runtime.Hook{h}, h.Stage, hin)
		if err != nil {
			return nil, err
		}
		return translateHookOutput(h.Stage, out), nil
	}
}

// start lazy-starts the underlying clihook.Server.
func (hb *hookBridge) start() error { return hb.srv.Start() }

// settingsJSON returns the `--settings` JSON; empty when no hooks registered.
func (hb *hookBridge) settingsJSON() (string, error) { return hb.srv.BuildSettingsJSON() }

// shutdown closes the UDS server.
func (hb *hookBridge) shutdown(ctx context.Context) error { return hb.srv.Shutdown(ctx) }

// stageToCLIStage maps runtime.HookStage → clihook.Stage. Unknown stages return false.
func stageToCLIStage(s runtime.HookStage) (clihook.Stage, bool) {
	switch s {
	case runtime.StagePreToolUse:
		return clihook.PreToolUse, true
	case runtime.StagePostToolUse:
		return clihook.PostToolUse, true
	case runtime.StageUserPromptSubmit:
		return clihook.UserPromptSubmit, true
	case runtime.StageStop:
		return clihook.Stop, true
	case runtime.StageSubagentStop:
		return clihook.SubagentStop, true
	case runtime.StageSessionStart:
		return clihook.SessionStart, true
	case runtime.StageSessionEnd:
		return clihook.SessionEnd, true
	case runtime.StageNotification:
		return clihook.Notification, true
	}
	return "", false
}

// translateHookOutput maps runtime.HookOutput → *clihook.Output, respecting the
// stage applicability rules baked into the runtime hook contract. Fields not
// applicable at the given stage are silently dropped.
func translateHookOutput(stage runtime.HookStage, out runtime.HookOutput) *clihook.Output {
	cli := &clihook.Output{}
	hadField := false

	if out.StopRun && stageAllowsStopRun(stage) {
		cont := false
		cli.Continue = &cont
		cli.StopReason = out.Reason
		hadField = true
	}

	switch out.Decision {
	case runtime.DecisionDeny:
		if stageAllowsDeny(stage) {
			cli.Decision = "block"
			cli.Reason = out.Reason
			hadField = true
		}
	case runtime.DecisionApprove:
		if stage == runtime.StagePreToolUse {
			cli.Decision = "approve"
			cli.Reason = out.Reason
			hadField = true
		}
	}

	if out.AdditionalContext != "" && stageAllowsAdditionalContext(stage) {
		cli.HookSpecificOutput = map[string]any{
			"hookEventName":     mustCLIStage(stage),
			"additionalContext": out.AdditionalContext,
		}
		hadField = true
	}

	if out.SuppressOutput && stage == runtime.StagePostToolUse {
		cli.SuppressOutput = true
		hadField = true
	}

	if !hadField {
		return nil
	}
	return cli
}

func stageAllowsStopRun(stage runtime.HookStage) bool {
	switch stage {
	case runtime.StagePreToolUse, runtime.StagePostToolUse, runtime.StageUserPromptSubmit,
		runtime.StageSessionStart, runtime.StageSubagentStop:
		return true
	}
	return false
}

func stageAllowsDeny(stage runtime.HookStage) bool {
	return stage == runtime.StagePreToolUse || stage == runtime.StageUserPromptSubmit
}

func stageAllowsAdditionalContext(stage runtime.HookStage) bool {
	switch stage {
	case runtime.StagePreToolUse, runtime.StagePostToolUse, runtime.StageUserPromptSubmit,
		runtime.StageSessionStart:
		return true
	}
	return false
}

func mustCLIStage(s runtime.HookStage) string {
	cs, _ := stageToCLIStage(s)
	return string(cs)
}
