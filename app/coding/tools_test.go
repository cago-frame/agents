package coding_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cago-frame/agents/agent"
	"github.com/cago-frame/agents/app/coding"
	"github.com/cago-frame/agents/tool"
)

func findTool(tools []tool.Tool, name string) tool.Tool {
	for _, t := range tools {
		if t.Name() == name {
			return t
		}
	}
	return nil
}

// resultText extracts the concatenated text content from a ToolResultBlock.
func resultText(b *agent.ToolResultBlock) string {
	if b == nil {
		return ""
	}
	var sb strings.Builder
	for _, c := range b.Content {
		if t, ok := c.(agent.TextBlock); ok {
			sb.WriteString(t.Text)
		}
	}
	return sb.String()
}

// toolResultText is used by tests that check error blocks.
func toolResultText(b *agent.ToolResultBlock) string { return resultText(b) }

func TestTools_HasSeven(t *testing.T) {
	got := coding.Tools(".")
	if len(got) != 7 {
		t.Fatalf("expected 7 tools, got %d", len(got))
	}
	for _, name := range []string{"read", "write", "edit", "bash", "grep", "find", "ls"} {
		if findTool(got, name) == nil {
			t.Errorf("missing tool: %s", name)
		}
	}
}

func TestReadOnly_HasFour(t *testing.T) {
	got := coding.ReadOnly(".")
	if len(got) != 4 {
		t.Fatalf("expected 4 tools, got %d", len(got))
	}
	for _, name := range []string{"read", "grep", "find", "ls"} {
		if findTool(got, name) == nil {
			t.Errorf("missing tool: %s", name)
		}
	}
}

func TestSession_AllReturnsFourteen(t *testing.T) {
	sess := coding.NewSession(".")
	tools := sess.All()
	if len(tools) != 14 {
		t.Fatalf("expected 14 tools, got %d", len(tools))
	}
	for _, n := range []string{
		"read", "write", "edit",
		"bash", "bash_output", "kill_shell",
		"grep", "find", "ls",
		"task_create", "task_list", "task_get", "task_update", "task_delete",
	} {
		if findTool(tools, n) == nil {
			t.Errorf("missing tool: %s", n)
		}
	}
}

func TestSession_CodingHasBashTrioNoTask(t *testing.T) {
	tools := coding.NewSession(".").Coding()
	for _, n := range []string{"bash", "bash_output", "kill_shell"} {
		if findTool(tools, n) == nil {
			t.Errorf("missing %s in Coding()", n)
		}
	}
	for _, n := range []string{"task_create", "task_list", "task_get", "task_update", "task_delete"} {
		if findTool(tools, n) != nil {
			t.Errorf("%s should not be in Coding()", n)
		}
	}
}

func TestSession_ReadOnlyHasFourNoTask(t *testing.T) {
	tools := coding.NewSession(".").ReadOnly()
	if len(tools) != 4 {
		t.Fatalf("expected 4 tools, got %d", len(tools))
	}
	for _, n := range []string{"task_create", "task_list", "task_get", "task_update", "task_delete"} {
		if findTool(tools, n) != nil {
			t.Errorf("%s should not be in ReadOnly()", n)
		}
	}
}

func TestSession_EditEnforcesReadFirst(t *testing.T) {
	dir := t.TempDir()
	sess := coding.NewSession(dir)
	tools := sess.Coding()

	writeT := findTool(tools, "write")
	editT := findTool(tools, "edit")

	if _, err := writeT.Call(context.Background(), map[string]any{"path": "x.txt", "content": "alpha"}); err != nil {
		t.Fatalf("write: %v", err)
	}
	res, err := editT.Call(context.Background(), map[string]any{
		"path":  "x.txt",
		"edits": []any{map[string]any{"oldText": "alpha", "newText": "ALPHA"}},
	})
	if err != nil {
		t.Fatalf("edit returned unexpected error: %v", err)
	}
	if !res.IsError || !strings.Contains(toolResultText(res), "has not been read") {
		t.Fatalf("expected read-first enforcement, got isError=%v text=%q", res.IsError, toolResultText(res))
	}
}

