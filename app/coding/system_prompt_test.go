package coding

import (
	"strings"
	"testing"
	"time"

	"github.com/cago-frame/agents/tool"
	"github.com/cago-frame/agents/tool/bash"
	"github.com/cago-frame/agents/tool/grep"
	"github.com/cago-frame/agents/tool/read"
)

type agentTool = tool.Tool

func TestBuildSystemPrompt_BasicShape(t *testing.T) {
	tools := []any{
		read.New(read.Cwd(".")),
		bash.New(bash.Cwd(".")),
		grep.New(grep.Cwd(".")),
	}
	out, err := BuildSystemPrompt(SystemPromptOptions{
		Cwd:         "/tmp/proj",
		Tools:       toToolSlice(tools),
		HasReadTool: true,
		Now:         time.Date(2026, 5, 6, 0, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	mustContain(t, out, SystemIntro)
	mustContain(t, out, "## Available tools")
	mustContain(t, out, "- read: ")
	mustContain(t, out, "- bash: ")
	mustContain(t, out, "- grep: ")
	mustContain(t, out, "## Guidelines")
	mustContain(t, out, "Prefer grep/find/ls tools over bash") // combination guideline (G1)
	mustContain(t, out, BaseGuidelines[0])
	mustContain(t, out, "Current date: 2026-05-06")
	mustContain(t, out, "Current working directory: /tmp/proj")
}

func TestBuildSystemPrompt_AppendAndContextAndSkills(t *testing.T) {
	out, err := BuildSystemPrompt(SystemPromptOptions{
		Cwd:          "/tmp",
		Tools:        toToolSlice([]any{read.New(read.Cwd("."))}),
		AppendSystem: "Be extra polite.",
		ContextFiles: []ContextFile{
			{RelPath: "CLAUDE.md", Content: "# Project\nUse 4-space indents."},
		},
		Skills: []Skill{
			{Name: "writing-plans", Description: "Use when planning", Path: "/abs/path/SKILL.md"},
		},
		HasReadTool: true,
		Now:         time.Date(2026, 5, 6, 0, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	mustContain(t, out, "Be extra polite.")
	mustContain(t, out, "## Project Context")
	mustContain(t, out, "### CLAUDE.md")
	mustContain(t, out, "Use 4-space indents.")
	mustContain(t, out, "<available-skills>")
	mustContain(t, out, "<name>writing-plans</name>")
	mustContain(t, out, "<path>/abs/path/SKILL.md</path>")
}

func TestBuildSystemPrompt_NoReadToolSkipsSkills(t *testing.T) {
	out, err := BuildSystemPrompt(SystemPromptOptions{
		Cwd:         "/tmp",
		Tools:       nil,
		Skills:      []Skill{{Name: "x", Description: "y", Path: "/p"}},
		HasReadTool: false,
		Now:         time.Now(),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.Contains(out, "<available-skills>") {
		t.Fatalf("skills should be skipped without read tool")
	}
}

// TestBuildSystemPrompt_DefaultByteStable 断言默认模板对一组确定性输入的输出字节级稳定。
// 把空工具集 + 仅 BaseGuidelines 的最简形态写成 expected，验证 template wiring 没改变结构。
// 如果工具集 / 段落顺序 / 分隔符变了，这个测试会先挂。
func TestBuildSystemPrompt_DefaultByteStable(t *testing.T) {
	out, err := BuildSystemPrompt(SystemPromptOptions{
		Cwd: "/tmp/proj",
		Now: time.Date(2026, 5, 6, 0, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	expected := SystemIntro + "\n\n## Available tools\n(none)\n\n## Guidelines\n"
	for _, g := range BaseGuidelines {
		expected += "- " + g + "\n"
	}
	expected += "\nCurrent date: 2026-05-06\nCurrent working directory: /tmp/proj"
	if out != expected {
		t.Fatalf("default output drift:\n--- got ---\n%s\n--- want ---\n%s\n", out, expected)
	}
}

// TestBuildSystemPrompt_CustomTemplate 用极简模板验证只渲染选定变量。
func TestBuildSystemPrompt_CustomTemplate(t *testing.T) {
	out, err := BuildSystemPrompt(SystemPromptOptions{
		Cwd:      "/tmp",
		Now:      time.Date(2026, 5, 12, 0, 0, 0, 0, time.UTC),
		Template: "X|{{.Cwd}}|{{.Date}}",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if want := "X|/tmp|2026-05-12"; out != want {
		t.Fatalf("got %q want %q", out, want)
	}
}

// TestBuildSystemPrompt_ParseError 验证非法模板返回错误而不是 panic。
func TestBuildSystemPrompt_ParseError(t *testing.T) {
	_, err := BuildSystemPrompt(SystemPromptOptions{
		Cwd:      "/tmp",
		Template: "{{",
	})
	if err == nil {
		t.Fatalf("expected parse error, got nil")
	}
}

// TestBuildSystemPrompt_ToolsListVar 验证 {{.ToolsList}} 渲染出工具行。
func TestBuildSystemPrompt_ToolsListVar(t *testing.T) {
	tools := toToolSlice([]any{
		read.New(read.Cwd(".")),
		bash.New(bash.Cwd(".")),
	})
	out, err := BuildSystemPrompt(SystemPromptOptions{
		Cwd:      "/tmp",
		Tools:    tools,
		Template: "TOOLS:\n{{.ToolsList}}",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	mustContain(t, out, "- read: ")
	mustContain(t, out, "- bash: ")
	if !strings.HasPrefix(out, "TOOLS:\n") {
		t.Fatalf("expected prefix %q in %q", "TOOLS:\n", out)
	}
}

func mustContain(t *testing.T, hay, needle string) {
	t.Helper()
	if !strings.Contains(hay, needle) {
		t.Fatalf("expected %q to contain %q", hay, needle)
	}
}

func toToolSlice(items []any) []agentTool {
	out := make([]agentTool, 0, len(items))
	for _, it := range items {
		if t, ok := it.(agentTool); ok {
			out = append(out, t)
		}
	}
	return out
}
