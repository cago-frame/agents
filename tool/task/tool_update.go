package task

import (
	"context"
	"fmt"
	"strings"

	agent "github.com/cago-frame/agents/agent"
	"github.com/cago-frame/agents/tool"
)

// UpdateName 是 task_update 暴露给 LLM 的工具名。
const UpdateName = "task_update"

// UpdateDescription 暴露给 LLM 的工具描述。
const UpdateDescription = "Partially update one or more tasks by id. Only the fields you pass change; omitted fields stay the same. Batch multiple updates in a single call (e.g., complete one task and start the next)."

// NewUpdate 构造 task_update 工具。
func NewUpdate(opts ...Option) tool.Tool {
	cfg := newConfig(opts...)
	return &tool.RawTool{
		NameStr:   UpdateName,
		DescStr:   UpdateDescription,
		SchemaVal: updateSchema(),
		IsSerial:  cfg.serial,
		Handler: func(ctx context.Context, in map[string]any) (*agent.ToolResultBlock, error) {
			return runUpdate(ctx, cfg, in)
		},
		PromptSnippetStr: "Patch tasks by id (status / content / activeForm / priority).",
		PromptGuidelinesArr: []string{
			"Mark a task in_progress when you start it; mark it completed immediately when done; batch the two in one task_update call when transitioning.",
		},
	}
}

func updateSchema() agent.Schema {
	noAdditional := false
	return agent.Schema{
		Type:     "object",
		Required: []string{"updates"},
		Properties: map[string]*agent.Property{
			"updates": {
				Type:        "array",
				Description: "List of partial patches. Each must include id; other fields are optional and only included keys are applied.",
				Items: &agent.Property{
					Type: "object",
					Properties: map[string]*agent.Property{
						"id":         {Type: "string"},
						"status":     {Type: "string", Enum: []any{"pending", "in_progress", "completed"}},
						"content":    {Type: "string"},
						"activeForm": {Type: "string"},
						"priority":   {Type: "string", Enum: []any{"high", "medium", "low"}},
					},
					Required:             []string{"id"},
					AdditionalProperties: &noAdditional,
				},
			},
		},
	}
}

func runUpdate(ctx context.Context, cfg *config, in map[string]any) (*agent.ToolResultBlock, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	raw, ok := in["updates"]
	if !ok {
		return tool.ErrorResult("task_update: missing required field: updates"), nil
	}
	slice, ok := raw.([]any)
	if !ok {
		return tool.ErrorResult("task_update: updates must be an array"), nil
	}
	if len(slice) == 0 {
		return tool.ErrorResult("task_update: updates is empty (provide at least one)"), nil
	}

	type pending struct {
		id    string
		patch Patch
	}
	queued := make([]pending, 0, len(slice))

	for i, rawItem := range slice {
		m, ok := rawItem.(map[string]any)
		if !ok {
			return tool.ErrorResult(fmt.Sprintf("task_update: updates[%d] must be an object", i)), nil
		}
		id, _ := m["id"].(string)
		if id == "" {
			return tool.ErrorResult(fmt.Sprintf("task_update: updates[%d].id is required", i)), nil
		}
		var p Patch
		if v, ok := m["status"]; ok {
			s, _ := v.(string)
			st, err := parseStatus(s)
			if err != nil {
				return tool.ErrorResult(fmt.Sprintf("task_update: updates[%d].status: %s", i, err.Error())), nil
			}
			p.Status = &st
		}
		if v, ok := m["content"]; ok {
			s, _ := v.(string)
			if strings.TrimSpace(s) == "" {
				return tool.ErrorResult(fmt.Sprintf("task_update: updates[%d].content is empty", i)), nil
			}
			p.Content = &s
		}
		if v, ok := m["activeForm"]; ok {
			s, _ := v.(string)
			p.ActiveForm = &s
		}
		if v, ok := m["priority"]; ok {
			s, _ := v.(string)
			pr, err := parsePriority(s)
			if err != nil {
				return tool.ErrorResult(fmt.Sprintf("task_update: updates[%d].priority: %s", i, err.Error())), nil
			}
			p.Priority = &pr
		}
		queued = append(queued, pending{id: id, patch: p})
	}

	// 先全量校验 id 存在，再统一应用，避免出现"改了一半失败"的中间态。
	for _, q := range queued {
		if _, ok := cfg.store.GetItem(q.id); !ok {
			return tool.ErrorResult("task_update: id " + quote(q.id) + " not found"), nil
		}
	}
	updated := make([]Item, 0, len(queued))
	for _, q := range queued {
		it, ok := cfg.store.UpdatePatch(q.id, q.patch)
		if !ok {
			return tool.ErrorResult("task_update: id " + quote(q.id) + " was deleted concurrently"), nil
		}
		updated = append(updated, it)
	}

	body := "Updated " + plural(len(updated), "task", "tasks") + ":\n" + formatItems(updated) +
		"\n\n" + formatList(cfg.store.Snapshot())
	return tool.TextResult(body), nil
}
