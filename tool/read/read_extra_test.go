package read_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	agent "github.com/cago-frame/agents/agent"
	"github.com/cago-frame/agents/tool/read"
	"github.com/cago-frame/agents/tool/state"
)

func TestReadRecordsToTracker(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "a.txt")
	_ = os.WriteFile(p, []byte("hello"), 0o600)
	tr := state.NewReadTracker()
	tl := read.New(read.Cwd(dir), read.Tracker(tr))

	out, err := tl.Call(context.Background(), map[string]any{"path": "a.txt"})
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if out.IsError {
		t.Fatalf("unexpected IsError, content=%v", out.Content)
	}
	if _, err := tr.Check(p); err != nil {
		t.Fatalf("expected tracker hit, got %v", err)
	}
}

func TestReadLineNumbersFormat(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "a.txt")
	_ = os.WriteFile(p, []byte("alpha\nbeta\ngamma"), 0o600)
	tl := read.New(read.Cwd(dir), read.LineNumbers())

	out, err := tl.Call(context.Background(), map[string]any{"path": "a.txt"})
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if out.IsError {
		t.Fatalf("unexpected IsError, content=%v", out.Content)
	}
	text := out.Content[0].(agent.TextBlock).Text
	if !strings.Contains(text, "     1\talpha") {
		t.Fatalf("missing line 1 prefix: %q", text)
	}
	if !strings.Contains(text, "     2\tbeta") {
		t.Fatalf("missing line 2 prefix: %q", text)
	}
}

func TestReadLineNumbersWithOffset(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "a.txt")
	_ = os.WriteFile(p, []byte("a\nb\nc\nd\ne"), 0o600)
	tl := read.New(read.Cwd(dir), read.LineNumbers())

	out, err := tl.Call(context.Background(), map[string]any{"path": "a.txt", "offset": 3, "limit": 2})
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if out.IsError {
		t.Fatalf("unexpected IsError, content=%v", out.Content)
	}
	text := out.Content[0].(agent.TextBlock).Text
	if !strings.HasPrefix(text, "     3\tc") {
		t.Fatalf("expected to start at line 3, got %q", text)
	}
	if !strings.Contains(text, "     4\td") {
		t.Fatalf("expected line 4, got %q", text)
	}
}
