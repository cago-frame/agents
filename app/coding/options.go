package coding

import (
	agent "github.com/cago-frame/agents/agent"
	"github.com/cago-frame/agents/provider"
	"github.com/cago-frame/agents/tool"
	"github.com/cago-frame/agents/tool/subagent"
	"github.com/cago-frame/agents/tool/webfetch"
	"github.com/cago-frame/agents/tool/websearch"
)

// Option 配置 *System；同名 Option 后调用覆盖前调用。
type Option func(*systemConfig)

// systemConfig 累计所有 New 时的配置。各字段语义见对应 With* 方法。
type systemConfig struct {
	model          string
	systemAppend   string
	systemTemplate string

	subagentModels  map[string]string // typ -> model
	subagentSystems map[string]string // typ -> system

	extraTools     []tool.Tool
	extraSubagents []subagent.Entry
	extraAgentOpts []agent.Option
	includeGP      bool

	search *websearchSpec
	fetch  *webfetchSpec

	// project context
	disableContextFiles bool
	contextDirsOverride []string

	// skills
	disableSkills     bool
	skillDirsOverride []string

	// compaction
	compactSpec       *CompactSpec
	disableCompaction bool
	compactThreshold  int

	// slash commands
	disableSlash      bool
	slashDirsOverride []string
}

func defaultSystemConfig() systemConfig {
	return systemConfig{
		includeGP:       true,
		subagentModels:  map[string]string{},
		subagentSystems: map[string]string{},
	}
}

// WithModel 覆盖父 agent 的模型名。空字符串 = 不调 BuiltinModel，走 backend 默认。
func WithModel(name string) Option { return func(c *systemConfig) { c.model = name } }

// AppendSystem 在默认 SystemPrompt 之后追加（用 \n\n 分隔，注入到模板的 {{.AppendSystem}} 位）。
func AppendSystem(extra string) Option {
	return func(c *systemConfig) { c.systemAppend = extra }
}

// WithSystemTemplate 用自定义 Go text/template 渲染父 agent（及 GP 子 agent）的 system prompt。
// 模板变量见 SystemPromptData；空字符串等同于不调用此 Option（fallback 到 DefaultSystemTemplate）。
// 模板解析失败时 coding.New 会立即返回错误。
//
// 想完全替换 prompt（以前 WithSystem 的语义）：写一段不含 {{...}} 占位符的纯文本即可，
// 渲染结果就是这段字符串。
func WithSystemTemplate(tmpl string) Option {
	return func(c *systemConfig) { c.systemTemplate = tmpl }
}

// WithSubagentModel 覆盖某个默认子 agent 的模型名。typ ∈ {"explore","plan","general-purpose"}；
// 不在这三个里时 silently 无效。若该 typ 已被 WithExtraSubagents 替换，本 Option 不生效。
func WithSubagentModel(typ, name string) Option {
	return func(c *systemConfig) { c.subagentModels[typ] = name }
}

// WithSubagentSystem 覆盖某个默认子 agent 的 system prompt。语义同 WithSubagentModel。
func WithSubagentSystem(typ, prompt string) Option {
	return func(c *systemConfig) { c.subagentSystems[typ] = prompt }
}

// WithExtraTools 给父 agent 追加自定义 tool（除默认工具集 + dispatch_subagent 之外）。
func WithExtraTools(tools ...tool.Tool) Option {
	return func(c *systemConfig) { c.extraTools = append(c.extraTools, tools...) }
}

// WithAgentOpts 把额外的 agent.Option 追加到父 agent 的构造参数末尾。
// 用来注入 OnEvent / Hook 等不属于 coding 内部状态的横切关注点。语义对应
// SubagentWithAgentOpts，但作用于父 agent。多次调用累加；后写的同字段
// （如 agent.System）覆盖 coding.New 内部计算的默认值，调用者自负其责。
func WithAgentOpts(opts ...agent.Option) Option {
	return func(c *systemConfig) { c.extraAgentOpts = append(c.extraAgentOpts, opts...) }
}

// WithExtraSubagents 给 dispatch_subagent 工具追加 Entry。若 Entry.Type 与某个默认
// Entry（"explore"/"plan"/"general-purpose"）相同，**替换**默认 Entry（被替换的默认
// Entry 在 New 内部立即 Close 释放）；否则 append 到末尾。
func WithExtraSubagents(entries ...subagent.Entry) Option {
	return func(c *systemConfig) { c.extraSubagents = append(c.extraSubagents, entries...) }
}

