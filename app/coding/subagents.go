package coding

import (
	"github.com/cago-frame/agents/agent"
	"github.com/cago-frame/agents/provider"
	"github.com/cago-frame/agents/tool"
	"github.com/cago-frame/agents/tool/find"
	"github.com/cago-frame/agents/tool/grep"
	"github.com/cago-frame/agents/tool/ls"
	"github.com/cago-frame/agents/tool/subagent"
)

// SubagentOption 是子 agent 工厂（Explore / Plan / GeneralPurpose）的 Option。
// 与 *System 的 Option（Option 类型）显式区分，调用者一眼看出 scope。
type SubagentOption func(*subagentConfig)

type subagentConfig struct {
	typ         string
	description string
	system      string
	model       string
	tools       []tool.Tool
	systemSet   bool // 区分"未设"和"显式空字符串"
	agentOpts   []agent.Option
}

// SubagentWithType 覆盖 Entry.Type。
func SubagentWithType(s string) SubagentOption { return func(c *subagentConfig) { c.typ = s } }

// SubagentWithDescription 覆盖 Entry.Description（必须单行短句，subagent.NewTool
// 内部把它拼进 dispatch 工具描述）。
func SubagentWithDescription(s string) SubagentOption {
	return func(c *subagentConfig) { c.description = s }
}

// SubagentWithSystem 覆盖系统提示词。空字符串 = 显式无 system。
func SubagentWithSystem(s string) SubagentOption {
	return func(c *subagentConfig) { c.system = s; c.systemSet = true }
}

// SubagentWithModel 覆盖模型名。空字符串 = 不调 BuiltinModel，让 backend 默认值生效。
func SubagentWithModel(s string) SubagentOption { return func(c *subagentConfig) { c.model = s } }

// SubagentWithTools 完全替换默认工具集。想要"默认 + 我的"，自行 append(coding.ReadOnly(cwd), myTool) 后传进来。
func SubagentWithTools(ts ...tool.Tool) SubagentOption {
	return func(c *subagentConfig) { c.tools = ts }
}

// SubagentWithAgentOpts appends extra agent.Option values into the child agent
// constructor. Used by system.go to attach observers or hooks to the child without
// exposing internals through the public API.
func SubagentWithAgentOpts(opts ...agent.Option) SubagentOption {
	return func(c *subagentConfig) { c.agentOpts = append(c.agentOpts, opts...) }
}

// applyDefaults 是 Explore / Plan / GeneralPurpose 共用的应用 Options 流程。
// 不同 subagent 的默认配置由 caller 先填好再传 opts。
func applyDefaults(cfg subagentConfig, opts []SubagentOption) subagentConfig {
	for _, o := range opts {
		o(&cfg)
	}
	return cfg
}

// buildEntry 用 cfg 构造一个真正跑起来的 child agent + Entry。
// cfg.system 为空字符串 = 显式无 system；非空 = 设置。
// Phase 3: child agents are built via agent.New; parent agent stays legacy.
func buildEntry(prov provider.Provider, cfg subagentConfig) subagent.Entry {
	opts := make([]agent.Option, 0, 2+len(cfg.agentOpts))
	opts = append(opts, agent.Tools(cfg.tools...))
	if cfg.system != "" {
		opts = append(opts, agent.System(cfg.system))
	}
	if cfg.model != "" {
		opts = append(opts, agent.Model(cfg.model))
	}
	opts = append(opts, cfg.agentOpts...)
	child := agent.New(prov, opts...)
	return subagent.Entry{
		Type:        cfg.typ,
		Description: cfg.description,
		Agent:       child,
	}
}

// Explore 返回一个「只读代码探索」子 agent Entry。默认：
//
//	Type        = "explore"
//	Description = "在仓库里搜代码 / 定位符号 / 拉文件片段（只读，不改写）"
//	System      = ExplorePrompt
//	Tools       = ReadOnly(cwd)
//
// agent *agent.Agent 没有 Close 方法；返回的 Entry 不需要显式收摊。
func Explore(prov provider.Provider, cwd string, opts ...SubagentOption) subagent.Entry {
	cfg := subagentConfig{
		typ:         "explore",
		description: "在仓库里搜代码 / 定位符号 / 拉文件片段（只读，不改写）",
		system:      ExplorePrompt,
		tools:       ReadOnly(cwd),
	}
	return buildEntry(prov, applyDefaults(cfg, opts))
}

// Plan 返回一个「架构规划」子 agent Entry。默认：
//
//	Type        = "plan"
//	Description = "为实现任务设计步骤化方案，识别关键文件并权衡架构取舍"
//	System      = PlanPrompt
//	Tools       = ReadOnly(cwd)
func Plan(prov provider.Provider, cwd string, opts ...SubagentOption) subagent.Entry {
	cfg := subagentConfig{
		typ:         "plan",
		description: "为实现任务设计步骤化方案，识别关键文件并权衡架构取舍",
		system:      PlanPrompt,
		tools:       ReadOnly(cwd),
	}
	return buildEntry(prov, applyDefaults(cfg, opts))
}

// generalPurposeTools 计算 GeneralPurpose 子 agent 的工具集：
// = NewSession(cwd) 的 Coding + grep/find/ls + 可选 web，**不含** task / dispatch。
// 独立 tracker + jobs（不与父共享）。
func generalPurposeTools(cwd string, search *websearchSpec, fetch *webfetchSpec) []tool.Tool {
	sess := NewSession(cwd)
	tools := append([]tool.Tool{}, sess.Coding()...) // read/write/edit + bash trio
	tools = append(tools,
		grep.New(grep.Cwd(sess.cwd)),
		find.New(find.Cwd(sess.cwd)),
		ls.New(ls.Cwd(sess.cwd)),
	)
	if search != nil {
		tools = append(tools, search.build()...)
	}
	if fetch != nil {
		tools = append(tools, fetch.build()...)
	}
	return tools
}

// GeneralPurpose 返回一个「通用任务执行」子 agent Entry。默认：
//
//	Type        = "general-purpose"
//	Description = "执行不属于 explore / plan 的杂活，工具同父但不能再分派"
//	System      = GeneralPurposePrompt
//	Tools       = generalPurposeTools(cwd, nil, nil)（无 web）
//
// 调用者用 coding.New(...) 时若开了 WithSearch / WithFetch，框架会把 web 工具拼进
// GP 的工具集；直接调 GeneralPurpose 工厂无 web 注入入口（要 web 自己用 SubagentWithTools 替换）。
func GeneralPurpose(prov provider.Provider, cwd string, opts ...SubagentOption) subagent.Entry {
	cfg := subagentConfig{
		typ:         "general-purpose",
		description: "执行不属于 explore / plan 的杂活，工具同父但不能再分派",
		system:      GeneralPurposePrompt,
		tools:       generalPurposeTools(cwd, nil, nil),
	}
	return buildEntry(prov, applyDefaults(cfg, opts))
}
