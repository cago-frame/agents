package ls_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	agent "github.com/cago-frame/agents/agent"
	"github.com/cago-frame/agents/tool"
	"github.com/cago-frame/agents/tool/ls"
)

func mustWrite(t *testing.T, dir, rel string) {
	t.Helper()
	p := filepath.Join(dir, rel)
	_ = os.MkdirAll(filepath.Dir(p), 0o755)
	if err := os.WriteFile(p, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
}

func callLs(t *testing.T, tl tool.Tool, args map[string]any) string {
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

func TestLsBasic(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, dir, "b.txt")
	mustWrite(t, dir, ".hidden")
	_ = os.Mkdir(filepath.Join(dir, "sub"), 0o755)
	tl := ls.New(ls.Cwd(dir))

	out := callLs(t, tl, map[string]any{})
	// case-insensitive sort: .hidden < b.txt < sub/
	want := ".hidden\nb.txt\nsub/"
	if out != want {
		t.Fatalf("got %q want %q", out, want)
	}
}

func TestLsEmptyDirMessage(t *testing.T) {
	dir := t.TempDir()
	tl := ls.New(ls.Cwd(dir))
	out := callLs(t, tl, map[string]any{})
	if out != "(empty directory)" {
		t.Fatalf("got %q", out)
	}
}

func TestLsPathNotFound(t *testing.T) {
	tl := ls.New(ls.Cwd("/tmp"))
	out, err := tl.Call(context.Background(), map[string]any{"path": "definitely-not-here-xyz"})
	if err != nil {
		t.Fatalf("unexpected Go error: %v", err)
	}
	if !out.IsError {
		t.Fatalf("expected IsError=true")
	}
	if !strings.Contains(out.Content[0].(agent.TextBlock).Text, "Path not found") {
		t.Fatalf("wrong error text: %s", out.Content[0].(agent.TextBlock).Text)
	}
}

func TestLsNotADirectory(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, dir, "a.txt")
	tl := ls.New(ls.Cwd(dir))
	out, err := tl.Call(context.Background(), map[string]any{"path": "a.txt"})
	if err != nil {
		t.Fatalf("unexpected Go error: %v", err)
	}
	if !out.IsError {
		t.Fatalf("expected IsError=true")
	}
	if !strings.Contains(out.Content[0].(agent.TextBlock).Text, "Not a directory") {
		t.Fatalf("wrong error text: %s", out.Content[0].(agent.TextBlock).Text)
	}
}

func TestLsLimit(t *testing.T) {
	dir := t.TempDir()
	for _, n := range []string{"a", "b", "c", "d", "e"} {
		mustWrite(t, dir, n)
	}
	tl := ls.New(ls.Cwd(dir))
	out := callLs(t, tl, map[string]any{"limit": 2})
	if !strings.Contains(out, "2 entries limit reached. Use limit=4 for more") {
		t.Fatalf("missing limit notice: %q", out)
	}
}

func TestLsSerial(t *testing.T) {
	tl := ls.New(ls.Serial())
	if !tl.(agent.SerialTool).Serial() {
		t.Fatal("expected Serial()=true")
	}
}
