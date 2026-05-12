package find_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	agent "github.com/cago-frame/agents/agent"
	"github.com/cago-frame/agents/tool"
	"github.com/cago-frame/agents/tool/find"
)

func mustWrite(t *testing.T, dir, rel string) {
	t.Helper()
	p := filepath.Join(dir, rel)
	_ = os.MkdirAll(filepath.Dir(p), 0o755)
	if err := os.WriteFile(p, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
}

func callFind(t *testing.T, tl tool.Tool, args map[string]any) string {
	t.Helper()
	out, err := tl.Call(context.Background(), args)
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	if out.IsError {
		t.Fatalf("tool error: %s", out.Content[0].(agent.TextBlock).Text)
	}
	return out.Content[0].(agent.TextBlock).Text
}

func TestFindBasenamePattern(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, dir, "a.go")
	mustWrite(t, dir, "b.txt")
	mustWrite(t, dir, "src/c.go")
	tl := find.New(find.Cwd(dir))

	out := callFind(t, tl, map[string]any{"pattern": "*.go"})
	if !strings.Contains(out, "a.go") || !strings.Contains(out, "src/c.go") {
		t.Fatalf("expected both go files, got %q", out)
	}
	if strings.Contains(out, "b.txt") {
		t.Fatalf("unexpected match: %q", out)
	}
}

func TestFindDoubleStarPath(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, dir, "a.go")
	mustWrite(t, dir, "src/c.go")
	mustWrite(t, dir, "src/deep/d.go")
	tl := find.New(find.Cwd(dir))

	out := callFind(t, tl, map[string]any{"pattern": "src/**/*.go"})
	if !strings.Contains(out, "src/c.go") || !strings.Contains(out, "src/deep/d.go") {
		t.Fatalf("expected src files, got %q", out)
	}
	if strings.Contains(out, "a.go\n") || strings.HasPrefix(out, "a.go") {
		t.Fatalf("a.go should not match: %q", out)
	}
}

func TestFindIgnoresGitDir(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, dir, "good.go")
	mustWrite(t, dir, ".git/objects/x.go")
	tl := find.New(find.Cwd(dir))

	out := callFind(t, tl, map[string]any{"pattern": "*.go"})
	if strings.Contains(out, ".git/") {
		t.Fatalf(".git should be filtered: %q", out)
	}
}

func TestFindIncludeAll(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, dir, ".git/objects/x.go")
	tl := find.New(find.Cwd(dir), find.IncludeAll())

	out := callFind(t, tl, map[string]any{"pattern": "*.go"})
	if !strings.Contains(out, ".git/objects/x.go") {
		t.Fatalf("IncludeAll should expose .git: %q", out)
	}
}

func TestFindNoMatch(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, dir, "a.txt")
	tl := find.New(find.Cwd(dir))
	out := callFind(t, tl, map[string]any{"pattern": "*.go"})
	if out != "No files found matching pattern" {
		t.Fatalf("got %q", out)
	}
}

func TestFindLimitNotice(t *testing.T) {
	dir := t.TempDir()
	for _, n := range []string{"a.go", "b.go", "c.go"} {
		mustWrite(t, dir, n)
	}
	tl := find.New(find.Cwd(dir))
	out := callFind(t, tl, map[string]any{"pattern": "*.go", "limit": 2})
	if !strings.Contains(out, "2 results limit reached. Use limit=4 for more") {
		t.Fatalf("missing notice: %q", out)
	}
}

func TestFindPathNotFound(t *testing.T) {
	tl := find.New()
	out, err := tl.Call(context.Background(), map[string]any{"pattern": "*", "path": "/__nope__/__nope__"})
	if err != nil {
		t.Fatalf("unexpected Go error: %v", err)
	}
	if !out.IsError {
		t.Fatalf("expected IsError=true, got text: %s", out.Content[0].(agent.TextBlock).Text)
	}
	if !strings.Contains(out.Content[0].(agent.TextBlock).Text, "Path not found") {
		t.Fatalf("wrong error text: %s", out.Content[0].(agent.TextBlock).Text)
	}
}

func TestFindSerial(t *testing.T) {
	tl := find.New(find.Serial())
	if !tl.(agent.SerialTool).Serial() {
		t.Fatal("expected Serial()=true")
	}
}
