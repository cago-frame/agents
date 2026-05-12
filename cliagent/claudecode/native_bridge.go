package claudecode

import (
	"github.com/cago-frame/agents/cliagent/internal/runtime"
)

// 本文件包含 claudecode native 类型与 runtime.* 的内部转换。

func toNativeEvent(in runtime.Event) Event {
	out := Event{
		Kind:      EventKind(in.Kind),
		SessionID: in.SessionID,
		RunID:     in.RunID,
		Cwd:       in.Cwd,
		Text:      in.Text,
		Prompt:    in.Prompt,
		Usage:     in.Usage,
		Stop:      StopReason(in.Stop),
		Err:       in.Err,
		Raw:       in.Raw,

		PermissionRequestID: in.PermissionRequestID,
	}
	if in.Tool != nil {
		out.Tool = &ToolEvent{
			ID:       in.Tool.ID,
			Name:     in.Tool.Name,
			Input:    in.Tool.Input,
			Response: in.Tool.Response,
			Err:      in.Tool.Err,
			ParentID: in.Tool.ParentID,
			Source:   ToolSource(in.Tool.Source),
		}
	}
	if in.Message != nil {
		m := toNativeMessage(*in.Message)
		out.Message = &m
	}
	return out
}

func toNativeMessage(in runtime.Message) Message {
	out := Message{
		Role:    MessageRole(in.Role),
		Origin:  MessageOrigin(in.Origin),
		Text:    in.Text,
		Persist: in.Persist,
		Time:    in.CreatedAt,
		Raw:     in.Raw,
	}
	// derive Kind from runtime fields (heuristic, since runtime.Message has no Kind).
	switch {
	case len(in.ToolCalls) > 0:
		out.Kind = MessageKindToolCall
		first := in.ToolCalls[0]
		out.ToolCall = &ToolCall{
			ID:   first.ID,
			Name: first.Name,
			Args: first.Input,
		}
	case in.ToolResult != nil:
		out.Kind = MessageKindToolResult
		out.ToolResult = &ToolResult{
			Result: in.ToolResult.Output,
		}
	default:
		out.Kind = MessageKindText
	}
	return out
}

func toNativeMessages(in []runtime.Message) []Message {
	if in == nil {
		return nil
	}
	out := make([]Message, len(in))
	for i, m := range in {
		out[i] = toNativeMessage(m)
	}
	return out
}

func toNativeResult(in *runtime.Result) *Result {
	if in == nil {
		return nil
	}
	out := &Result{
		Text:     in.Text,
		Messages: toNativeMessages(in.History),
		Usage:    in.Usage,
		Stop:     StopReason(in.Stop),
		State: State{
			ThreadID: in.State.ThreadID,
			Values:   cloneStringValues(in.State.Values),
		},
	}
	return out
}

// cloneStringValues converts a runtime.State.Values (map[string]any) to the
// public claudecode State.Values (map[string]string). Non-string values are
// stringified via fmt.Sprint to preserve forward-compat (the previous public
// API was map[string]string). For now most values are simple strings.
func cloneStringValues(in map[string]any) map[string]string {
	if in == nil {
		return nil
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		if s, ok := v.(string); ok {
			out[k] = s
			continue
		}
		// best-effort stringify; non-string values are rare and only set by tests.
		out[k] = ""
	}
	return out
}
