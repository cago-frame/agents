package coding

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/cago-frame/cago/pkg/logger"
	"go.uber.org/zap"
)

// Skill describes one discovered skill.
type Skill struct {
	Name                   string // frontmatter name
	Description            string // frontmatter description
	Path                   string // absolute SKILL.md path
	DisableModelInvocation bool
	Source                 string // "global" | "project"
}

const (
	skillNameMaxLen        = 64
	skillDescriptionMaxLen = 1024
)

var skillNamePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)

// LoadSkills scans ~/.claude/skills/ + each .claude/skills/ along the cwd→repo-root chain.
// Project-level skills override global on Name conflict; cwd-direction overrides root-direction
// within the project chain.
func LoadSkills(ctx context.Context, cwd string) ([]Skill, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	return loadSkillsWithHome(ctx, cwd, home)
}

func loadSkillsWithHome(ctx context.Context, cwd, home string) ([]Skill, error) {
	cwdAbs, err := filepath.Abs(cwd)
	if err != nil {
		return nil, err
	}
	// On macOS, TempDir paths may go through symlinks; resolve them for consistent path matching.
	if resolved, err := filepath.EvalSymlinks(cwdAbs); err == nil {
		cwdAbs = resolved
	}
	// Also resolve home for consistent path comparison.
	if home != "" {
		if resolved, err := filepath.EvalSymlinks(home); err == nil {
			home = resolved
		}
	}

	root, isRepo := findGitRoot(cwdAbs)
	if !isRepo {
		root = home
	}

	byName := make(map[string]Skill)
	insert := func(s Skill) {
		byName[s.Name] = s // last write wins; we order writes from low priority to high priority
	}

	// 1. Global (lowest priority).
	if home != "" {
		for _, s := range scanSkillDir(ctx, filepath.Join(home, ".claude", "skills"), "global") {
			insert(s)
		}
	}
	// 2. Project chain root → cwd (high priority closer to cwd).
	for _, dir := range chainDirs(root, cwdAbs) {
		for _, s := range scanSkillDir(ctx, filepath.Join(dir, ".claude", "skills"), "project") {
			insert(s)
		}
	}

	out := make([]Skill, 0, len(byName))
	for _, s := range byName {
		out = append(out, s)
	}
	// Insertion sort for stable output by Name.
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j-1].Name > out[j].Name; j-- {
			out[j-1], out[j] = out[j], out[j-1]
		}
	}
	return out, nil
}

// scanSkillDir walks a skills dir. Subdirs containing SKILL.md become one skill
// (no further recursion into that subdir); subdirs WITHOUT SKILL.md are recursed one level.
// Markdown files at the dir root also count.
func scanSkillDir(ctx context.Context, dir, source string) []Skill {
	var skills []Skill
	entries, err := os.ReadDir(dir)
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			logger.Ctx(ctx).Warn("scanSkillDir: readdir failed",
				zap.String("dir", dir),
				zap.Error(err),
			)
		}
		return nil
	}
	for _, e := range entries {
		full := filepath.Join(dir, e.Name())
		if e.IsDir() {
			candidate := filepath.Join(full, "SKILL.md")
			if data, err := os.ReadFile(candidate); err == nil { //nolint:gosec // path from trusted dir scan, not user input
				if sk, ok := parseSkillFrontmatter(data); ok {
					sk.Path = candidate
					sk.Source = source
					if sk.Name == "" {
						sk.Name = e.Name()
					}
					skills = append(skills, sk)
				}
			} else {
				skills = append(skills, scanSkillDir(ctx, full, source)...)
			}
			continue
		}
		if !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		data, err := os.ReadFile(full) //nolint:gosec // path from trusted dir scan, not user input
		if err != nil {
			continue
		}
		sk, ok := parseSkillFrontmatter(data)
		if !ok {
			continue
		}
		sk.Path = full
		sk.Source = source
		skills = append(skills, sk)
	}
	return skills
}

// parseSkillFrontmatter parses the "---\nkey: value\n---" YAML-ish header.
// Returns (skill, true) on success; (zero, false) on missing/invalid.
//
// Recognized keys: name, description, disable-model-invocation. Unknown keys are ignored.
func parseSkillFrontmatter(data []byte) (Skill, bool) {
	if !bytes.HasPrefix(bytes.TrimLeft(data, " \t\r\n"), []byte("---")) {
		return Skill{}, false
	}
	lines := strings.Split(string(data), "\n")
	if len(lines) < 3 {
		return Skill{}, false
	}
	open, closeIdx := -1, -1
	for i, l := range lines {
		t := strings.TrimSpace(l)
		if t == "---" {
			if open == -1 {
				open = i
			} else {
				closeIdx = i
				break
			}
		}
	}
	if open == -1 || closeIdx == -1 || closeIdx <= open {
		return Skill{}, false
	}
	var sk Skill
	scanner := bufio.NewScanner(strings.NewReader(strings.Join(lines[open+1:closeIdx], "\n")))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		colon := strings.IndexByte(line, ':')
		if colon < 0 {
			continue
		}
		key := strings.ToLower(strings.TrimSpace(line[:colon]))
		val := strings.TrimSpace(line[colon+1:])
		val = strings.Trim(val, `"'`)
		switch key {
		case "name":
			sk.Name = val
		case "description":
			sk.Description = val
		case "disable-model-invocation":
			sk.DisableModelInvocation = strings.EqualFold(val, "true")
		}
	}
	if sk.Name == "" || !skillNamePattern.MatchString(sk.Name) || len(sk.Name) > skillNameMaxLen {
		return Skill{}, false
	}
	if sk.Description == "" || len(sk.Description) > skillDescriptionMaxLen {
		return Skill{}, false
	}
	return sk, true
}

// formatSkillsBlock renders the XML block consumed by BuildSystemPrompt.
// Skills with DisableModelInvocation=true are omitted; returns "" when no skill is eligible.
func formatSkillsBlock(skills []Skill) string {
	visible := make([]Skill, 0, len(skills))
	for _, s := range skills {
		if !s.DisableModelInvocation {
			visible = append(visible, s)
		}
	}
	if len(visible) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("<available-skills>\nUse the read tool to load a skill's file when the task matches its description.\n\n")
	for _, sk := range visible {
		b.WriteString("<skill>\n  <name>")
		b.WriteString(sk.Name)
		b.WriteString("</name>\n  <description>")
		b.WriteString(sk.Description)
		b.WriteString("</description>\n  <path>")
		b.WriteString(sk.Path)
		b.WriteString("</path>\n</skill>\n")
	}
	b.WriteString("</available-skills>\n")
	return b.String()
}
