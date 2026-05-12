package codex

import (
	"github.com/cago-frame/agents/cliagent/internal/runtime"
)

// 本文件包含 codex native 类型与 cliagent/internal/runtime 之间的内部转换。
// 仅供 native_stream.go / native_session.go 使用 —— 用户从不直接接触这些函数。

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
			Values:   cloneValues(in.State.Values),
		},
	}
	return out
}

// cloneValues converts a runtime.State.Values (map[string]any) to the public
// codex State.Values (map[string]string). Non-string values are stringified
// with empty fallback to preserve the previous public API shape.
func cloneValues(in map[string]any) map[string]string {
	if in == nil {
		return nil
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		if s, ok := v.(string); ok {
			out[k] = s
			continue
		}
		out[k] = ""
	}
	return out
}
