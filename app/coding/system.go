package coding

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	agent "github.com/cago-frame/agents/agent"
	compactorpkg "github.com/cago-frame/agents/agent/compactor"
	"github.com/cago-frame/agents/provider"
	"github.com/cago-frame/agents/tool"
	"github.com/cago-frame/agents/tool/subagent"
)

const dispatchToolName = "dispatch_subagent"
const dispatchToolDesc = "把任务委派给专长子 agent（explore / plan / general-purpose 或调用者追加的）"

// System 是组装好的 Coding Agent 系统：父 agent + 子 agent 列表 + 状态 + 关闭手柄。
// 必须通过 New 构造；零值无效。
type System struct {
	parent     *agent.Agent
	subagents  []subagent.Entry // 实际使用的 entries（默认 + extra 合并后）
	parentJobs interface{ StopAll() error }
	closeOnce  sync.Once
	closeErr   error

	// dynamic prompt fields
	contextFiles       []ContextFile
	skills             []Skill
	slashRegistry      *SlashRegistry
	compactor          *compactor
	cachedSystemProm   string // stored at New time for test seam
	cachedGPSystemProm string // stored at New time for test seam
}

// New 组装一个 Coding Agent 系统。
//
//	ctx  : 用于 setup 期间的日志 / trace 上下文（context.Background() 也可）
//	prov : LLM provider（父 + 默认所有子 agent 共用）
//	cwd  : 工具的工作目录
//	opts : 见各 WithXxx
//
// 调用者必须 defer sys.Close(ctx) 释放子 agent + 后台 bash。
func New(ctx context.Context, prov provider.Provider, cwd string, opts ...Option) (*System, error) {
	cfg := defaultSystemConfig()
	for _, o := range opts {
		o(&cfg)
	}

	// ── 1. Discover project context ──────────────────────────────────────────
	var contextFiles []ContextFile
	if !cfg.disableContextFiles {
		if len(cfg.contextDirsOverride) > 0 {
			contextFiles = loadContextDirsLiteral(cfg.contextDirsOverride)
		} else {
			var err error
			contextFiles, err = LoadProjectContext(ctx, cwd)
			if err != nil {
				contextFiles = nil // non-fatal; continue without context
			}
		}
	}

	// ── 2. Discover skills ───────────────────────────────────────────────────
	var skills []Skill
	if !cfg.disableSkills {
		if len(cfg.skillDirsOverride) > 0 {
			skills = loadSkillDirsLiteral(ctx, cfg.skillDirsOverride)
		} else {
			var err error
			skills, err = LoadSkills(ctx, cwd)
			if err != nil {
				skills = nil // non-fatal
			}
		}
	}

	// ── 3. Build subagent entries ────────────────────────────────────────────
	defaults := []subagent.Entry{
		Explore(prov, cwd, defaultExploreOpts(cfg)...),
		Plan(prov, cwd, defaultPlanOpts(cfg)...),
	}
	var gpSystemProm string
	if cfg.includeGP {
		var gpEntry subagent.Entry
		var err error
		gpEntry, gpSystemProm, err = gpEntryWithWeb(prov, cwd, cfg, contextFiles, skills)
		if err != nil {
			return nil, err
		}
		defaults = append(defaults, gpEntry)
	}

	// Merge cfg.extraSubagents (same Type replaces, otherwise append).
	final, replacedErr := mergeSubagents(defaults, cfg.extraSubagents)

	// ── 4. Build parent tool list (agent native; no legacy.Adapt). ────────
	sess := NewSession(cwd)
	parentTools := append([]tool.Tool{}, sess.All()...)
	if cfg.search != nil {
		parentTools = append(parentTools, cfg.search.build()...)
	}
	if cfg.fetch != nil {
		parentTools = append(parentTools, cfg.fetch.build()...)
	}
	parentTools = append(parentTools, subagent.NewTool(dispatchToolName, dispatchToolDesc, final))
	parentTools = append(parentTools, cfg.extraTools...)

	// ── 5. Build parent system prompt ────────────────────────────────────────
	hasRead := toolsHave(parentTools, "read")
	sysPrompt, err := BuildSystemPrompt(SystemPromptOptions{
		Cwd:          cwd,
		Tools:        parentTools,
		AppendSystem: cfg.systemAppend,
		ContextFiles: contextFiles,
		Skills:       skills,
		HasReadTool:  hasRead,
		Template:     cfg.systemTemplate,
	})
	if err != nil {
		return nil, err
	}

	// ── 6. Build compactor (single object; auto + manual share strategy). ────
	var cmpactor *compactor
	if !cfg.disableCompaction {
		cmpactor = newCompactor(prov, cfg.model, cfg.compactSpec, cfg.compactThreshold)
	}

	// ── 7. Build agent options (construction-time only) ───────────────────────
	agentOpts := []agent.Option{
		agent.Tools(parentTools...),
		agent.System(sysPrompt),
	}
	if cfg.model != "" {
		agentOpts = append(agentOpts, agent.Model(cfg.model))
	}
	if cmpactor != nil && cfg.compactThreshold > 0 {
		agentOpts = append(agentOpts, compactorpkg.WithStrategy(cmpactor.strategy))
	}
	// 调用者通过 WithAgentOpts 注入的额外选项放最后，允许覆盖默认值。
	agentOpts = append(agentOpts, cfg.extraAgentOpts...)

	// ── 8. Construct parent agent ─────────────────────────────────────────────
	parent := agent.New(prov, agentOpts...)

	// ── 9. Slash registry ─────────────────────────────────────────────────────
	var slashReg *SlashRegistry
	if !cfg.disableSlash {
		slashReg = newSlashRegistry()
		// Load templates.
		var templates []Template
		if len(cfg.slashDirsOverride) > 0 {
			templates = loadTemplatesDirsLiteral(ctx, cfg.slashDirsOverride)
		} else {
			var err error
			templates, err = LoadTemplates(ctx, cwd)
			if err != nil {
				templates = nil // non-fatal
			}
		}
		for i := range templates {
			t := templates[i]
			slashReg.templates[t.Name] = &t
		}
	}

	// ── 10. Assemble System ───────────────────────────────────────────────────
	sys := &System{
		parent:             parent,
		subagents:          final,
		parentJobs:         sess.Jobs(),
		contextFiles:       contextFiles,
		skills:             skills,
		slashRegistry:      slashReg,
		compactor:          cmpactor,
		cachedSystemProm:   sysPrompt,
		cachedGPSystemProm: gpSystemProm,
	}
	if replacedErr != nil {
		// 被替换的默认 Entry close 失败：不阻塞 New，把 error 记到 closeErr 里，
		// 后续 sys.Close 时会被聚合返回。
		sys.closeErr = replacedErr
	}

	// Register builtins in slash registry.
	if slashReg != nil {
		registerBuiltins(slashReg, sys)
	}

	return sys, nil
}

