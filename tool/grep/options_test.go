package grep

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	agent "github.com/cago-frame/agents/agent"
)

func TestOptions_AllSetters(t *testing.T) {
	cfg := &config{}
	Cwd("/tmp")(cfg)
	DefaultLimit(50)(cfg)
	MaxBytes(1024)(cfg)
	MaxLineChars(80)(cfg)
	IgnoreDirs("custom-skip")(cfg)
	IncludeAll()(cfg)
	Serial()(cfg)
	if cfg.cwd != "/tmp" {
		t.Errorf("cwd = %q", cfg.cwd)
	}
	if cfg.defaultLimit != 50 {
		t.Errorf("defaultLimit = %d", cfg.defaultLimit)
	}
	if cfg.maxBytes != 1024 {
		t.Errorf("maxBytes = %d", cfg.maxBytes)
	}
	if cfg.maxLineChars != 80 {
		t.Errorf("maxLineChars = %d", cfg.maxLineChars)
	}
	if len(cfg.ignoredDirs) != 1 || cfg.ignoredDirs[0] != "custom-skip" {
		t.Errorf("ignoredDirs = %v", cfg.ignoredDirs)
	}
	if !cfg.includeAll {
		t.Error("IncludeAll not applied")
	}
	if !cfg.serial {
		t.Error("Serial not applied")
	}
}

func TestNew_SerialOption(t *testing.T) {
	tl := New(Serial())
	st, ok := tl.(agent.SerialTool)
	if !ok {
		t.Fatal("expected SerialTool")
	}
	if !st.Serial() {
		t.Error("Serial: want true")
	}
}

func TestNew_DefaultsApplied(t *testing.T) {
	tl := New() // no opts → defaults
	if tl.Name() == "" {
		t.Error("Name empty")
	}
}

// TestRun_GlobNormalizeAndMatch exercises normalizeGlob / matchGlob /
// matchSegments via the public glob argument.
func TestRun_GlobNormalize(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "a", "b"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "a", "b", "x.go"), []byte("hit\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "x.txt"), []byte("hit\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	tl := New(Cwd(dir))
	res, err := tl.Call(context.Background(), map[string]any{"pattern": "hit", "glob": "*.go"})
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	out := res.Content[0].(agent.TextBlock).Text
	if !strings.Contains(out, "x.go") {
		t.Errorf("expected x.go in output, got %q", out)
	}
	if strings.Contains(out, "x.txt") {
		t.Errorf("did not expect x.txt in output, got %q", out)
	}
}

func TestRun_GlobAbsoluteRoot(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "x.go"), []byte("hit\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	tl := New(Cwd(dir))
	// leading slash → normalizeGlob trims it; "**/*.go" is the result
	res, err := tl.Call(context.Background(), map[string]any{"pattern": "hit", "glob": "/*.go"})
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	out := res.Content[0].(agent.TextBlock).Text
	if !strings.Contains(out, "x.go") {
		t.Errorf("got %q", out)
	}
}

func TestRun_IncludeAllSearchesIgnoredDirs(t *testing.T) {
	dir := t.TempDir()
	gitDir := filepath.Join(dir, ".git")
	if err := os.MkdirAll(gitDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(gitDir, "config"), []byte("hit\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	// Default skips .git
	tl := New(Cwd(dir))
	res, err := tl.Call(context.Background(), map[string]any{"pattern": "hit"})
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	out := res.Content[0].(agent.TextBlock).Text
	if strings.Contains(out, ".git") {
		t.Error("default New should skip .git dir")
	}

	// IncludeAll should descend into .git
	tlAll := New(Cwd(dir), IncludeAll())
	res, err = tlAll.Call(context.Background(), map[string]any{"pattern": "hit"})
	if err != nil {
		t.Fatalf("Call IncludeAll: %v", err)
	}
	out = res.Content[0].(agent.TextBlock).Text
	if !strings.Contains(out, ".git") {
		t.Errorf("IncludeAll should descend into .git, got %q", out)
	}
}

func TestRun_EmptyPatternErr(t *testing.T) {
	tl := New()
	res, err := tl.Call(context.Background(), map[string]any{"pattern": ""})
	if err != nil {
		t.Fatalf("unexpected Go error: %v", err)
	}
	if !res.IsError {
		t.Error("expected IsError for empty pattern")
	}
}

func TestRun_InvalidPattern(t *testing.T) {
	tl := New()
	// Invalid regex (unclosed paren)
	res, err := tl.Call(context.Background(), map[string]any{"pattern": "("})
	if err != nil {
		t.Fatalf("unexpected Go error: %v", err)
	}
	if !res.IsError {
		t.Error("expected IsError for invalid regex")
	}
}

func TestRun_CtxCanceledImmediately(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	tl := New()
	_, err := tl.Call(ctx, map[string]any{"pattern": "x"})
	if err == nil {
		t.Error("expected error on canceled ctx")
	}
}

func TestMatchGlob_DirectAndDoubleStar(t *testing.T) {
	cases := []struct {
		pattern, path string
		want          bool
	}{
		{"*.go", "x.go", true},
		{"*.go", "x.txt", false},
		{"**/*.go", "a/b/x.go", true},
		{"**/*.go", "x.go", true},
		{"a/**/x.go", "a/b/c/x.go", true},
		{"a/**/x.go", "x.go", false},
		{"**", "anything/here", true},
		{"a/b", "a/c", false},
		{"a/b/c", "a/b", false},
	}
	for _, tc := range cases {
		if got := matchGlob(tc.pattern, tc.path); got != tc.want {
			t.Errorf("matchGlob(%q,%q) = %v, want %v", tc.pattern, tc.path, got, tc.want)
		}
	}
}

func TestNormalizeGlob(t *testing.T) {
	cases := map[string]string{
		"*.go":    "**/*.go",
		"a/*.go":  "**/a/*.go",
		"**/x.go": "**/x.go",
		"/abs.go": "abs.go",
		"**":      "**",
		"":        "**/",
	}
	for in, want := range cases {
		if got := normalizeGlob(in); got != want {
			t.Errorf("normalizeGlob(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestRelPath_FileRoot(t *testing.T) {
	// rootIsDir=false should always return base name.
	if got := relPath("/x/y/z.go", "/x/y/z.go", false); got != "z.go" {
		t.Errorf("got %q", got)
	}
	// dir root, normal case
	if got := relPath("/x/y/sub/z.go", "/x/y", true); got != "sub/z.go" {
		t.Errorf("got %q", got)
	}
}
