package task

import (
	"context"
	"fmt"

	agent "github.com/cago-frame/agents/agent"
	"github.com/cago-frame/agents/tool"
)

// DeleteName 是 task_delete 暴露给 LLM 的工具名。
const DeleteName = "task_delete"

// DeleteDescription 暴露给 LLM 的工具描述。
const DeleteDescription = "Delete tasks by id, or pass clear=true to wipe the list. Exactly one of ids / clear must be set."

// NewDelete 构造 task_delete 工具。
func NewDelete(opts ...Option) tool.Tool {
	cfg := newConfig(opts...)
	return &tool.RawTool{
		NameStr:   DeleteName,
		DescStr:   DeleteDescription,
		SchemaVal: deleteSchema(),
		IsSerial:  cfg.serial,
		Handler: func(ctx context.Context, in map[string]any) (*agent.ToolResultBlock, error) {
			return runDelete(ctx, cfg, in)
		},
		PromptSnippetStr: "Remove tasks by id or clear the whole list.",
		PromptGuidelinesArr: []string{
			"Prefer task_update {status: completed} over task_delete; only delete tasks that turned out to be irrelevant.",
		},
	}
}

func deleteSchema() agent.Schema {
	return agent.Schema{
		Type: "object",
		Properties: map[string]*agent.Property{
			"ids": {
				Type:        "array",
				Description: "Task ids to remove. Unknown ids are reported as an error.",
				Items:       &agent.Property{Type: "string"},
			},
			"clear": {
				Type:        "boolean",
				Description: "If true, wipe the entire task list. Mutually exclusive with ids.",
			},
		},
	}
}

func runDelete(ctx context.Context, cfg *config, in map[string]any) (*agent.ToolResultBlock, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	doClear, _ := in["clear"].(bool)
	rawIDs, hasIDs := in["ids"]

	switch {
	case doClear && hasIDs:
		return tool.ErrorResult("task_delete: pass either ids or clear, not both"), nil
	case !doClear && !hasIDs:
		return tool.ErrorResult("task_delete: missing both ids and clear (pass one)"), nil
	}

	if doClear {
		n := cfg.store.Clear()
		body := fmt.Sprintf("Cleared %d task(s).\n\n%s", n, formatList(cfg.store.Snapshot()))
		return tool.TextResult(body), nil
	}

	slice, ok := rawIDs.([]any)
	if !ok {
		return tool.ErrorResult("task_delete: ids must be an array of strings"), nil
	}
	if len(slice) == 0 {
		return tool.ErrorResult("task_delete: ids is empty"), nil
	}
	ids := make([]string, 0, len(slice))
	for i, raw := range slice {
		s, ok := raw.(string)
		if !ok || s == "" {
			return tool.ErrorResult(fmt.Sprintf("task_delete: ids[%d] must be a non-empty string", i)), nil
		}
		ids = append(ids, s)
	}
	// 全量校验存在再删，避免"一半成功"。
	for _, id := range ids {
		if _, ok := cfg.store.GetItem(id); !ok {
			return tool.ErrorResult("task_delete: id " + quote(id) + " not found"), nil
		}
	}
	removed := cfg.store.DeleteIDs(ids)
	body := fmt.Sprintf("Deleted %d task(s).\n\n%s", removed, formatList(cfg.store.Snapshot()))
	return tool.TextResult(body), nil
}
