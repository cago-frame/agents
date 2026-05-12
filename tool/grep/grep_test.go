package grep_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	agent "github.com/cago-frame/agents/agent"
	"github.com/cago-frame/agents/tool"
	"github.com/cago-frame/agents/tool/grep"
)

func mustWrite(t *testing.T, dir, rel, content string) {
	t.Helper()
	p := filepath.Join(dir, rel)
	_ = os.MkdirAll(filepath.Dir(p), 0o755)
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func callGrep(t *testing.T, tl tool.Tool, args map[string]any) string {
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

func TestGrepBasic(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, dir, "a.go", "alpha\nbeta gamma\nfoo\n")
	tl := grep.New(grep.Cwd(dir))
	out := callGrep(t, tl, map[string]any{"pattern": "beta"})
	if !strings.Contains(out, "a.go:2: beta gamma") {
		t.Fatalf("unexpected: %q", out)
	}
}

func TestGrepRegex(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, dir, "a.txt", "alpha-1\nalpha-2\nbeta\n")
	tl := grep.New(grep.Cwd(dir))
	out := callGrep(t, tl, map[string]any{"pattern": `alpha-\d`})
	if !strings.Contains(out, "alpha-1") || !strings.Contains(out, "alpha-2") {
		t.Fatalf("got %q", out)
	}
}

func TestGrepLiteralMode(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, dir, "a.txt", "(.*)\nplain\n")
	tl := grep.New(grep.Cwd(dir))
	out := callGrep(t, tl, map[string]any{"pattern": "(.*)", "literal": true})
	if !strings.Contains(out, "(.*)") {
		t.Fatalf("literal should match parens: %q", out)
	}
	if strings.Contains(out, "plain") {
		t.Fatalf("literal must not match plain: %q", out)
	}
}

func TestGrepIgnoreCase(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, dir, "a.txt", "Hello\nWORLD\n")
	tl := grep.New(grep.Cwd(dir))
	out := callGrep(t, tl, map[string]any{"pattern": "hello", "ignoreCase": true})
	if !strings.Contains(out, "Hello") {
		t.Fatalf("got %q", out)
	}
}

func TestGrepGlobFilter(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, dir, "a.go", "TARGET\n")
	mustWrite(t, dir, "b.txt", "TARGET\n")
	tl := grep.New(grep.Cwd(dir))
	out := callGrep(t, tl, map[string]any{"pattern": "TARGET", "glob": "*.go"})
	if !strings.Contains(out, "a.go") || strings.Contains(out, "b.txt") {
		t.Fatalf("glob filter wrong: %q", out)
	}
}

func TestGrepContext(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, dir, "a.txt", "before2\nbefore1\nMATCH\nafter1\nafter2\n")
	tl := grep.New(grep.Cwd(dir))
	out := callGrep(t, tl, map[string]any{"pattern": "MATCH", "context": 1})
	if !strings.Contains(out, "a.txt-2- before1") {
		t.Fatalf("missing before-context: %q", out)
	}
	if !strings.Contains(out, "a.txt:3: MATCH") {
		t.Fatalf("missing match line: %q", out)
	}
	if !strings.Contains(out, "a.txt-4- after1") {
		t.Fatalf("missing after-context: %q", out)
	}
}

func TestGrepNoMatches(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, dir, "a.txt", "alpha\n")
	tl := grep.New(grep.Cwd(dir))
	out := callGrep(t, tl, map[string]any{"pattern": "missing"})
	if out != "No matches found" {
		t.Fatalf("got %q", out)
	}
}

func TestGrepLimit(t *testing.T) {
	dir := t.TempDir()
	var b strings.Builder
	for i := range 50 {
		_ = i
		b.WriteString("hit\n")
	}
	mustWrite(t, dir, "a.txt", b.String())
	tl := grep.New(grep.Cwd(dir))
	out := callGrep(t, tl, map[string]any{"pattern": "hit", "limit": 5})
	if !strings.Contains(out, "5 matches limit reached. Use limit=10 for more") {
		t.Fatalf("missing limit notice: %q", out)
	}
}

func TestGrepSkipsGitDir(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, dir, ".git/HEAD", "TARGET\n")
	mustWrite(t, dir, "a.txt", "alpha\n")
	tl := grep.New(grep.Cwd(dir))
	out := callGrep(t, tl, map[string]any{"pattern": "TARGET"})
	if out != "No matches found" {
		t.Fatalf("git dir leak: %q", out)
	}
}