// Agent 返回组装好的父 agent。
func (s *System) Agent() *agent.Agent { return s.parent }

// SlashRegistry exposes the merged registry. nil when slash commands disabled.
func (s *System) SlashRegistry() *SlashRegistry { return s.slashRegistry }

// Compact runs a manual compaction on conv. Returns ErrCompactionDisabled if WithoutCompaction was set.
func (s *System) Compact(ctx context.Context, conv *agent.Conversation, opts ...CompactOption) (*CompactionResult, error) {
	if s.compactor == nil {
		return nil, ErrCompactionDisabled
	}
	co := CompactOptions{}
	for _, o := range opts {
		o(&co)
	}
	if co.Trigger == "" {
		co.Trigger = "manual"
	}
	return s.compactor.compact(ctx, conv, co)
}

// Close 关闭父 bash JobManager。agent *Agent 无需 Close。
// 幂等（sync.Once 保护）；多个错误用 errors.Join 累计。
func (s *System) Close(ctx context.Context) error {
	_ = ctx
	s.closeOnce.Do(func() {
		var errs []error
		if s.closeErr != nil {
			errs = append(errs, s.closeErr)
			s.closeErr = nil
		}
		if s.parentJobs != nil {
			if err := s.parentJobs.StopAll(); err != nil {
				errs = append(errs, err)
			}
		}
		s.closeErr = errors.Join(errs...)
	})
	return s.closeErr
}

