package clihook

// InjectAdditionalContext helper：构造一个只注入 additionalContext 的 output。
// 适用于 PostToolUse / UserPromptSubmit / SessionStart。
func InjectAdditionalContext(stage Stage, text string) *Output {
	return &Output{
		HookSpecificOutput: map[string]any{
			"hookEventName":     string(stage),
			"additionalContext": text,
		},
	}
}

// DenyTool helper：PreToolUse 用，拒绝工具调用并给出原因（CLI 会把 reason 反馈给 LLM）。
func DenyTool(reason string) *Output {
	return &Output{Decision: "block", Reason: reason}
}