// WithoutGeneralPurpose 关闭默认 GeneralPurpose 子 agent。
func WithoutGeneralPurpose() Option { return func(c *systemConfig) { c.includeGP = false } }

// websearchSpec 累计 WithSearch 注入参数；spec.build() 返回一个长度为 1 的 tool slice。
type websearchSpec struct {
	prov websearch.Provider
	opts []websearch.Option
}

func (s *websearchSpec) build() []tool.Tool {
	if s == nil {
		return nil
	}
	opts := append([]websearch.Option{websearch.WithProvider(s.prov)}, s.opts...)
	return []tool.Tool{websearch.New(opts...)}
}

// webfetchSpec 累计 WithFetch 注入参数；spec.build() 返回一个长度为 1 的 tool slice。
type webfetchSpec struct {
	opts []webfetch.Option
}

func (s *webfetchSpec) build() []tool.Tool {
	if s == nil {
		return nil
	}
	return []tool.Tool{webfetch.New(s.opts...)}
}

// WithSearch 让父 + GeneralPurpose 挂上 websearch 工具。p 必须非 nil；
// 调用者负责 p 的生命周期，本 System 不接管。
//
// 额外的 websearch.Option（如 AllowedDomains / DefaultMaxResults）通过 sopts 传入。
func WithSearch(p websearch.Provider, sopts ...websearch.Option) Option {
	return func(c *systemConfig) {
		c.search = &websearchSpec{prov: p, opts: sopts}
	}
}

// WithFetch 让父 + GeneralPurpose 挂上 webfetch 工具。无 summarizer；
// 想要 LLM 总结，自己用 webfetch.WithProvider + webfetch.Model 通过 fopts 注入。
func WithFetch(fopts ...webfetch.Option) Option {
	return func(c *systemConfig) {
		c.fetch = &webfetchSpec{opts: fopts}
	}
}

// WithFetchSummarizer 是 WithFetch 的便捷封装：注入 LLM provider + 模型，
// 让 webfetch 调用模型把抓回来的页面总结成短文本。
func WithFetchSummarizer(p provider.Provider, model string, fopts ...webfetch.Option) Option {
	all := append([]webfetch.Option{webfetch.WithProvider(p), webfetch.Model(model)}, fopts...)
	return WithFetch(all...)
}

// === Project context ===

// WithoutContextFiles disables auto-loading of CLAUDE.md / AGENTS.md.
func WithoutContextFiles() Option { return func(c *systemConfig) { c.disableContextFiles = true } }

// WithContextDirs replaces the default scan paths (default = ~/.claude + git chain).
// Provided dirs are scanned literally for CLAUDE.md / AGENTS.md (no upward walk).
func WithContextDirs(dirs ...string) Option {
	return func(c *systemConfig) {
		c.contextDirsOverride = append([]string{}, dirs...)
	}
}

// === Skills ===

// WithoutSkills disables auto-loading of skills.
func WithoutSkills() Option { return func(c *systemConfig) { c.disableSkills = true } }

// WithSkillDirs replaces the default skill scan paths.
func WithSkillDirs(dirs ...string) Option {
	return func(c *systemConfig) {
		c.skillDirsOverride = append([]string{}, dirs...)
	}
}

// === Compaction ===

// WithCompactor uses a separate provider/model for the summarizer LLM.
// nil Provider = reuse parent provider; empty Model = reuse parent model.
func WithCompactor(spec CompactSpec) Option {
	return func(c *systemConfig) { c.compactSpec = &spec }
}

// WithoutCompaction disables auto-compaction and makes System.Compact return ErrCompactionDisabled.
func WithoutCompaction() Option { return func(c *systemConfig) { c.disableCompaction = true } }

// WithCompactionThreshold sets the auto-compaction trigger (when last EventUsage.PromptTokens > N).
// 0 = no auto-trigger (manual System.Compact / /compact still work).
func WithCompactionThreshold(promptTokens int) Option {
	return func(c *systemConfig) { c.compactThreshold = promptTokens }
}

// === Slash commands ===

// WithoutSlashCommands disables the slash registry (System.SlashRegistry returns nil).
func WithoutSlashCommands() Option { return func(c *systemConfig) { c.disableSlash = true } }

// WithSlashDirs replaces the default slash-command scan paths.
func WithSlashDirs(dirs ...string) Option {
	return func(c *systemConfig) {
		c.slashDirsOverride = append([]string{}, dirs...)
	}
}
