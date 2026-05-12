package pathutil_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/cago-frame/agents/tool/internal/pathutil"
)

func TestExpandStripsAtPrefix(t *testing.T) {
	if got := pathutil.Expand("@foo/bar"); got != "foo/bar" {
		t.Fatalf("got %q", got)
	}
}

func TestExpandHome(t *testing.T) {
	home, _ := os.UserHomeDir()
	if got := pathutil.Expand("~/code"); got != filepath.Join(home, "code") {
		t.Fatalf("got %q", got)
	}
	if got := pathutil.Expand("~"); got != home {
		t.Fatalf("got %q", got)
	}
}

func TestExpandNormalizesNBSP(t *testing.T) {
	got := pathutil.Expand("foo bar")
	if got != "foo bar" {
		t.Fatalf("got %q", got)
	}
}

func TestResolveToCwdAbsolute(t *testing.T) {
	got := pathutil.ResolveToCwd("/tmp/x", "/somewhere")
	if got != "/tmp/x" {
		t.Fatalf("got %q", got)
	}
}

func TestResolveToCwdRelative(t *testing.T) {
	got := pathutil.ResolveToCwd("a/b", "/wd")
	if got != "/wd/a/b" {
		t.Fatalf("got %q", got)
	}
}

func TestResolveToCwdEmptyCwd(t *testing.T) {
	got := pathutil.ResolveToCwd("a/b", "")
	if got != "a/b" {
		t.Fatalf("got %q", got)
	}
}

func TestResolveReadPathExisting(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "x.txt")
	if err := os.WriteFile(f, []byte("hi"), 0o600); err != nil {
		t.Fatal(err)
	}
	got := pathutil.ResolveReadPath("x.txt", dir)
	if got != f {
		t.Fatalf("got %q want %q", got, f)
	}
}

func TestResolveReadPathFallback(t *testing.T) {
	// 不存在的路径 -> 返回首选路径（错误由调用方读文件时报）
	got := pathutil.ResolveReadPath("nope.txt", "/tmp")
	if got != "/tmp/nope.txt" {
		t.Fatalf("got %q", got)
	}
}
