package read_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	agent "github.com/cago-frame/agents/agent"
	"github.com/cago-frame/agents/tool"
	"github.com/cago-frame/agents/tool/read"
)

func writeFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func callTool(t *testing.T, tl tool.Tool, args map[string]any) string {
	t.Helper()
	out, err := tl.Call(context.Background(), args)
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	if out.IsError {
		t.Fatalf("unexpected IsError, content=%v", out.Content)
	}
	return out.Content[0].(agent.TextBlock).Text
}

func TestReadSmallFileReturnsRawContent(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "a.txt", "line1\nline2\nline3")
	tl := read.New(read.Cwd(dir))

	out := callTool(t, tl, map[string]any{"path": "a.txt"})
	if out != "line1\nline2\nline3" {
		t.Fatalf("got %q", out)
	}
}

func TestReadOffsetAndLimit(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "a.txt", "1\n2\n3\n4\n5")
	tl := read.New(read.Cwd(dir))

	out := callTool(t, tl, map[string]any{"path": "a.txt", "offset": 2, "limit": 2})
	if !strings.HasPrefix(out, "2\n3") {
		t.Fatalf("expected lines 2-3 first, got %q", out)
	}
	if !strings.Contains(out, "[2 more lines in file. Use offset=4 to continue.]") {
		t.Fatalf("missing more-lines notice: %q", out)
	}
}

func TestReadOffsetBeyondEnd(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "a.txt", "x\ny")
	tl := read.New(read.Cwd(dir))
	out, err := tl.Call(context.Background(), map[string]any{"path": "a.txt", "offset": 99})
	if err != nil {
		t.Fatalf("unexpected Go error: %v", err)
	}
	if !out.IsError {
		t.Fatalf("expected IsError=true")
	}
	msg := out.Content[0].(agent.TextBlock).Text
	if !strings.Contains(msg, "beyond end of file") {
		t.Fatalf("wrong error message: %q", msg)
	}
}

func TestReadLineLimitTruncated(t *testing.T) {
	dir := t.TempDir()
	var b strings.Builder
	for range 100 {
		b.WriteString("x\n")
	}
	writeFile(t, dir, "big.txt", b.String())
	tl := read.New(read.Cwd(dir), read.MaxLines(10))

	out := callTool(t, tl, map[string]any{"path": "big.txt"})
	if !strings.Contains(out, "[Showing lines 1-10 of 101. Use offset=11 to continue.]") {
		t.Fatalf("missing line truncation notice: %q", out)
	}
}

func TestReadByteLimitTruncated(t *testing.T) {
	dir := t.TempDir()
	var b strings.Builder
	for range 1000 {
		b.WriteString("abcd\n")
	}
	writeFile(t, dir, "big.txt", b.String())
	tl := read.New(read.Cwd(dir), read.MaxBytes(40))

	out := callTool(t, tl, map[string]any{"path": "big.txt"})
	if !strings.Contains(out, "limit). Use offset=") {
		t.Fatalf("expected byte-limit notice: %q", out)
	}
}

func TestReadFirstLineExceeds(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "wide.txt", strings.Repeat("x", 200)+"\nrest")
	tl := read.New(read.Cwd(dir), read.MaxBytes(50))

	out := callTool(t, tl, map[string]any{"path": "wide.txt"})
	if !strings.HasPrefix(out, "[Line 1 is ") {
		t.Fatalf("expected first-line notice, got %q", out)
	}
}

func TestReadMissingFileError(t *testing.T) {
	dir := t.TempDir()
	tl := read.New(read.Cwd(dir))
	out, err := tl.Call(context.Background(), map[string]any{"path": "nope.txt"})
	if err != nil {
		t.Fatalf("unexpected Go error: %v", err)
	}
	if !out.IsError {
		t.Fatalf("expected IsError=true for missing file")
	}
}
