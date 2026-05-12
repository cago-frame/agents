package coding

import (
	"fmt"
	"strings"
	"text/template"
	"time"

	"github.com/cago-frame/agents/tool"
)

// PromptMeta is the structural interface implemented by tools that contribute
// system-prompt content. BuildSystemPrompt detects it via type assertion;
// tools that do not implement it fall back to Description() for the snippet.
type PromptMeta interface {
	PromptSnippet() string
	PromptGuidelines() []string
}

// SystemPromptOptions is the input to BuildSystemPrompt.
type SystemPromptOptions struct {
	Cwd          string        // required
	Tools        []tool.Tool   // current tool set; builder collects PromptMeta
	AppendSystem string        // user-injected suffix (e.g., AppendSystem option)
	ContextFiles []ContextFile // LoadProjectContext result
	Skills       []Skill       // LoadSkills result
	HasReadTool  bool          // false ⇒ skip skills section (skills require read)
	Now          time.Time     // injection time; tests fix it. zero ⇒ time.Now()
	Template     string        // 可选；空时使用 DefaultSystemTemplate
}

// BuildSystemPrompt assembles the dynamic system prompt for the parent coding agent.
// Default order (DefaultSystemTemplate): intro → ## Available tools → ## Guidelines
// (per-tool dedup, then combination, then base) → optional append → optional Project Context
// → optional skills XML → footer (current date + cwd).
//
// 传入 opts.Template 可完全自定义结构；模板变量见 SystemPromptData。
// 模板解析或执行失败时返回 error；正常路径下 error 为 nil。
func BuildSystemPrompt(opts SystemPromptOptions) (string, error) {
	data := gatherSystemPromptData(opts)
	tmplText := opts.Template
	if tmplText == "" {
		tmplText = DefaultSystemTemplate
	}
	tmpl, err := template.New("coding.system").Parse(tmplText)
	if err != nil {
		return "", fmt.Errorf("coding: parse system template: %w", err)
	}
	var b strings.Builder
	if err := tmpl.Execute(&b, data); err != nil {
		return "", fmt.Errorf("coding: execute system template: %w", err)
	}
	return b.String(), nil
}

// gatherSystemPromptData 把 opts 各段预渲染成字符串，组装成 SystemPromptData。
// 每个可选段（AppendSystem / ContextFiles / SkillsBlock）为空或自带 "\n" 前缀，
// 这样模板里多个 {{.Xxx}} 并排时不会出现孤立空行——是否输出空行完全由数据控制。
func gatherSystemPromptData(opts SystemPromptOptions) SystemPromptData {
	snippets, toolGuidelines, toolNames := gatherToolMeta(opts.Tools)

	var toolsBuf strings.Builder
	if len(opts.Tools) == 0 {
		toolsBuf.WriteString("(none)\n")
	} else {
		for _, t := range opts.Tools {
			name := t.Name()
			snip := snippets[name]
			if snip == "" {
				snip = strings.TrimSpace(t.Description())
			}
			toolsBuf.WriteString("- ")
			toolsBuf.WriteString(name)
			toolsBuf.WriteString(": ")
			toolsBuf.WriteString(snip)
			toolsBuf.WriteString("\n")
		}
	}

	var guidelinesBuf strings.Builder
	for _, g := range toolGuidelines {
		guidelinesBuf.WriteString("- ")
		guidelinesBuf.WriteString(g)
		guidelinesBuf.WriteString("\n")
	}
	for _, g := range combinationGuidelines(toolNames) {
		guidelinesBuf.WriteString("- ")
		guidelinesBuf.WriteString(g)
		guidelinesBuf.WriteString("\n")
	}
	for _, g := range BaseGuidelines {
		guidelinesBuf.WriteString("- ")
		guidelinesBuf.WriteString(g)
		guidelinesBuf.WriteString("\n")
	}

	var appendBlock string
	if s := strings.TrimSpace(opts.AppendSystem); s != "" {
		appendBlock = "\n" + s + "\n"
	}

	var contextBlock string
	if len(opts.ContextFiles) > 0 {
		var cb strings.Builder
		cb.WriteString("\n## Project Context\n\nProject-specific instructions and guidelines (read carefully):\n")
		for _, cf := range opts.ContextFiles {
			cb.WriteString("\n### ")
			cb.WriteString(cf.RelPath)
			cb.WriteString("\n\n")
			cb.WriteString(cf.Content)
			if !strings.HasSuffix(cf.Content, "\n") {
				cb.WriteString("\n")
			}
		}
		contextBlock = cb.String()
	}

	var skillsBlock string
	if opts.HasReadTool && len(opts.Skills) > 0 {
		skillsBlock = "\n" + formatSkillsBlock(opts.Skills)
	}

	now := opts.Now
	if now.IsZero() {
		now = time.Now()
	}

	return SystemPromptData{
		Intro:          SystemIntro,
		ToolsList:      toolsBuf.String(),
		GuidelinesList: guidelinesBuf.String(),
		AppendSystem:   appendBlock,
		ContextFiles:   contextBlock,
		SkillsBlock:    skillsBlock,
		Cwd:            opts.Cwd,
		Date:           now.Format("2006-01-02"),
	}
}

// gatherToolMeta walks tools collecting (snippetByName, dedupedGuidelines, toolNameSet).
// Guideline order = first-seen-tool order.
func gatherToolMeta(tools []tool.Tool) (map[string]string, []string, map[string]bool) {
	snippets := make(map[string]string, len(tools))
	names := make(map[string]bool, len(tools))
	var guidelines []string
	seen := make(map[string]bool)
	for _, t := range tools {
		name := t.Name()
		names[name] = true
		pm, ok := t.(PromptMeta)
		if !ok {
			continue
		}
		if s := strings.TrimSpace(pm.PromptSnippet()); s != "" {
			snippets[name] = s
		}
		for _, g := range pm.PromptGuidelines() {
			g = strings.TrimSpace(g)
			if g == "" || seen[g] {
				continue
			}
			seen[g] = true
			guidelines = append(guidelines, g)
		}
	}
	return snippets, guidelines, names
}

// combinationGuidelines returns guidelines that depend on which tool combinations are present.
// Implements G1: when both bash and at least one of grep/find/ls are present, prefer the latter.
func combinationGuidelines(names map[string]bool) []string {
	var out []string
	hasBash := names["bash"]
	hasGrep := names["grep"]
	hasFind := names["find"]
	hasLs := names["ls"]
	hasTask := names["task_create"]
	hasDispatch := names["subagent"]

	switch {
	case hasBash && !hasGrep && !hasFind && !hasLs:
		out = append(out, "Use bash for file exploration (ls, rg, find).")
	case hasBash && (hasGrep || hasFind || hasLs):
		out = append(out, "Prefer grep/find/ls tools over bash for file exploration (faster, .gitignore aware).")
	}
	if hasTask {
		out = append(out, "For multi-step work, task_create the steps up front; task_update {status: in_progress} when you start one and {status: completed} immediately when done. When in doubt about ids or state, task_list first.")
	}
	if hasDispatch {
		out = append(out, "Dispatch large explorations to the explore subagent; dispatch multi-file plans to plan; hand off self-contained subtasks to general-purpose.")
	}
	return out
}