// parentSystemPromptForTest is a package-private test seam that returns the
// system prompt computed during New. Tests in the same package can call it.
func (s *System) parentSystemPromptForTest() string { return s.cachedSystemProm }

// gpSystemPromptForTest is a package-private test seam that returns the GP subagent
// system prompt computed during New. Empty string when GP is disabled.
func (s *System) gpSystemPromptForTest() string { return s.cachedGPSystemProm }

// ─── helpers ───────────────────────────────────────────────────────────────

// defaultExploreOpts / defaultPlanOpts 把 systemConfig 里的 subagent override 翻译成
// Explore / Plan 的 SubagentOption 切片。
func defaultExploreOpts(cfg systemConfig) []SubagentOption {
	var out []SubagentOption
	if m, ok := cfg.subagentModels["explore"]; ok && m != "" {
		out = append(out, SubagentWithModel(m))
	}
	if s, ok := cfg.subagentSystems["explore"]; ok {
		out = append(out, SubagentWithSystem(s))
	}
	return out
}

func defaultPlanOpts(cfg systemConfig) []SubagentOption {
	var out []SubagentOption
	if m, ok := cfg.subagentModels["plan"]; ok && m != "" {
		out = append(out, SubagentWithModel(m))
	}
	if s, ok := cfg.subagentSystems["plan"]; ok {
		out = append(out, SubagentWithSystem(s))
	}
	return out
}

// gpEntryWithWeb 构造 GeneralPurpose Entry，并把 cfg 里的 web 注入塞进 GP 工具集。
// 这里要绕过 GeneralPurpose 默认工厂（默认无 web），手动用 SubagentWithTools 注入。
// 同时注入与父相同的 contextFiles + skills，除非调用者已通过
// WithSubagentSystem("general-purpose", ...) 显式覆盖系统提示词。
//
// Returns the Entry, the effective system prompt (for test seam / caching), and any
// template error from BuildSystemPrompt.
func gpEntryWithWeb(prov provider.Provider, cwd string, cfg systemConfig, contextFiles []ContextFile, skills []Skill) (subagent.Entry, string, error) {
	gpTools := generalPurposeTools(cwd, cfg.search, cfg.fetch)
	opts := []SubagentOption{SubagentWithTools(gpTools...)}
	if m, ok := cfg.subagentModels["general-purpose"]; ok && m != "" {
		opts = append(opts, SubagentWithModel(m))
	}
	// Build dynamic system prompt for GP (includes project context + skills) unless
	// the caller has already provided an explicit system override for GP.
	var gpSystemPrompt string
	if _, hasUserOverride := cfg.subagentSystems["general-purpose"]; !hasUserOverride {
		built, err := BuildSystemPrompt(SystemPromptOptions{
			Cwd:          cwd,
			Tools:        gpTools,
			AppendSystem: GeneralPurposePrompt,
			ContextFiles: contextFiles,
			Skills:       skills,
			HasReadTool:  toolsHave(gpTools, "read"),
			Template:     cfg.systemTemplate,
		})
		if err != nil {
			return subagent.Entry{}, "", err
		}
		gpSystemPrompt = built
	} else {
		gpSystemPrompt = cfg.subagentSystems["general-purpose"]
	}
	opts = append(opts, SubagentWithSystem(gpSystemPrompt))
	return GeneralPurpose(prov, cwd, opts...), gpSystemPrompt, nil
}

