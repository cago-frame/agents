package agent

import (
	"encoding/json"
	"slices"

	"github.com/cago-frame/agents/agent/blocks"
	"github.com/cago-frame/agents/provider"
)

// DefaultStripPartialReasons lists the PartialState values whose trailing
// assistant messages are stripped from each turn's LLM request when no
// explicit override is given.
//
// Only PartialStreaming is stripped by default. That state means a previous
// runner died mid-stream without finalizing — replaying it would resend
// uncommitted bytes the model itself never finished. Canceled and errored
// partials, by contrast, represent intentional or recoverable interruptions
// whose already-streamed text is meaningful: keeping them lets the next turn
// continue from where the model left off.
var DefaultStripPartialReasons = []PartialState{PartialStreaming}

// RequestSpec is the input to BuildRequest. It bundles a Conversation snapshot
// plus the static parts of an Agent's config that affect a turn's request.
type RequestSpec struct {
	System   string
	Extra    []string // appended to system in order
	Messages []Message
	Tools    []Tool
	Model    string
	Thinking *provider.ThinkingConfig
	// StripPartialReasons, if non-nil, overrides DefaultStripPartialReasons.
	// Trailing assistant messages whose PartialReason matches any value in
	// the slice are stripped from req.Messages. A non-nil empty slice keeps
	// every trailing partial.
	StripPartialReasons *[]PartialState
}

// BuildRequest converts a Conversation snapshot + Agent config into a
// provider.CompletionRequest. The projection is audience-driven: every
// ContentBlock whose Audience() omits blocks.ToLLM is dropped before the
// request is constructed, so UI-only blocks (DisplayTextBlock, RefBlock,
// NoticeBlock) never reach the model. Trailing assistant messages whose
// PartialReason is in DefaultStripPartialReasons are also stripped; pass
// spec.StripPartialReasons to override.
func BuildRequest(spec RequestSpec) *provider.CompletionRequest {
	reasons := DefaultStripPartialReasons
	if spec.StripPartialReasons != nil {
		reasons = *spec.StripPartialReasons
	}
	msgs := stripTrailingPartialAssistants(spec.Messages, reasons)

	req := &provider.CompletionRequest{
		Model: spec.Model,
	}
	if spec.Thinking != nil {
		v := *spec.Thinking
		req.Thinking = &v
	}
	if sys := assembleSystem(spec.System, spec.Extra); sys != "" {
		req.Messages = append(req.Messages, provider.Message{
			Role:    provider.RoleSystem,
			Content: sys,
		})
	}
	for _, m := range msgs {
		pm := convertMessage(m)
		// Drop empty assistant messages — these arise when a partial got
		// stripped of its only meaningful blocks (e.g. unfinalized tool_uses)
		// and would otherwise reach the LLM as a literal empty assistant turn,
		// which several providers reject.
		if pm.Role == provider.RoleAssistant &&
			pm.Content == "" &&
			len(pm.MultiContent) == 0 &&
			len(pm.ToolCalls) == 0 &&
			len(pm.Thinking) == 0 {
			continue
		}
		req.Messages = append(req.Messages, pm)
	}
	for _, t := range spec.Tools {
		req.Tools = append(req.Tools, convertTool(t))
	}
	return req
}

func assembleSystem(base string, extra []string) string {
	if base == "" && len(extra) == 0 {
		return ""
	}
	out := base
	for _, e := range extra {
		if out == "" {
			out = e
		} else {
			out = out + "\n\n" + e
		}
	}
	return out
}

func stripTrailingPartialAssistants(msgs []Message, reasons []PartialState) []Message {
	if len(reasons) == 0 {
		return msgs
	}
	end := len(msgs)
	for end > 0 {
		m := msgs[end-1]
		if m.Role != RoleAssistant || m.PartialReason == PartialNone {
			break
		}
		if !slices.Contains(reasons, m.PartialReason) {
			break
		}
		end--
	}
	return msgs[:end]
}

func convertMessage(m Message) provider.Message {
	pm := provider.Message{Role: convertRole(m.Role)}
	var text string
	var toolCalls []provider.ToolCall
	var thinking []provider.ThinkingBlock
	var images []ImageBlock
	hasImage := false
	for _, b := range m.Content {
		// Audience-driven projection: any block whose Audience omits ToLLM
		// is invisible to the model. This is what makes the design
		// extensible — adding a new UI-only block type costs nothing here.
		if !b.Audience().Has(blocks.ToLLM) {
			continue
		}
		switch v := b.(type) {
		case TextBlock:
			if text != "" {
				text += "\n"
			}
			text += v.Text
		case SummaryBlock:
			if text != "" {
				text += "\n"
			}
			text += v.Text
		case ToolUseBlock:
			// Skip blocks whose args never reached the ready state. Replaying
			// them to the LLM as malformed tool_calls would break the request
			// and there's no paired tool_result anyway.
			if v.State != ToolUseReady || v.Input == nil {
				continue
			}
			args, _ := json.Marshal(v.Input)
			toolCalls = append(toolCalls, provider.ToolCall{
				ID:   v.ID,
				Type: provider.ToolTypeFunction,
				Function: provider.ToolCallFunction{
					Name:      v.Name,
					Arguments: string(args),
				},
			})
		case ToolResultBlock:
			// For tool role messages, the LLM expects:
			//   { role: "tool", tool_call_id: <id>, content: <text> }
			pm.ToolCallID = v.ToolUseID
			for _, sub := range v.Content {
				if !sub.Audience().Has(blocks.ToLLM) {
					continue
				}
				if tb, ok := sub.(TextBlock); ok {
					if text != "" {
						text += "\n"
					}
					text += tb.Text
				}
			}
		case ThinkingBlock:
			thinking = append(thinking, provider.ThinkingBlock{Text: v.Text, Signature: v.Signature})
		case ImageBlock:
			hasImage = true
			images = append(images, v)
		}
	}

	if hasImage {
		if text != "" {
			pm.MultiContent = append(pm.MultiContent, provider.MessagePart{
				Type: provider.MessagePartText,
				Text: text,
			})
		}
		for _, img := range images {
			pm.MultiContent = append(pm.MultiContent, provider.MessagePart{
				Type: provider.MessagePartImage,
				Image: &provider.MessageImage{
					URL:       img.Source.URL,
					MediaType: img.MediaType,
					Inline:    img.Source.Inline,
				},
			})
		}
	} else {
		pm.Content = text
	}

	pm.ToolCalls = toolCalls
	pm.Thinking = thinking
	return pm
}

func convertRole(r Role) provider.Role {
	switch r {
	case RoleSystem:
		return provider.RoleSystem
	case RoleAssistant:
		return provider.RoleAssistant
	case RoleTool:
		return provider.RoleTool
	default:
		return provider.RoleUser
	}
}

func convertTool(t Tool) provider.Tool {
	schema := t.Schema()
	raw, _ := json.Marshal(schema)
	return provider.Tool{
		Type: provider.ToolTypeFunction,
		Function: &provider.FunctionDefinition{
			Name:        t.Name(),
			Description: t.Description(),
			Parameters:  raw,
		},
	}
}
