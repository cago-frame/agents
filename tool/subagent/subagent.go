// Package subagent provides a "wrap multiple sub-agents into a single
// dispatcher tool" helper. The returned tool is a normal agent.Tool;
// the parent agent calls it to delegate work to one of several child
// agents identified by a `type` enum.
//
// As of Phase 3, sub-agent events do NOT bubble up to the parent's event
// stream — observe the child agents directly via agent.OnEvent if you
// need that.
package subagent

import (
	"context"
	"errors"
	"fmt"
	"strings"

	agent "github.com/cago-frame/agents/agent"
	"github.com/cago-frame/agents/tool"
)

// Entry describes one sub-agent and its dispatch type.
type Entry struct {
	Type        string       // enum value (non-empty, unique within one NewTool call)
	Description string       // appended to the parent tool description
	Agent       *agent.Agent // constructed via agent.New(prov, ...)
}

// Option is an optional parameter for NewTool.
type Option func(*config)

type config struct{ serial bool }

// WithSerial marks this tool as not parallel-safe with other tools.
func WithSerial() Option { return func(c *config) { c.serial = true } }

// NewTool constructs the sub-agent dispatch tool. Construction-time validation
// errors panic (they are programming errors, not runtime errors).
func NewTool(name, description string, entries []Entry, opts ...Option) tool.Tool {
	if len(entries) == 0 {
		panic("subagent.NewTool: entries must not be empty")
	}
	seen := make(map[string]struct{}, len(entries))
	for i, e := range entries {
		if e.Type == "" {
			panic(fmt.Sprintf("subagent.NewTool: entries[%d].Type must not be empty", i))
		}
		if _, dup := seen[e.Type]; dup {
			panic(fmt.Sprintf("subagent.NewTool: duplicate Type %q", e.Type))
		}
		seen[e.Type] = struct{}{}
		if e.Agent == nil {
			panic(fmt.Sprintf("subagent.NewTool: entries[%d].Agent must not be nil", i))
		}
	}
	cfg := &config{}
	for _, o := range opts {
		o(cfg)
	}
	byType := make(map[string]Entry, len(entries))
	for _, e := range entries {
		byType[e.Type] = e
	}

	return &tool.RawTool{
		NameStr:   name,
		DescStr:   buildDescription(description, entries),
		SchemaVal: buildSchema(entries),
		IsSerial:  cfg.serial,
		Handler: func(ctx context.Context, in map[string]any) (*agent.ToolResultBlock, error) {
			return runChild(ctx, byType, in)
		},
	}
}

func runChild(ctx context.Context, byType map[string]Entry, in map[string]any) (*agent.ToolResultBlock, error) {
	typ, _ := in["type"].(string)
	prompt, _ := in["prompt"].(string)
	entry, ok := byType[typ]
	if !ok {
		return tool.ErrorResult("sub-agent unknown type: " + typ), nil
	}

	conv := agent.NewConversation()
	runner, err := entry.Agent.TryRunner(conv)
	if err != nil {
		return tool.ErrorResult("sub-agent error: " + err.Error()), nil
	}
	defer func() { _ = runner.Close() }()

	// Capture the stop reason + last stream-level error. runner.Wait drains the
	// event channel and returns nil for stream errors (they show up as
	// EventError frames, not as Wait's Go error). Without observing EventError,
	// a child whose API call fails would surface as "sub-agent returned no
	// content" with the real cause silently dropped — the parent agent then
	// can't see / report / retry the underlying error.
	var (
		lastStop  agent.StopReason
		lastError error
	)
	unsub := runner.OnEvent(
		agent.OnlyKinds(agent.EventTurnEnd, agent.EventError),
		func(_ context.Context, ev agent.Event) {
			switch ev.Kind {
			case agent.EventTurnEnd:
				lastStop = ev.StopReason
			case agent.EventError:
				if ev.Error != nil {
					lastError = ev.Error
				}
			}
		},
	)
	defer unsub()

	if err := runner.Wait(ctx, prompt); err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return nil, err
		}
		return tool.ErrorResult("sub-agent error: " + err.Error()), nil
	}

	text := lastAssistantText(conv)

	switch lastStop {
	case agent.StopEndTurn:
		if text == "" {
			return tool.TextResult("sub-agent returned no content"), nil
		}
		return tool.TextResult(text), nil
	case agent.StopMaxSteps:
		return tool.TextResult("[sub-agent stopped: max_steps]\n" + text), nil
	case agent.StopHook:
		return tool.TextResult("[sub-agent stopped: hook_stop]\n" + text), nil
	case agent.StopCancelled:
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return tool.TextResult("[sub-agent stopped: canceled]\n" + text), nil
	case agent.StopError:
		msg := "[sub-agent error]"
		if lastError != nil {
			msg = "[sub-agent error: " + lastError.Error() + "]"
		}
		if text != "" {
			return tool.ErrorResult(msg + "\n" + text), nil
		}
		return tool.ErrorResult(msg), nil
	case agent.StopTokenLimit:
		return tool.TextResult("[sub-agent stopped: token_limit]\n" + text), nil
	case agent.StopTimeout:
		return tool.TextResult("[sub-agent stopped: timeout]\n" + text), nil
	default:
		if text == "" {
			return tool.TextResult("sub-agent returned no content"), nil
		}
		return tool.TextResult(text), nil
	}
}

// lastAssistantText 从子对话里挑一段适合回给父 agent 的最终文本。
//
// 策略（从后往前）：
//  1. 命中最近一条含 TextBlock 的 assistant 消息 → 返回其文本（首选）。
//  2. 整条对话都没有 TextBlock 时，回退到最近一条含 ThinkingBlock 的
//     assistant 消息的思考文本——reasoning provider（DeepSeek-R1、Anthropic
//     extended thinking 等）允许模型把最终结论放在 thinking 流里而不发
//     content；不回退就会把这种模型的输出整段吞掉，父 agent 看到
//     "sub-agent returned no content"。
func lastAssistantText(conv *agent.Conversation) string {
	msgs := conv.Messages()
	var fallbackThinking string
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role != agent.RoleAssistant {
			continue
		}
		var text, thinking strings.Builder
		for _, c := range msgs[i].Content {
			switch t := c.(type) {
			case agent.TextBlock:
				text.WriteString(t.Text)
			case agent.ThinkingBlock:
				thinking.WriteString(t.Text)
			}
		}
		if text.Len() > 0 {
			return text.String()
		}
		if fallbackThinking == "" && thinking.Len() > 0 {
			fallbackThinking = thinking.String()
		}
	}
	return fallbackThinking
}

func buildDescription(base string, entries []Entry) string {
	var b strings.Builder
	b.WriteString(base)
	if base != "" {
		b.WriteString("\n\n")
	}
	b.WriteString("Available types:")
	for _, e := range entries {
		b.WriteString("\n- ")
		b.WriteString(e.Type)
		b.WriteString(": ")
		b.WriteString(e.Description)
	}
	return b.String()
}

func buildSchema(entries []Entry) agent.Schema {
	enumVals := make([]any, 0, len(entries))
	for _, e := range entries {
		enumVals = append(enumVals, e.Type)
	}
	return agent.Schema{
		Type:        "object",
		Description: "Dispatch a task to a sub-agent",
		Properties: map[string]*agent.Property{
			"title":  {Type: "string", Description: "human-readable label (optional)"},
			"type":   {Type: "string", Description: "sub-agent type", Enum: enumVals},
			"prompt": {Type: "string", Description: "task description"},
		},
		Required: []string{"type", "prompt"},
	}
}
