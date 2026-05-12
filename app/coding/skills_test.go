package coding

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadSkills_GlobalAndProjectDirs(t *testing.T) {
	home := t.TempDir()
	repo := filepath.Join(home, "repo")
	mustMkdir(t, filepath.Join(repo, ".git"))
	mustWrite(t,
		filepath.Join(home, ".claude", "skills", "writing-plans", "SKILL.md"),
		"---\nname: writing-plans\ndescription: Use when planning a multi-step task\n---\nbody")
	mustWrite(t,
		filepath.Join(repo, ".claude", "skills", "deploy", "SKILL.md"),
		"---\nname: deploy\ndescription: Use when deploying\n---\nbody")

	skills, err := loadSkillsWithHome(t.Context(), repo, home)
	if err != nil {
		t.Fatalf("LoadSkills: %v", err)
	}
	if len(skills) != 2 {
		t.Fatalf("want 2 skills, got %d", len(skills))
	}
	names := map[string]string{}
	for _, s := range skills {
		names[s.Name] = s.Source
	}
	if names["writing-plans"] != "global" {
		t.Errorf("writing-plans Source=%q", names["writing-plans"])
	}
	if names["deploy"] != "project" {
		t.Errorf("deploy Source=%q", names["deploy"])
	}
}

func TestLoadSkills_ProjectOverridesGlobal(t *testing.T) {
	home := t.TempDir()
	repo := filepath.Join(home, "repo")
	mustMkdir(t, filepath.Join(repo, ".git"))
	mustWrite(t,
		filepath.Join(home, ".claude", "skills", "skill-x", "SKILL.md"),
		"---\nname: skill-x\ndescription: global version\n---\n")
	mustWrite(t,
		filepath.Join(repo, ".claude", "skills", "skill-x", "SKILL.md"),
		"---\nname: skill-x\ndescription: project version\n---\n")

	skills, _ := loadSkillsWithHome(t.Context(), repo, home)
	if len(skills) != 1 || skills[0].Description != "project version" {
		t.Fatalf("want project version, got %+v", skills)
	}
}

func TestLoadSkills_DisableModelInvocationOmittedFromBlock(t *testing.T) {
	skills := []Skill{
		{Name: "a", Description: "always", Path: "/a", DisableModelInvocation: false},
		{Name: "b", Description: "explicit only", Path: "/b", DisableModelInvocation: true},
	}
	block := formatSkillsBlock(skills)
	if !strings.Contains(block, "<name>a</name>") {
		t.Fatalf("block missing skill a: %s", block)
	}
	if strings.Contains(block, "<name>b</name>") {
		t.Fatalf("block must NOT include disable-model-invocation skill: %s", block)
	}
}

func TestParseSkillFrontmatter_SkipsBadFiles(t *testing.T) {
	cases := []struct {
		name string
		body string
	}{
		{"missing frontmatter", "no frontmatter here"},
		{"missing name", "---\ndescription: ok\n---\n"},
		{"name too long", "---\nname: " + strings.Repeat("a", 100) + "\ndescription: ok\n---\n"},
		{"description too long", "---\nname: ok\ndescription: " + strings.Repeat("a", 2000) + "\n---\n"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, ok := parseSkillFrontmatter([]byte(c.body))
			if ok {
				t.Fatalf("expected parse to reject %s", c.name)
			}
		})
	}
}
