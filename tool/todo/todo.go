// Package todo 把 Claude Code 的 TodoWrite 包成一个 agent.Tool。
//
// 语义: replace-all。每次调用传入完整的当前 todo 列表,整体覆盖之前的状态。
// 没有单独的 add / update / delete 动作:
//
//   - 新增: 在新列表里追加一项。
//   - 修改: 用相同 id (或同位置) 传新内容。
//   - 删除: 在新列表里省略那一项即可。
//
// 状态由 *List 持有, *agent.Session 通常配一个 *List, 让多轮对话共享 todo。
package todo

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	agent "github.com/cago-frame/agents/agent"
	"github.com/cago-frame/agents/tool"
)

// Name 是工具暴露给 LLM 的名字。
const Name = "todo_write"

// Description 暴露给 LLM 的工具描述。
const Description = "Manage a structured task list. Provide the COMPLETE current todo list — items omitted will be deleted. Use this for multi-step tasks (>=3 steps) to track progress."

// Status 任务状态。
type Status string

// 三档状态。
const (
	StatusPending    Status = "pending"
	StatusInProgress Status = "in_progress"
	StatusCompleted  Status = "completed"
)

// Priority 任务优先级。
type Priority string

// 三档优先级。
const (
	PriorityHigh   Priority = "high"
	PriorityMedium Priority = "medium"
	PriorityLow    Priority = "low"
)

// Item 一条 todo。
type Item struct {
	ID         string    `json:"id,omitempty"`
	Content    string    `json:"content"`
	Status     Status    `json:"status"`
	ActiveForm string    `json:"activeForm,omitempty"`
	Priority   Priority  `json:"priority,omitempty"`
	UpdatedAt  time.Time `json:"updated_at,omitzero"`
}

// List 持有当前 todo 状态。线程安全,多 tool call 共享同一 *List。
type List struct {
	mu    sync.Mutex
	items []Item
}

// NewList 构造一个空 List。
func NewList() *List { return &List{} }

// Snapshot 返回当前列表的拷贝。
func (l *List) Snapshot() []Item {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make([]Item, len(l.items))
	copy(out, l.items)
	return out
}

