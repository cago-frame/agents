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

	// Capture the stop reason from EventTurnEnd so we can prefix the result.
	var lastStop agent.StopReason
	unsub := runner.OnEvent(agent.OnlyKinds(agent.EventTurnEnd), func(_ context.Context, ev agent.Event) {
		lastStop = ev.StopReason
	})
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
	default:
		if text == "" {
			return tool.TextResult("sub-agent returned no content"), nil
		}
		return tool.TextResult(text), nil
	}
}

func lastAssistantText(conv *agent.Conversation) string {
	msgs := conv.Messages()
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role != agent.RoleAssistant {
			continue
		}
		var sb strings.Builder
		for _, c := range msgs[i].Content {
			if t, ok := c.(agent.TextBlock); ok {
				sb.WriteString(t.Text)
			}
		}
		if sb.Len() > 0 {
			return sb.String()
		}
	}
	return ""
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
