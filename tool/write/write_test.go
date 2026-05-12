package write_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	agent "github.com/cago-frame/agents/agent"
	"github.com/cago-frame/agents/tool/write"
)

func TestWriteCreatesFile(t *testing.T) {
	dir := t.TempDir()
	tl := write.New(write.Cwd(dir))
	out, err := tl.Call(context.Background(), map[string]any{"path": "a.txt", "content": "hello"})
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	if out.IsError {
		t.Fatalf("unexpected tool error: %v", out.Content[0].(agent.TextBlock).Text)
	}
	text := out.Content[0].(agent.TextBlock).Text
	if !strings.Contains(text, "Successfully wrote 5 bytes to a.txt") {
		t.Fatalf("unexpected: %q", text)
	}
	got, _ := os.ReadFile(filepath.Join(dir, "a.txt")) //nolint:gosec // test reads back what we just wrote
	if string(got) != "hello" {
		t.Fatalf("file contents: %q", got)
	}
}

func TestWriteCreatesParentDirs(t *testing.T) {
	dir := t.TempDir()
	tl := write.New(write.Cwd(dir))
	out, err := tl.Call(context.Background(), map[string]any{"path": "deep/nested/x.txt", "content": "x"})
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	if out.IsError {
		t.Fatalf("unexpected tool error: %v", out.Content[0].(agent.TextBlock).Text)
	}
	if _, err := os.Stat(filepath.Join(dir, "deep/nested/x.txt")); err != nil {
		t.Fatalf("expected file created, got %v", err)
	}
}

func TestWriteOverwritesExisting(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "x.txt")
	_ = os.WriteFile(p, []byte("old"), 0o600)
	tl := write.New(write.Cwd(dir))
	out, err := tl.Call(context.Background(), map[string]any{"path": "x.txt", "content": "new"})
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	if out.IsError {
		t.Fatalf("unexpected tool error: %v", out.Content[0].(agent.TextBlock).Text)
	}
	got, _ := os.ReadFile(p) //nolint:gosec // test reads back what we just wrote
	if string(got) != "new" {
		t.Fatalf("got %q", got)
	}
}

func TestWriteEmptyPathRejected(t *testing.T) {
	tl := write.New()
	out, err := tl.Call(context.Background(), map[string]any{"path": "", "content": "x"})
	if err != nil {
		t.Fatalf("unexpected hard error: %v", err)
	}
	if !out.IsError {
		t.Fatalf("expected tool error result")
	}
	text := out.Content[0].(agent.TextBlock).Text
	if !strings.Contains(text, "path must not be empty") {
		t.Fatalf("unexpected error text: %q", text)
	}
}
