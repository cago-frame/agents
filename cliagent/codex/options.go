package codex

import (
	"time"

	"github.com/cago-frame/agents/tool"
)

const defaultMaxSteps = 25

type SandboxMode string

const (
	SandboxReadOnly         SandboxMode = "read-only"
	SandboxWorkspaceWrite   SandboxMode = "workspace-write"
	SandboxDangerFullAccess SandboxMode = "danger-full-access"
)

type ApprovalPolicy string

const (
	ApprovalUntrusted ApprovalPolicy = "untrusted"
	ApprovalOnFailure ApprovalPolicy = "on-failure"
	ApprovalOnRequest ApprovalPolicy = "on-request"
	ApprovalNever     ApprovalPolicy = "never"
)

type backendCfg struct {
	binary       string
	model        string
	cwd          string
	env          map[string]string
	systemPrompt string
	sandbox      SandboxMode
	approval     ApprovalPolicy

	bypass    bool
	ephemeral bool

	config      []string
	extraArgs   []string
	eventBuffer int
	maxSteps    int
	killGrace   time.Duration

	appRunner appServerRunner

	// ─── Runner-level fields (consumed by Runner.New, ignored by Backend()) ───

	// agentTools 是 Runner 暴露给 Codex 的工具集（通过 MCP loopback bridge）。
	agentTools []tool.Tool

	// agentHooks 是 Runner 收集的 native hook 注册。
	agentHooks []hookSpec
}

type Option func(*backendCfg)

func defaultBackendCfg() backendCfg {
	return backendCfg{
		binary:      codexBinaryName,
		approval:    ApprovalNever,
		eventBuffer: 64,
		maxSteps:    defaultMaxSteps,
		killGrace:   10 * time.Second,
		appRunner:   &execAppServerRunner{},
	}
}

func Binary(path string) Option { return func(c *backendCfg) { c.binary = path } }

func Model(model string) Option { return func(c *backendCfg) { c.model = model } }

func Cwd(path string) Option { return func(c *backendCfg) { c.cwd = path } }

func Env(kv map[string]string) Option { return func(c *backendCfg) { c.env = kv } }

func SystemPrompt(prompt string) Option {
	return func(c *backendCfg) { c.systemPrompt = prompt }
}

func Sandbox(mode SandboxMode) Option { return func(c *backendCfg) { c.sandbox = mode } }

func Approval(policy ApprovalPolicy) Option { return func(c *backendCfg) { c.approval = policy } }

func DangerouslyBypassApprovalsAndSandbox() Option {
	return func(c *backendCfg) { c.bypass = true }
}

func Ephemeral() Option { return func(c *backendCfg) { c.ephemeral = true } }

// Config appends a raw Codex CLI config override in key=value form.
// The value side is parsed by Codex as TOML, matching `codex -c`.
func Config(expr string) Option {
	return func(c *backendCfg) {
		if expr != "" {
			c.config = append(c.config, expr)
		}
	}
}

func ExtraArgs(args []string) Option {
	return func(c *backendCfg) { c.extraArgs = append(c.extraArgs, args...) }
}

func EventBuffer(n int) Option {
	return func(c *backendCfg) {
		if n > 0 {
			c.eventBuffer = n
		}
	}
}

func MaxSteps(n int) Option {
	return func(c *backendCfg) {
		if n > 0 {
			c.maxSteps = n
		}
	}
}

func KillGrace(d time.Duration) Option { return func(c *backendCfg) { c.killGrace = d } }

func WithAppServerRunnerForTesting(r appServerRunner) Option {
	return func(c *backendCfg) {
		c.appRunner = r
	}
}
