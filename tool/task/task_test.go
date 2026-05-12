package task_test

import (
	"context"
	"strings"
	"testing"

	agent "github.com/cago-frame/agents/agent"
	"github.com/cago-frame/agents/tool"
	"github.com/cago-frame/agents/tool/task"
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

func mustError(t *testing.T, tl tool.Tool, args map[string]any, wantSubstr string) {
	t.Helper()
	out, err := callTool(t, tl, args)
	if err != nil {
		t.Fatalf("unexpected Go error: %v", err)
	}
	if !out.IsError {
		t.Fatalf("expected IsError=true, got %q", out.Content[0].(agent.TextBlock).Text)
	}
	msg := out.Content[0].(agent.TextBlock).Text
	if !strings.Contains(msg, wantSubstr) {
		t.Fatalf("expected error containing %q, got %q", wantSubstr, msg)
	}
}

func TestSuite_SharesStore(t *testing.T) {
	store := task.NewStore()
	tools := task.NewSuite(task.WithStore(store))
	if len(tools) != 5 {
		t.Fatalf("expected 5 tools, got %d", len(tools))
	}
	names := map[string]bool{}
	for _, tl := range tools {
		names[tl.Name()] = true
	}
	for _, want := range []string{"task_create", "task_list", "task_get", "task_update", "task_delete"} {
		if !names[want] {
			t.Errorf("missing tool %s", want)
		}
	}
}

func TestCreate_AssignsServerIDs(t *testing.T) {
	store := task.NewStore()
	create := task.NewCreate(task.WithStore(store))
	out := mustCall(t, create, map[string]any{"tasks": []any{
		map[string]any{"content": "Alpha"},
		map[string]any{"content": "Beta", "priority": "high"},
	}})
	if !strings.Contains(out, "t1 ☐ Alpha") {
		t.Errorf("expected t1 Alpha row, got %q", out)
	}
	if !strings.Contains(out, "t2 ☐ Beta [high]") {
		t.Errorf("expected t2 Beta with priority, got %q", out)
	}
	snap := store.Snapshot()
	if len(snap) != 2 || snap[0].ID != "t1" || snap[1].ID != "t2" {
		t.Fatalf("unexpected store state: %+v", snap)
	}
	if snap[0].Status != task.StatusPending {
		t.Errorf("default status should be pending, got %q", snap[0].Status)
	}
	if snap[0].CreatedAt.IsZero() || snap[0].UpdatedAt.IsZero() {
		t.Error("CreatedAt / UpdatedAt should be set")
	}
}

func TestCreate_AppendsWithoutWiping(t *testing.T) {
	store := task.NewStore()
	create := task.NewCreate(task.WithStore(store))
	mustCall(t, create, map[string]any{"tasks": []any{map[string]any{"content": "A"}}})
	mustCall(t, create, map[string]any{"tasks": []any{map[string]any{"content": "B"}}})
	snap := store.Snapshot()
	if len(snap) != 2 {
		t.Fatalf("expected 2 items (append, not replace), got %d", len(snap))
	}
	if snap[0].ID != "t1" || snap[1].ID != "t2" {
		t.Errorf("ids should keep increasing: %+v", snap)
	}
}

func TestCreate_ValidationErrors(t *testing.T) {
	create := task.NewCreate()
	mustError(t, create, map[string]any{}, "missing required field: tasks")
	mustError(t, create, map[string]any{"tasks": "not-an-array"}, "tasks must be an array")
	mustError(t, create, map[string]any{"tasks": []any{}}, "tasks is empty")
	mustError(t, create, map[string]any{"tasks": []any{map[string]any{"content": "   "}}}, "content is empty")
	mustError(t, create, map[string]any{"tasks": []any{map[string]any{"content": "x", "priority": "urgent"}}}, "invalid priority")
}

func TestList_FilterByStatus(t *testing.T) {
	store := task.NewStore()
	store.Replace([]task.Item{
		{Content: "A", Status: task.StatusPending},
		{Content: "B", Status: task.StatusInProgress},
		{Content: "C", Status: task.StatusCompleted},
	})
	list := task.NewList(task.WithStore(store))

	all := mustCall(t, list, map[string]any{})
	for _, want := range []string{"A", "B", "C"} {
		if !strings.Contains(all, want) {
			t.Errorf("list all missing %s: %q", want, all)
		}
	}

	onlyPending := mustCall(t, list, map[string]any{"status": "pending"})
	if !strings.Contains(onlyPending, "A") || strings.Contains(onlyPending, "▶") || strings.Contains(onlyPending, "✓") {
		t.Errorf("filter pending wrong: %q", onlyPending)
	}

	empty := mustCall(t, list, map[string]any{"status": "completed"})
	if !strings.Contains(empty, "C") {
		t.Errorf("filter completed missing C: %q", empty)
	}

	mustError(t, list, map[string]any{"status": "bogus"}, "invalid status")
}

func TestGet_FoundAndMissing(t *testing.T) {
	store := task.NewStore()
	create := task.NewCreate(task.WithStore(store))
	get := task.NewGet(task.WithStore(store))

	mustCall(t, create, map[string]any{"tasks": []any{map[string]any{"content": "Hello"}}})
	out := mustCall(t, get, map[string]any{"id": "t1"})
	if !strings.Contains(out, "Hello") {
		t.Errorf("get returned %q", out)
	}

	mustError(t, get, map[string]any{"id": "t999"}, "not found")
	mustError(t, get, map[string]any{}, "missing required field: id")
}

func TestUpdate_PartialPatch(t *testing.T) {
	store := task.NewStore()
	create := task.NewCreate(task.WithStore(store))
	upd := task.NewUpdate(task.WithStore(store))

	mustCall(t, create, map[string]any{"tasks": []any{
		map[string]any{"content": "Write tests", "activeForm": "Writing tests"},
	}})

	// 只改状态，content / activeForm 保留
	out := mustCall(t, upd, map[string]any{"updates": []any{
		map[string]any{"id": "t1", "status": "in_progress"},
	}})
	if !strings.Contains(out, "▶ Writing tests") {
		t.Errorf("expected activeForm rendering: %q", out)
	}
	snap := store.Snapshot()
	if snap[0].Content != "Write tests" {
		t.Errorf("content should not have changed: %q", snap[0].Content)
	}
	if snap[0].Status != task.StatusInProgress {
		t.Errorf("status should be in_progress: %q", snap[0].Status)
	}

	// 改 content
	mustCall(t, upd, map[string]any{"updates": []any{
		map[string]any{"id": "t1", "content": "Write more tests"},
	}})
	if store.Snapshot()[0].Content != "Write more tests" {
		t.Errorf("content did not update: %+v", store.Snapshot())
	}
	if store.Snapshot()[0].Status != task.StatusInProgress {
		t.Errorf("status should not have reverted: %+v", store.Snapshot())
	}
}

func TestUpdate_Batch(t *testing.T) {
	store := task.NewStore()
	create := task.NewCreate(task.WithStore(store))
	upd := task.NewUpdate(task.WithStore(store))

	mustCall(t, create, map[string]any{"tasks": []any{
		map[string]any{"content": "A"},
		map[string]any{"content": "B"},
	}})

	mustCall(t, upd, map[string]any{"updates": []any{
		map[string]any{"id": "t1", "status": "completed"},
		map[string]any{"id": "t2", "status": "in_progress"},
	}})
	snap := store.Snapshot()
	if snap[0].Status != task.StatusCompleted || snap[1].Status != task.StatusInProgress {
		t.Fatalf("batch update failed: %+v", snap)
	}
}

func TestUpdate_UnknownIDIsAtomic(t *testing.T) {
	store := task.NewStore()
	create := task.NewCreate(task.WithStore(store))
	upd := task.NewUpdate(task.WithStore(store))

	mustCall(t, create, map[string]any{"tasks": []any{map[string]any{"content": "A"}}})

	// 第二条 id 不存在 —— 整批应当不应用（保持 t1 还是 pending）
	mustError(t, upd, map[string]any{"updates": []any{
		map[string]any{"id": "t1", "status": "completed"},
		map[string]any{"id": "t999", "status": "completed"},
	}}, "not found")

	if store.Snapshot()[0].Status != task.StatusPending {
		t.Errorf("partial application detected; t1 should remain pending: %+v", store.Snapshot())
	}
}

func TestUpdate_ValidationErrors(t *testing.T) {
	store := task.NewStore()
	create := task.NewCreate(task.WithStore(store))
	upd := task.NewUpdate(task.WithStore(store))
	mustCall(t, create, map[string]any{"tasks": []any{map[string]any{"content": "A"}}})

	mustError(t, upd, map[string]any{}, "missing required field: updates")
	mustError(t, upd, map[string]any{"updates": []any{}}, "updates is empty")
	mustError(t, upd, map[string]any{"updates": []any{map[string]any{"status": "pending"}}}, "id is required")
	mustError(t, upd, map[string]any{"updates": []any{map[string]any{"id": "t1", "status": "bogus"}}}, "invalid status")
	mustError(t, upd, map[string]any{"updates": []any{map[string]any{"id": "t1", "content": "  "}}}, "content is empty")
}

func TestDelete_ByIDs(t *testing.T) {
	store := task.NewStore()
	create := task.NewCreate(task.WithStore(store))
	del := task.NewDelete(task.WithStore(store))

	mustCall(t, create, map[string]any{"tasks": []any{
		map[string]any{"content": "A"},
		map[string]any{"content": "B"},
		map[string]any{"content": "C"},
	}})

	out := mustCall(t, del, map[string]any{"ids": []any{"t1", "t3"}})
	if !strings.Contains(out, "Deleted 2") {
		t.Errorf("expected deletion summary: %q", out)
	}
	snap := store.Snapshot()
	if len(snap) != 1 || snap[0].ID != "t2" {
		t.Fatalf("only t2 should remain: %+v", snap)
	}
}

func TestDelete_Clear(t *testing.T) {
	store := task.NewStore()
	create := task.NewCreate(task.WithStore(store))
	del := task.NewDelete(task.WithStore(store))
	mustCall(t, create, map[string]any{"tasks": []any{map[string]any{"content": "A"}, map[string]any{"content": "B"}}})

	out := mustCall(t, del, map[string]any{"clear": true})
	if !strings.Contains(out, "Cleared 2") {
		t.Errorf("expected clear summary: %q", out)
	}
	if len(store.Snapshot()) != 0 {
		t.Errorf("store should be empty after clear: %+v", store.Snapshot())
	}
}

func TestDelete_ValidationErrors(t *testing.T) {
	store := task.NewStore()
	create := task.NewCreate(task.WithStore(store))
	del := task.NewDelete(task.WithStore(store))
	mustCall(t, create, map[string]any{"tasks": []any{map[string]any{"content": "A"}}})

	mustError(t, del, map[string]any{}, "missing both ids and clear")
	mustError(t, del, map[string]any{"ids": []any{"t1"}, "clear": true}, "not both")
	mustError(t, del, map[string]any{"ids": []any{}}, "ids is empty")
	mustError(t, del, map[string]any{"ids": []any{"t999"}}, "not found")
	if len(store.Snapshot()) != 1 {
		t.Errorf("nothing should have been deleted on the error path: %+v", store.Snapshot())
	}
}

func TestStore_ReplaceRespectsHostIDs(t *testing.T) {
	store := task.NewStore()
	store.Replace([]task.Item{
		{ID: "t5", Content: "X", Status: task.StatusPending},
	})
	create := task.NewCreate(task.WithStore(store))
	out := mustCall(t, create, map[string]any{"tasks": []any{map[string]any{"content": "Y"}}})
	// 下一个分配的 id 应当跳过到 t6
	if !strings.Contains(out, "t6 ☐ Y") {
		t.Errorf("expected next id to be t6 after Replace seeded t5: %q", out)
	}
}

func TestStore_SnapshotIsCopy(t *testing.T) {
	store := task.NewStore()
	store.Replace([]task.Item{{Content: "A", Status: task.StatusPending}})
	snap := store.Snapshot()
	snap[0].Content = "MUTATED"
	if store.Snapshot()[0].Content != "A" {
		t.Errorf("snapshot is not a copy")
	}
}

func TestSerial(t *testing.T) {
	for _, tl := range []tool.Tool{
		task.NewCreate(task.Serial()),
		task.NewList(task.Serial()),
		task.NewGet(task.Serial()),
		task.NewUpdate(task.Serial()),
		task.NewDelete(task.Serial()),
	} {
		st, ok := tl.(agent.SerialTool)
		if !ok || !st.Serial() {
			t.Errorf("%s: expected Serial()=true", tl.Name())
		}
	}
}

func TestSchemaSmokeAndPromptMeta(t *testing.T) {
	for _, tl := range task.NewSuite() {
		sc := tl.Schema()
		if sc.Type != "object" {
			t.Errorf("%s: schema type = %q", tl.Name(), sc.Type)
		}
		pm, ok := tl.(interface {
			PromptSnippet() string
			PromptGuidelines() []string
		})
		if !ok {
			t.Errorf("%s: missing PromptMeta", tl.Name())
			continue
		}
		if pm.PromptSnippet() == "" {
			t.Errorf("%s: empty PromptSnippet", tl.Name())
		}
		if len(pm.PromptGuidelines()) == 0 {
			t.Errorf("%s: empty PromptGuidelines", tl.Name())
		}
	}
}
