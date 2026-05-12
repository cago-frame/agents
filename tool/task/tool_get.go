package task

import (
	"context"

	agent "github.com/cago-frame/agents/agent"
	"github.com/cago-frame/agents/tool"
)

// GetName 是 task_get 暴露给 LLM 的工具名。
const GetName = "task_get"

// GetDescription 暴露给 LLM 的工具描述。
const GetDescription = "Fetch a single task by id. Returns an error if the id is unknown — call task_list to re-sync."

// NewGet 构造 task_get 工具。
func NewGet(opts ...Option) tool.Tool {
	cfg := newConfig(opts...)
	return &tool.RawTool{
		NameStr:   GetName,
		DescStr:   GetDescription,
		SchemaVal: getSchema(),
		IsSerial:  cfg.serial,
		Handler: func(ctx context.Context, in map[string]any) (*agent.ToolResultBlock, error) {
			return runGet(ctx, cfg, in)
		},
		PromptSnippetStr: "Read a single task by id.",
		PromptGuidelinesArr: []string{
			"Use task_get to confirm a task's current fields before mutating it.",
		},
	}
}

func getSchema() agent.Schema {
	return agent.Schema{
		Type:     "object",
		Required: []string{"id"},
		Properties: map[string]*agent.Property{
			"id": {Type: "string", Description: "Task id assigned by task_create (t1, t2, …)."},
		},
	}
}

func runGet(ctx context.Context, cfg *config, in map[string]any) (*agent.ToolResultBlock, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	id, _ := in["id"].(string)
	if id == "" {
		return tool.ErrorResult("task_get: missing required field: id"), nil
	}
	it, ok := cfg.store.GetItem(id)
	if !ok {
		return tool.ErrorResult("task_get: id " + quote(id) + " not found"), nil
	}
	return tool.TextResult(formatItem(it)), nil
}

func quote(s string) string { return "\"" + s + "\"" }
