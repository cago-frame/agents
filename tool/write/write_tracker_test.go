package write_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	agent "github.com/cago-frame/agents/agent"
	"github.com/cago-frame/agents/tool/state"
	"github.com/cago-frame/agents/tool/write"
)

func TestWriteOverwriteRequiresPriorRead(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "a.txt")
	_ = os.WriteFile(p, []byte("old"), 0o600)
	tr := state.NewReadTracker()
	tl := write.New(write.Cwd(dir), write.Tracker(tr))

	out, err := tl.Call(context.Background(), map[string]any{"path": "a.txt", "content": "new"})
	if err != nil {
		t.Fatalf("unexpected hard error: %v", err)
	}
	if !out.IsError {
		t.Fatalf("expected tool error result")
	}
	text := out.Content[0].(agent.TextBlock).Text
	if !strings.Contains(text, "has not been read") {
		t.Fatalf("expected not-read error, got %q", text)
	}
}

func TestWriteNewFileNoReadNeeded(t *testing.T) {
	dir := t.TempDir()
	tr := state.NewReadTracker()
	tl := write.New(write.Cwd(dir), write.Tracker(tr))

	out, err := tl.Call(context.Background(), map[string]any{"path": "newfile.txt", "content": "hi"})
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if out.IsError {
		t.Fatalf("unexpected tool error: %v", out.Content[0].(agent.TextBlock).Text)
	}
}

func TestWriteOverwriteAfterRead(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "a.txt")
	_ = os.WriteFile(p, []byte("old"), 0o600)
	st, _ := os.Stat(p)
	tr := state.NewReadTracker()
	tr.Record(p, st)
	tl := write.New(write.Cwd(dir), write.Tracker(tr))

	out, err := tl.Call(context.Background(), map[string]any{"path": "a.txt", "content": "new"})
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if out.IsError {
		t.Fatalf("unexpected tool error: %v", out.Content[0].(agent.TextBlock).Text)
	}
}

func TestWriteStaleRejected(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "a.txt")
	_ = os.WriteFile(p, []byte("old"), 0o600)
	st, _ := os.Stat(p)
	tr := state.NewReadTracker()
	tr.Record(p, st)
	future := time.Now().Add(time.Hour)
	_ = os.Chtimes(p, future, future)

	tl := write.New(write.Cwd(dir), write.Tracker(tr))
	out, err := tl.Call(context.Background(), map[string]any{"path": "a.txt", "content": "new"})
	if err != nil {
		t.Fatalf("unexpected hard error: %v", err)
	}
	if !out.IsError {
		t.Fatalf("expected tool error result")
	}
	text := out.Content[0].(agent.TextBlock).Text
	if !strings.Contains(text, "modified since the last read") {
		t.Fatalf("expected stale, got %q", text)
	}
}
