package coding

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// setupContextFixture lays out a fake home + repo:
//
//	home/.claude/CLAUDE.md         "global root"
//	home/repo/.git/                (marker)
//	home/repo/CLAUDE.md            "repo root"
//	home/repo/AGENTS.md            "repo agents"
//	home/repo/sub/AGENTS.md        "sub agents"
//	home/repo/sub/cwd/             (cwd, no md files)
//
// LoadProjectContext(cwd, homeOverride=home) should return all four files in order:
// global → repo root (CLAUDE then AGENTS) → sub.
func TestLoadProjectContext_ClaudeCodeStyle(t *testing.T) {
	home := t.TempDir()
	mustWrite(t, filepath.Join(home, ".claude", "CLAUDE.md"), "global root")
	repo := filepath.Join(home, "repo")
	mustMkdir(t, filepath.Join(repo, ".git"))
	mustWrite(t, filepath.Join(repo, "CLAUDE.md"), "repo claude")
	mustWrite(t, filepath.Join(repo, "AGENTS.md"), "repo agents")
	sub := filepath.Join(repo, "sub")
	mustWrite(t, filepath.Join(sub, "AGENTS.md"), "sub agents")
	cwd := filepath.Join(sub, "cwd")
	mustMkdir(t, cwd)

	files, err := loadProjectContextWithHome(t.Context(), cwd, home)
	if err != nil {
		t.Fatalf("LoadProjectContext: %v", err)
	}
	if len(files) != 4 {
		t.Fatalf("want 4 files, got %d: %+v", len(files), files)
	}
	want := []string{"global root", "repo claude", "repo agents", "sub agents"}
	for i, w := range want {
		if files[i].Content != w {
			t.Fatalf("file[%d] content: want %q got %q", i, w, files[i].Content)
		}
	}
	// First file is global; its RelPath should keep "~/.claude/CLAUDE.md" form.
	if !strings.HasPrefix(files[0].RelPath, "~/") {
		t.Fatalf("global file RelPath want ~/-prefixed, got %q", files[0].RelPath)
	}
}

func TestLoadProjectContext_NoGitRoot_FallsBackToHome(t *testing.T) {
	home := t.TempDir()
	cwd := filepath.Join(home, "noproj", "deep")
	mustMkdir(t, cwd)
	mustWrite(t, filepath.Join(home, "noproj", "CLAUDE.md"), "no-git claude")

	files, err := loadProjectContextWithHome(t.Context(), cwd, home)
	if err != nil {
		t.Fatalf("LoadProjectContext: %v", err)
	}
	for _, f := range files {
		if f.Content == "no-git claude" {
			return
		}
	}
	t.Fatalf("expected to find no-git claude; got: %+v", files)
}

func TestLoadProjectContext_DeduplicatesSamePath(t *testing.T) {
	home := t.TempDir()
	repo := filepath.Join(home, "repo")
	mustMkdir(t, filepath.Join(repo, ".git"))
	mustWrite(t, filepath.Join(repo, "CLAUDE.md"), "only one")

	files, err := loadProjectContextWithHome(t.Context(), repo, home)
	if err != nil {
		t.Fatalf("LoadProjectContext: %v", err)
	}
	count := 0
	for _, f := range files {
		if f.Content == "only one" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("expected 1 occurrence, got %d", count)
	}
}

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
func mustMkdir(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
}
