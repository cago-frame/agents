package coding

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/cago-frame/cago/pkg/logger"
	"go.uber.org/zap"
)

// ContextFile represents one loaded project-context file.
type ContextFile struct {
	AbsPath string // absolute path on disk; used for de-duplication
	RelPath string // display path: "~/.claude/CLAUDE.md" for global, relative-to-cwd otherwise
	Content string
}

// contextFilenames is the set of project-context filenames we look for.
// Order matters: when both exist in the same directory, AGENTS.md is appended after CLAUDE.md.
var contextFilenames = []string{"CLAUDE.md", "AGENTS.md"}

// LoadProjectContext scans Claude Code-style:
//  1. ~/.claude/CLAUDE.md (global)
//  2. Walk from cwd upward to the first .git/ directory (= repo root); every directory along the way
//     (root → cwd) contributes its CLAUDE.md and/or AGENTS.md if present.
//  3. If no .git/ is found, fall back to user home as the root and stop there.
//
// Files are returned outermost-first: global → repo-root → ... → cwd directory.
// The same absolute path is included only once. IO errors on a single file are logged and skipped.
func LoadProjectContext(ctx context.Context, cwd string) ([]ContextFile, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	return loadProjectContextWithHome(ctx, cwd, home)
}

func loadProjectContextWithHome(ctx context.Context, cwd, home string) ([]ContextFile, error) {
	cwdAbs, err := filepath.Abs(cwd)
	if err != nil {
		return nil, err
	}
	// On macOS, TempDir paths may go through symlinks; resolve them for consistent dedup.
	if resolved, err := filepath.EvalSymlinks(cwdAbs); err == nil {
		cwdAbs = resolved
	}
	// Also resolve home for consistent path comparison.
	if home != "" {
		if resolved, err := filepath.EvalSymlinks(home); err == nil {
			home = resolved
		}
	}
	var (
		out  []ContextFile
		seen = make(map[string]bool)
	)
	add := func(absPath, relPath string) {
		clean := filepath.Clean(absPath)
		// Resolve symlinks for dedup consistency.
		if resolved, err := filepath.EvalSymlinks(clean); err == nil {
			clean = resolved
		}
		if seen[clean] {
			return
		}
		data, err := os.ReadFile(clean)
		if err != nil {
			if !errors.Is(err, fs.ErrNotExist) {
				logger.Ctx(ctx).Warn("LoadProjectContext: read failed",
					zap.String("path", clean),
					zap.Error(err),
				)
			}
			return
		}
		seen[clean] = true
		out = append(out, ContextFile{
			AbsPath: clean,
			RelPath: relPath,
			Content: string(data),
		})
	}

	// 1. Global CLAUDE.md.
	if home != "" {
		gp := filepath.Join(home, ".claude", "CLAUDE.md")
		add(gp, "~/.claude/CLAUDE.md")
	}

	// 2. Walk from repo root downward to cwd. Find repo root first.
	root, isRepo := findGitRoot(cwdAbs)
	if !isRepo {
		root = home
	}
	if root == "" {
		root = cwdAbs
	}

	dirs := chainDirs(root, cwdAbs)
	for _, dir := range dirs {
		for _, name := range contextFilenames {
			abs := filepath.Join(dir, name)
			rel, relErr := filepath.Rel(cwdAbs, abs)
			if relErr != nil {
				rel = abs
			}
			add(abs, rel)
		}
	}
	return out, nil
}

// findGitRoot walks upward from start until a .git/ directory is found.
func findGitRoot(start string) (string, bool) {
	cur := start
	for {
		if info, err := os.Stat(filepath.Join(cur, ".git")); err == nil && info.IsDir() {
			return cur, true
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return "", false
		}
		cur = parent
	}
}

// chainDirs returns the list of directories from root to leaf inclusive, in that order.
// If leaf is not under root, returns just [leaf].
func chainDirs(root, leaf string) []string {
	rel, err := filepath.Rel(root, leaf)
	if err != nil || strings.HasPrefix(rel, "..") {
		return []string{leaf}
	}
	parts := []string{root}
	if rel == "." {
		return parts
	}
	cur := root
	for _, seg := range strings.Split(rel, string(filepath.Separator)) {
		cur = filepath.Join(cur, seg)
		parts = append(parts, cur)
	}
	return parts
}
