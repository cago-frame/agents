package claudecode

import (
	"github.com/cago-frame/agents/tool"
)

// 本文件定义 claudecode runner-level 的 native Option 构造器:Tools /
// PreToolUse / PostToolUse / UserPromptSubmit / Stop / SubagentStop / SessionStart /
// SessionEnd / Notification。它们配置的是 Go 侧通过 MCP bridge / hook 链注入
// 到 CLI 的运行时上下文,而不是 CLI 进程级 argv 配置(那部分在 options.go)。
//
// 两组 Option 共用同一个 Option 类型(func(*backendCfg)),通过 backendCfg 内的
// agentTools / agentHooks 字段区分。

// Tools 把若干 tool.Tool (= agent.Tool) 注册到 Runner;运行时通过 MCP bridge
// 暴露给 CLI。多次调用累加。
func Tools(ts ...tool.Tool) Option {
	return func(c *backendCfg) {
		c.agentTools = append(c.agentTools, ts...)
	}
}

// PreToolUse 注册一个 PreToolUse hook;matcher 走 regex,空串视作 ".*"(匹配所有)。
func PreToolUse(matcher string, fn HookFunc) Option {
	return registerHook(StagePreToolUse, matcher, fn)
}

// PostToolUse 注册一个 PostToolUse hook。
func PostToolUse(matcher string, fn HookFunc) Option {
	return registerHook(StagePostToolUse, matcher, fn)
}

// UserPromptSubmit 注册一个 UserPromptSubmit hook(matcher 不适用)。
func UserPromptSubmit(fn HookFunc) Option {
	return registerHook(StageUserPromptSubmit, "", fn)
}

// Stop 注册一个 Stop hook(matcher 不适用)。
func Stop(fn HookFunc) Option {
	return registerHook(StageStop, "", fn)
}

// SubagentStop 注册一个 SubagentStop hook(仅 claudecode CLI 触发)。
func SubagentStop(fn HookFunc) Option {
	return registerHook(StageSubagentStop, "", fn)
}

// SessionStart 注册一个 SessionStart hook。
func SessionStart(fn HookFunc) Option {
	return registerHook(StageSessionStart, "", fn)
}

// SessionEnd 注册一个 SessionEnd hook。
func SessionEnd(fn HookFunc) Option {
	return registerHook(StageSessionEnd, "", fn)
}

// Notification 注册一个 Notification hook。
func Notification(fn HookFunc) Option {
	return registerHook(StageNotification, "", fn)
}

func registerHook(stage HookStage, matcher string, fn HookFunc) Option {
	return func(c *backendCfg) {
		c.agentHooks = append(c.agentHooks, hookSpec{Stage: stage, Matcher: matcher, Fn: fn})
	}
}
