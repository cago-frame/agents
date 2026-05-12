package edit_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	agent "github.com/cago-frame/agents/agent"
	"github.com/cago-frame/agents/tool/edit"
	"github.com/cago-frame/agents/tool/state"
)

func TestEditTrackerNotReadRejects(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "a.txt")
	_ = os.WriteFile(p, []byte("hello"), 0o600)
	tr := state.NewReadTracker()
	tl := edit.New(edit.Cwd(dir), edit.Tracker(tr))

	out, err := tl.Call(context.Background(), map[string]any{
		"path":  "a.txt",
		"edits": []map[string]any{{"oldText": "hello", "newText": "HI"}},
	})
	if err != nil {
		t.Fatalf("unexpected go error: %v", err)
	}
	if !out.IsError {
		t.Fatal("expected error result")
	}
	msg := out.Content[0].(agent.TextBlock).Text
	if !strings.Contains(msg, "has not been read") {
		t.Fatalf("expected not-read error, got %q", msg)
	}
}

func TestEditTrackerStaleRejects(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "a.txt")
	_ = os.WriteFile(p, []byte("hello"), 0o600)
	st, _ := os.Stat(p)
	tr := state.NewReadTracker()
	tr.Record(p, st)

	// 外部把文件改了
	future := time.Now().Add(time.Hour)
	_ = os.Chtimes(p, future, future)

	tl := edit.New(edit.Cwd(dir), edit.Tracker(tr))
	out, err := tl.Call(context.Background(), map[string]any{
		"path":  "a.txt",
		"edits": []map[string]any{{"oldText": "hello", "newText": "HI"}},
	})
	if err != nil {
		t.Fatalf("unexpected go error: %v", err)
	}
	if !out.IsError {
		t.Fatal("expected error result")
	}
	msg := out.Content[0].(agent.TextBlock).Text
	if !strings.Contains(msg, "modified since the last read") {
		t.Fatalf("expected stale error, got %q", msg)
	}
}

func TestEditTrackerHappyPath(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "a.txt")
	_ = os.WriteFile(p, []byte("hello"), 0o600)
	st, _ := os.Stat(p)
	tr := state.NewReadTracker()
	tr.Record(p, st)
	tl := edit.New(edit.Cwd(dir), edit.Tracker(tr))

	out, err := tl.Call(context.Background(), map[string]any{
		"path":  "a.txt",
		"edits": []map[string]any{{"oldText": "hello", "newText": "HI"}},
	})
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if out.IsError {
		t.Fatalf("unexpected error result: %s", out.Content[0].(agent.TextBlock).Text)
	}
	// edit 后应当 forget，再 edit 必须重新 read
	out2, err := tl.Call(context.Background(), map[string]any{
		"path":  "a.txt",
		"edits": []map[string]any{{"oldText": "HI", "newText": "X"}},
	})
	if err != nil {
		t.Fatalf("unexpected go error: %v", err)
	}
	if !out2.IsError {
		t.Fatal("expected re-read enforcement error result")
	}
	msg := out2.Content[0].(agent.TextBlock).Text
	if !strings.Contains(msg, "has not been read") {
		t.Fatalf("expected re-read enforcement, got %q", msg)
	}
}

func TestEditReplaceAll(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "a.txt")
	_ = os.WriteFile(p, []byte("foo bar foo baz foo"), 0o600)
	tl := edit.New(edit.Cwd(dir))

	out, err := tl.Call(context.Background(), map[string]any{
		"path": "a.txt",
		"edits": []map[string]any{
			{"oldText": "foo", "newText": "QUX", "replace_all": true},
		},
	})
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if out.IsError {
		t.Fatalf("unexpected error result: %s", out.Content[0].(agent.TextBlock).Text)
	}
	got, _ := os.ReadFile(p) //nolint:gosec
	if string(got) != "QUX bar QUX baz QUX" {
		t.Fatalf("got %q", got)
	}
}

func TestEditReplaceAllStillCatchesOverlap(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "a.txt")
	_ = os.WriteFile(p, []byte("abcabc"), 0o600)
	tl := edit.New(edit.Cwd(dir))

	out, err := tl.Call(context.Background(), map[string]any{
		"path": "a.txt",
		"edits": []map[string]any{
			{"oldText": "abc", "newText": "X", "replace_all": true},
			{"oldText": "bca", "newText": "Y"},
		},
	})
	if err != nil {
		t.Fatalf("unexpected go error: %v", err)
	}
	if !out.IsError {
		t.Fatal("expected error result")
	}
	msg := out.Content[0].(agent.TextBlock).Text
	if !strings.Contains(msg, "overlap") {
		t.Fatalf("expected overlap error, got %q", msg)
	}
}
