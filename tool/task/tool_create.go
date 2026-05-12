package task

import (
	"context"
	"fmt"
	"strings"

	agent "github.com/cago-frame/agents/agent"
	"github.com/cago-frame/agents/tool"
)

// CreateName 是 task_create 暴露给 LLM 的工具名。
const CreateName = "task_create"

// CreateDescription 暴露给 LLM 的工具描述。
const CreateDescription = "Append one or more tasks to the task list. IDs are server-assigned (t1, t2, …) and returned in the result — reuse them with task_update / task_get / task_delete."

// NewCreate 构造 task_create 工具。
func NewCreate(opts ...Option) tool.Tool {
	cfg := newConfig(opts...)
	return &tool.RawTool{
		NameStr:   CreateName,
		DescStr:   CreateDescription,
		SchemaVal: createSchema(),
		IsSerial:  cfg.serial,
		Handler: func(ctx context.Context, in map[string]any) (*agent.ToolResultBlock, error) {
			return runCreate(ctx, cfg, in)
		},
		PromptSnippetStr: "Append tasks (server assigns ids).",
		PromptGuidelinesArr: []string{
			"Break multi-step work (>=3 steps) into tasks via task_create; status defaults to pending.",
		},
	}
}

func createSchema() agent.Schema {
	noAdditional := false
	return agent.Schema{
		Type:     "object",
		Required: []string{"tasks"},
		Properties: map[string]*agent.Property{
			"tasks": {
				Type:        "array",
				Description: "New tasks to append. Existing tasks are untouched. Each task gets a fresh server-assigned id (t1, t2, …) in the result.",
				Items: &agent.Property{
					Type: "object",
					Properties: map[string]*agent.Property{
						"content":    {Type: "string", Description: "What the task is about (imperative form)."},
						"activeForm": {Type: "string", Description: "Active-tense form shown when the task is in_progress."},
						"priority":   {Type: "string", Enum: []any{"high", "medium", "low"}},
					},
					Required:             []string{"content"},
					AdditionalProperties: &noAdditional,
				},
			},
		},
	}
}

func runCreate(ctx context.Context, cfg *config, in map[string]any) (*agent.ToolResultBlock, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	raw, ok := in["tasks"]
	if !ok {
		return tool.ErrorResult("task_create: missing required field: tasks"), nil
	}
	slice, ok := raw.([]any)
	if !ok {
		return tool.ErrorResult("task_create: tasks must be an array"), nil
	}
	if len(slice) == 0 {
		return tool.ErrorResult("task_create: tasks is empty (provide at least one)"), nil
	}

	items := make([]Item, 0, len(slice))
	for i, rawItem := range slice {
		m, ok := rawItem.(map[string]any)
		if !ok {
			return tool.ErrorResult(fmt.Sprintf("task_create: tasks[%d] must be an object", i)), nil
		}
		content, _ := m["content"].(string)
		if strings.TrimSpace(content) == "" {
			return tool.ErrorResult(fmt.Sprintf("task_create: tasks[%d].content is empty", i)), nil
		}
		priorityStr, _ := m["priority"].(string)
		pr, err := parsePriority(priorityStr)
		if err != nil {
			return tool.ErrorResult(fmt.Sprintf("task_create: tasks[%d].priority: %s", i, err.Error())), nil
		}
		activeForm, _ := m["activeForm"].(string)
		items = append(items, Item{
			Content:    content,
			Status:     StatusPending,
			ActiveForm: activeForm,
			Priority:   pr,
		})
	}

	created := cfg.store.AddItems(items)
	body := "Created " + plural(len(created), "task", "tasks") + ":\n" + formatItems(created) +
		"\n\n" + formatList(cfg.store.Snapshot())
	return tool.TextResult(body), nil
}

func plural(n int, singular, plural string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, singular)
	}
	return fmt.Sprintf("%d %s", n, plural)
}