// Replace 全量替换列表。每项打上当前时间戳便于宿主排序。
func (l *List) Replace(items []Item) {
	now := time.Now()
	for i := range items {
		items[i].UpdatedAt = now
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.items = items
}

// Option 构造选项。
type Option func(*config)

type config struct {
	list   *List
	serial bool
}

// WithList 共享外部 *List 状态。零值时 New 内部新建一个匿名 List(外部无法观察)。
// With 前缀避免与包级 type List 标识符冲突。
func WithList(l *List) Option { return func(c *config) { c.list = l } }

// Serial 让该工具与同一轮的其他工具串行执行。
func Serial() Option { return func(c *config) { c.serial = true } }

// New 构造 todo_write 工具。
func New(opts ...Option) tool.Tool {
	cfg := &config{}
	for _, o := range opts {
		o(cfg)
	}
	if cfg.list == nil {
		cfg.list = NewList()
	}
	return &tool.RawTool{
		NameStr:   Name,
		DescStr:   Description,
		SchemaVal: schema(),
		IsSerial:  cfg.serial,
		Handler: func(ctx context.Context, in map[string]any) (*agent.ToolResultBlock, error) {
			return run(ctx, cfg, in)
		},
		PromptSnippetStr: "Manage a structured todo list visible to the user.",
		PromptGuidelinesArr: []string{
			"Break work into steps; mark items in_progress when starting and completed when done.",
		},
	}
}

func schema() agent.Schema {
	noAdditional := false
	return agent.Schema{
		Type:     "object",
		Required: []string{"todos"},
		Properties: map[string]*agent.Property{
			"todos": {
				Type:        "array",
				Description: "Provide the COMPLETE current todo list. This call REPLACES the previous list entirely — items omitted here will be deleted.",
				Items: &agent.Property{
					Type: "object",
					Properties: map[string]*agent.Property{
						"id":         {Type: "string", Description: "Stable id; reuse across updates to keep the same task"},
						"content":    {Type: "string"},
						"status":     {Type: "string", Enum: []any{"pending", "in_progress", "completed"}},
						"activeForm": {Type: "string", Description: "Active-tense form shown when status=in_progress"},
						"priority":   {Type: "string", Enum: []any{"high", "medium", "low"}},
					},
					Required:             []string{"content", "status"},
					AdditionalProperties: &noAdditional,
				},
			},
		},
	}
}

func run(ctx context.Context, cfg *config, in map[string]any) (*agent.ToolResultBlock, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	todosRaw, ok := in["todos"]
	if !ok {
		return tool.ErrorResult("todo_write: missing required field: todos"), nil
	}
	todosSlice, ok := todosRaw.([]any)
	if !ok {
		return tool.ErrorResult("todo_write: todos must be an array"), nil
	}

	items := make([]Item, 0, len(todosSlice))
	for i, rawItem := range todosSlice {
		m, ok := rawItem.(map[string]any)
		if !ok {
			return tool.ErrorResult(fmt.Sprintf("todo_write: todos[%d] must be an object", i)), nil
		}

		content, _ := m["content"].(string)
		if strings.TrimSpace(content) == "" {
			return tool.ErrorResult(fmt.Sprintf("todo_write: todos[%d].content is empty", i)), nil
		}

		statusStr, _ := m["status"].(string)
		st, err := parseStatus(statusStr)
		if err != nil {
			return tool.ErrorResult(fmt.Sprintf("todo_write: todos[%d].status: %s", i, err.Error())), nil
		}

		priorityStr, _ := m["priority"].(string)
		pr, err := parsePriority(priorityStr)
		if err != nil {
			return tool.ErrorResult(fmt.Sprintf("todo_write: todos[%d].priority: %s", i, err.Error())), nil
		}

		id, _ := m["id"].(string)
		activeForm, _ := m["activeForm"].(string)

		items = append(items, Item{
			ID:         id,
			Content:    content,
			Status:     st,
			ActiveForm: activeForm,
			Priority:   pr,
		})
	}

	cfg.list.Replace(items)
	return tool.TextResult(formatList(cfg.list.Snapshot())), nil
}

func parseStatus(s string) (Status, error) {
	switch Status(s) {
	case StatusPending, StatusInProgress, StatusCompleted:
		return Status(s), nil
	}
	return "", fmt.Errorf("invalid status %q (want pending|in_progress|completed)", s)
}

func parsePriority(s string) (Priority, error) {
	if s == "" {
		return "", nil
	}
	switch Priority(s) {
	case PriorityHigh, PriorityMedium, PriorityLow:
		return Priority(s), nil
	}
	return "", fmt.Errorf("invalid priority %q (want high|medium|low)", s)
}

func formatList(items []Item) string {
	if len(items) == 0 {
		return "[empty todo list]"
	}
	var b strings.Builder
	var pending, inProgress, completed int
	for _, it := range items {
		var prefix string
		switch it.Status {
		case StatusPending:
			prefix = "☐"
			pending++
		case StatusInProgress:
			prefix = "▶"
			inProgress++
		case StatusCompleted:
			prefix = "✓"
			completed++
		}
		text := it.Content
		if it.Status == StatusInProgress && it.ActiveForm != "" {
			text = it.ActiveForm
		}
		fmt.Fprintf(&b, "%s %s", prefix, text)
		if it.Priority != "" {
			fmt.Fprintf(&b, " [%s]", it.Priority)
		}
		b.WriteByte('\n')
	}
	fmt.Fprintf(&b, "\n%d pending, %d in progress, %d completed (total %d)",
		pending, inProgress, completed, len(items))
	return b.String()
}
