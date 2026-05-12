package todo_test

import (
	"context"
	"strings"
	"testing"

	agent "github.com/cago-frame/agents/agent"
	"github.com/cago-frame/agents/tool"
	"github.com/cago-frame/agents/tool/todo"
)

func callTool(t *testing.T, tl tool.Tool, args map[string]any) (*agent.ToolResultBlock, error) {
	t.Helper()
	return tl.Call(context.Background(), args)
}

func mustCall(t *testing.T, tl tool.Tool, args map[string]any) string {
	t.Helper()
	out, err := callTool(t, tl, args)
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	if out.IsError {
		t.Fatalf("tool error: %s", out.Content[0].(agent.TextBlock).Text)
	}
	return out.Content[0].(agent.TextBlock).Text
}

func TestTodoEmptyAck(t *testing.T) {
	out := mustCall(t, todo.New(), map[string]any{"todos": []any{}})
	if !strings.Contains(out, "empty todo list") {
		t.Fatalf("got %q", out)
	}
}

func TestTodoReplaceAndFormat(t *testing.T) {
	list := todo.NewList()
	tl := todo.New(todo.WithList(list))
	out := mustCall(t, tl, map[string]any{"todos": []any{
		map[string]any{"id": "1", "content": "Write tests", "status": "in_progress", "activeForm": "Writing tests"},
		map[string]any{"id": "2", "content": "Run lint", "status": "pending", "priority": "high"},
		map[string]any{"id": "3", "content": "Ship", "status": "completed"},
	}})

	if !strings.Contains(out, "▶ Writing tests") {
		t.Fatalf("missing in-progress activeForm: %q", out)
	}
	if !strings.Contains(out, "☐ Run lint [high]") {
		t.Fatalf("missing pending with priority: %q", out)
	}
	if !strings.Contains(out, "✓ Ship") {
		t.Fatalf("missing completed: %q", out)
	}
	if !strings.Contains(out, "1 pending, 1 in progress, 1 completed (total 3)") {
		t.Fatalf("missing summary: %q", out)
	}

	snap := list.Snapshot()
	if len(snap) != 3 {
		t.Fatalf("snapshot len: %d", len(snap))
	}
}

func TestTodoReplaceAllDeleteSemantics(t *testing.T) {
	list := todo.NewList()
	tl := todo.New(todo.WithList(list))

	mustCall(t, tl, map[string]any{"todos": []any{
		map[string]any{"id": "a", "content": "A", "status": "pending"},
		map[string]any{"id": "b", "content": "B", "status": "pending"},
		map[string]any{"id": "c", "content": "C", "status": "pending"},
	}})
	if len(list.Snapshot()) != 3 {
		t.Fatalf("expected 3 items")
	}

	// 第二次只传 a 和 c — b 应该消失
	mustCall(t, tl, map[string]any{"todos": []any{
		map[string]any{"id": "a", "content": "A", "status": "completed"},
		map[string]any{"id": "c", "content": "C", "status": "pending"},
	}})
	snap := list.Snapshot()
	if len(snap) != 2 {
		t.Fatalf("expected 2 after replace-all, got %d", len(snap))
	}
	for _, it := range snap {
		if it.ID == "b" {
			t.Fatalf("b should have been deleted but still in snapshot")
		}
	}
}

func TestTodoInvalidStatus(t *testing.T) {
	out, err := callTool(t, todo.New(), map[string]any{"todos": []any{
		map[string]any{"content": "x", "status": "bogus"},
	}})
	if err != nil {
		t.Fatalf("unexpected Go error: %v", err)
	}
	if !out.IsError {
		t.Fatal("expected IsError=true")
	}
	if !strings.Contains(out.Content[0].(agent.TextBlock).Text, "invalid status") {
		t.Fatalf("got %q", out.Content[0].(agent.TextBlock).Text)
	}
}

func TestTodoInvalidPriority(t *testing.T) {
	out, err := callTool(t, todo.New(), map[string]any{"todos": []any{
		map[string]any{"content": "x", "status": "pending", "priority": "urgent"},
	}})
	if err != nil {
		t.Fatalf("unexpected Go error: %v", err)
	}
	if !out.IsError {
		t.Fatal("expected IsError=true")
	}
	if !strings.Contains(out.Content[0].(agent.TextBlock).Text, "invalid priority") {
		t.Fatalf("got %q", out.Content[0].(agent.TextBlock).Text)
	}
}

func TestTodoEmptyContentRejected(t *testing.T) {
	out, err := callTool(t, todo.New(), map[string]any{"todos": []any{
		map[string]any{"content": "  ", "status": "pending"},
	}})
	if err != nil {
		t.Fatalf("unexpected Go error: %v", err)
	}
	if !out.IsError {
		t.Fatal("expected IsError=true")
	}
	if !strings.Contains(out.Content[0].(agent.TextBlock).Text, "content is empty") {
		t.Fatalf("got %q", out.Content[0].(agent.TextBlock).Text)
	}
}

func TestTodoSnapshotIsCopy(t *testing.T) {
	list := todo.NewList()
	tl := todo.New(todo.WithList(list))
	mustCall(t, tl, map[string]any{"todos": []any{
		map[string]any{"id": "a", "content": "A", "status": "pending"},
	}})
	snap := list.Snapshot()
	snap[0].Content = "MUTATED"

	// 第二次拿快照,应该没受影响
	snap2 := list.Snapshot()
	if snap2[0].Content != "A" {
		t.Fatalf("snapshot is not a copy; got %q", snap2[0].Content)
	}
}

func TestTodoListReplaceDirect(t *testing.T) {
	list := todo.NewList()
	list.Replace([]todo.Item{{ID: "x", Content: "X", Status: todo.StatusPending}})
	if len(list.Snapshot()) != 1 {
		t.Fatal("direct Replace failed")
	}
}

func TestTodoSchemaFields(t *testing.T) {
	tl := todo.New()
	if tl.Name() != todo.Name {
		t.Fatalf("name = %q", tl.Name())
	}
	sc := tl.Schema()
	if sc.Type != "object" {
		t.Fatalf("schema type = %q", sc.Type)
	}
	if len(sc.Properties) == 0 {
		t.Fatal("schema properties empty")
	}
}

func TestTodoUpdatedAtSet(t *testing.T) {
	list := todo.NewList()
	tl := todo.New(todo.WithList(list))
	mustCall(t, tl, map[string]any{"todos": []any{
		map[string]any{"content": "A", "status": "pending"},
	}})
	snap := list.Snapshot()
	if snap[0].UpdatedAt.IsZero() {
		t.Fatal("UpdatedAt not set")
	}
}

func TestTodoSerial(t *testing.T) {
	tl := todo.New(todo.Serial())
	if !tl.(agent.SerialTool).Serial() {
		t.Fatal("expected Serial()=true")
	}
}
