package runtime

import (
	"context"
	"fmt"
	"regexp"
)

// RunHookChain executes hooks for stage in order. Short-circuits on
// HookOutput.Decision == DecisionDeny or non-nil error.
//
// Behavior mirrors legacy agent.RunHookChain:
//   - Deny: returns the output as-is, no error; caller decides how to surface.
//   - Error: returns zero output + wrapped error.
//   - SuppressOutput / AdditionalContext / StopRun / Reason: forwarded; merging is the caller's job.
func RunHookChain(ctx context.Context, hooks []Hook, stage HookStage, in HookInput) (HookOutput, error) {
	var merged HookOutput
	for _, h := range hooks {
		if h.Stage != stage {
			continue
		}
		if h.Matcher != "" {
			ok, err := regexp.MatchString(h.Matcher, in.ToolName)
			if err != nil {
				return HookOutput{}, fmt.Errorf("hook matcher %q: %w", h.Matcher, err)
			}
			if !ok {
				continue
			}
		}
		out, err := h.Fn(ctx, in)
		if err != nil {
			return HookOutput{}, fmt.Errorf("hook %s: %w", stage, err)
		}
		if out.Decision == DecisionDeny {
			return out, nil
		}
		if out.Reason != "" {
			merged.Reason = out.Reason
		}
		if out.AdditionalContext != "" {
			if merged.AdditionalContext == "" {
				merged.AdditionalContext = out.AdditionalContext
			} else {
				merged.AdditionalContext += "\n" + out.AdditionalContext
			}
		}
		if out.SuppressOutput {
			merged.SuppressOutput = true
		}
		if out.StopRun {
			merged.StopRun = true
		}
	}
	return merged, nil
}