// mergeSubagents 把默认 entries 与 extra 合并：同 Type 替换 default、否则 append。
// *agent.Agent 没有 Close，被替换的默认 Entry 直接丢弃即可。
// error 返回值留作占位（当前总是 nil），以便日后 child 真有清理需求时无须改签名。
func mergeSubagents(defaults, extras []subagent.Entry) ([]subagent.Entry, error) {
	if len(extras) == 0 {
		return defaults, nil
	}
	byType := map[string]int{}
	out := append([]subagent.Entry{}, defaults...)
	for i, e := range out {
		byType[e.Type] = i
	}
	for _, e := range extras {
		if idx, dup := byType[e.Type]; dup {
			out[idx] = e
		} else {
			byType[e.Type] = len(out)
			out = append(out, e)
		}
	}
	return out, nil
}

// toolsHave returns true if any tool in the slice has the given name.
func toolsHave(tools []tool.Tool, name string) bool {
	for _, t := range tools {
		if t.Name() == name {
			return true
		}
	}
	return false
}

// loadContextDirsLiteral scans the given dirs literally (no upward walk) for context files.
func loadContextDirsLiteral(dirs []string) []ContextFile {
	var out []ContextFile
	for _, d := range dirs {
		for _, name := range contextFilenames {
			full := filepath.Join(d, name)
			data, err := os.ReadFile(full) //nolint:gosec // dir is caller-provided
			if err != nil {
				continue
			}
			out = append(out, ContextFile{AbsPath: full, RelPath: full, Content: string(data)})
		}
	}
	return out
}

// loadSkillDirsLiteral scans the given dirs literally for skills.
func loadSkillDirsLiteral(ctx context.Context, dirs []string) []Skill {
	out := make([]Skill, 0, len(dirs))
	for _, d := range dirs {
		out = append(out, scanSkillDir(ctx, d, "project")...)
	}
	return out
}

// loadTemplatesDirsLiteral scans the given dirs literally for slash command templates.
func loadTemplatesDirsLiteral(ctx context.Context, dirs []string) []Template {
	out := make([]Template, 0, len(dirs))
	for _, d := range dirs {
		out = append(out, scanCommandDir(ctx, d)...)
	}
	return out
}

// registerBuiltins wires the /compact and /help builtins into the registry.
func registerBuiltins(reg *SlashRegistry, sys *System) {
	reg.builtins["compact"] = Builtin{
		Name:        "compact",
		Description: "Compact session history to free context. Optional hint forwarded to summarizer.",
		Run: func(ctx context.Context, conv *agent.Conversation, args []string) (BuiltinOutput, error) {
			hint := strings.Join(args, " ")
			res, err := sys.Compact(ctx, conv, WithCompactHint(hint), WithCompactTrigger("slash"))
			if err != nil {
				return BuiltinOutput{}, err
			}
			if res == nil {
				return BuiltinOutput{Notice: "No compaction needed (history too short)."}, nil
			}
			return BuiltinOutput{
				Notice: fmt.Sprintf("Compaction complete: %d → %d messages.", res.Before, res.After),
			}, nil
		},
	}
	reg.builtins["help"] = Builtin{
		Name:        "help",
		Description: "List available slash commands.",
		Run: func(_ context.Context, _ *agent.Conversation, _ []string) (BuiltinOutput, error) {
			return BuiltinOutput{Notice: renderHelp(reg)}, nil
		},
	}
}

// renderHelp produces a text listing of all registered slash commands.
func renderHelp(reg *SlashRegistry) string {
	var b strings.Builder
	b.WriteString("Built-in commands:\n")
	for _, name := range sortedKeys(reg.builtins) {
		b.WriteString("  /")
		b.WriteString(name)
		b.WriteString(" — ")
		b.WriteString(reg.builtins[name].Description)
		b.WriteString("\n")
	}
	if len(reg.templates) > 0 {
		b.WriteString("\nTemplates:\n")
		keys := sortedKeys(reg.templates)
		for _, k := range keys {
			t := reg.templates[k]
			b.WriteString("  /")
			b.WriteString(k)
			if t.ArgumentHint != "" {
				b.WriteString(" ")
				b.WriteString(t.ArgumentHint)
			}
			if t.Description != "" {
				b.WriteString(" — ")
				b.WriteString(t.Description)
			}
			b.WriteString("\n")
		}
	}
	return b.String()
}

// sortedKeys returns sorted keys of a map.
func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