func TestSession_EditAfterReadOK(t *testing.T) {
	dir := t.TempDir()
	sess := coding.NewSession(dir)
	tools := sess.Coding()
	readT := findTool(tools, "read")
	writeT := findTool(tools, "write")
	editT := findTool(tools, "edit")

	if _, err := writeT.Call(context.Background(), map[string]any{"path": "x.txt", "content": "alpha"}); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := readT.Call(context.Background(), map[string]any{"path": "x.txt"}); err != nil {
		t.Fatalf("read: %v", err)
	}
	if _, err := editT.Call(context.Background(), map[string]any{
		"path":  "x.txt",
		"edits": []any{map[string]any{"oldText": "alpha", "newText": "ALPHA"}},
	}); err != nil {
		t.Fatalf("edit after read: %v", err)
	}
}

func TestSession_AcceptsRelativeAndAbsolute(t *testing.T) {
	dir := t.TempDir()
	abs := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(abs, []byte("alpha"), 0o600); err != nil {
		t.Fatal(err)
	}
	sess := coding.NewSession(dir)
	tools := sess.Coding()
	readT := findTool(tools, "read")
	editT := findTool(tools, "edit")

	if _, err := readT.Call(context.Background(), map[string]any{"path": "a.txt"}); err != nil {
		t.Fatalf("read relative: %v", err)
	}
	if _, err := editT.Call(context.Background(), map[string]any{
		"path": abs,
		"edits": []any{map[string]any{
			"oldText": "alpha", "newText": "ALPHA",
		}},
	}); err != nil {
		t.Fatalf("edit absolute after read relative: %v", err)
	}
}

// Verify JobManager shared across method calls — old tool/preset bug.
func TestSession_JobManagerSharedAcrossMethodCalls(t *testing.T) {
	dir := t.TempDir()
	sess := coding.NewSession(dir)
	bashTool := findTool(sess.Coding(), "bash")
	outputTool := findTool(sess.All(), "bash_output")

	resp, err := bashTool.Call(context.Background(), map[string]any{
		"command":           "echo hi",
		"run_in_background": true,
	})
	if err != nil {
		t.Fatalf("bash bg: %v", err)
	}
	if resp == nil {
		t.Fatal("bash bg returned nil result")
	}
	msg := resultText(resp)
	_, rest, ok2 := strings.Cut(msg, `shell_id="`)
	if !ok2 {
		t.Fatalf("no shell_id in resp: %s", msg)
	}
	shellID, _, ok2 := strings.Cut(rest, `"`)
	if !ok2 {
		t.Fatalf("malformed shell_id in resp: %s", msg)
	}

	if _, err := outputTool.Call(context.Background(), map[string]any{"shell_id": shellID}); err != nil {
		t.Fatalf("bash_output cross-call: %v", err)
	}
}

func TestSession_TasksSharedAcrossCalls(t *testing.T) {
	sess := coding.NewSession(".")
	createTool := findTool(sess.All(), "task_create")
	if createTool == nil {
		t.Fatal("task_create missing")
	}
	if _, err := createTool.Call(context.Background(), map[string]any{
		"tasks": []any{
			map[string]any{"content": "from session"},
		},
	}); err != nil {
		t.Fatalf("call: %v", err)
	}
	if got := sess.Tasks().Snapshot(); len(got) != 1 || got[0].Content != "from session" {
		t.Fatalf("session task not shared: %+v", got)
	}

	// task_list / task_update / task_delete 也都挂同一 Store
	listTool := findTool(sess.All(), "task_list")
	updTool := findTool(sess.All(), "task_update")
	delTool := findTool(sess.All(), "task_delete")
	if listTool == nil || updTool == nil || delTool == nil {
		t.Fatal("task_list / task_update / task_delete missing")
	}
	got := sess.Tasks().Snapshot()
	id := got[0].ID
	if _, err := updTool.Call(context.Background(), map[string]any{
		"updates": []any{map[string]any{"id": id, "status": "completed"}},
	}); err != nil {
		t.Fatalf("task_update: %v", err)
	}
	if sess.Tasks().Snapshot()[0].Status != "completed" {
		t.Fatalf("session task store not shared with task_update")
	}
	if _, err := delTool.Call(context.Background(), map[string]any{"clear": true}); err != nil {
		t.Fatalf("task_delete: %v", err)
	}
	if len(sess.Tasks().Snapshot()) != 0 {
		t.Fatalf("session task store not shared with task_delete")
	}
}
