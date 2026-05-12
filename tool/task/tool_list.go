package task

import (
	"context"

	agent "github.com/cago-frame/agents/agent"
	"github.com/cago-frame/agents/tool"
)

// ListName 是 task_list 暴露给 LLM 的工具名。
const ListName = "task_list"

// ListDescription 暴露给 LLM 的工具描述。
const ListDescription = "List current tasks. Pass status=pending|in_progress|completed to filter; omit for all. Use this to recover ids you may have forgotten."

// NewList 构造 task_list 工具。
func NewList(opts ...Option) tool.Tool {
	cfg := newConfig(opts...)
	return &tool.RawTool{
		NameStr:   ListName,
		DescStr:   ListDescription,
		SchemaVal: listSchema(),
		IsSerial:  cfg.serial,
		Handler: func(ctx context.Context, in map[string]any) (*agent.ToolResultBlock, error) {
			return runList(ctx, cfg, in)
		},
		PromptSnippetStr: "Read the current task list (optionally filtered by status).",
		PromptGuidelinesArr: []string{
			"When unsure of current task ids or state, call task_list before task_update / task_delete.",
		},
	}
}

func listSchema() agent.Schema {
	return agent.Schema{
		Type: "object",
		Properties: map[string]*agent.Property{
			"status": {
				Type:        "string",
				Description: "Optional status filter. Omit to return everything.",
				Enum:        []any{"pending", "in_progress", "completed"},
			},
		},
	}
}

func runList(ctx context.Context, cfg *config, in map[string]any) (*agent.ToolResultBlock, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	statusStr, _ := in["status"].(string)
	snap := cfg.store.Snapshot()
	if statusStr == "" {
		return tool.TextResult(formatList(snap)), nil
	}
	st, perr := parseStatus(statusStr)
	if perr != nil {
		return tool.ErrorResult("task_list: " + perr.Error()), nil //nolint:nilerr // 把校验失败投影成工具层 error block，不向上抛 Go error
	}
	filtered := make([]Item, 0, len(snap))
	for _, it := range snap {
		if it.Status == st {
			filtered = append(filtered, it)
		}
	}
	return tool.TextResult(formatList(filtered)), nil
}
