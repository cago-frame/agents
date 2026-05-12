package claudecode

import (
	"time"

	"github.com/cago-frame/agents/tool"
)

// defaultMaxSteps is the default per-Stream LLM-turn cap (parity with the builtin backend).
const defaultMaxSteps = 25

// defaultBinary 是 claude CLI 在 PATH 中的默认名字; Binary() 选项可覆盖。
const defaultBinary = "claude"

// backendCfg holds configuration for a backend instance.
type backendCfg struct {
	binary                string
	model                 string
	systemPrompt          string
	env                   map[string]string
	permission            PermissionMode
	allowed               []string
	disallowed            []string
	maxTurns              int
	extraArgs             []string
	strictAllowed         bool
	interactivePermission bool

	runner      processRunner
	killGrace   time.Duration
	initTimeout time.Duration
	eventBuffer int
	cwd         string
	maxSteps    int

	// ─── Runner-level fields (consumed by Runner.New, ignored by Backend()) ───

	// agentTools 是 Runner 暴露给 CLI 的工具集（通过 MCP loopback bridge）。
	agentTools []tool.Tool

	// agentHooks 是 Runner 收集的 native hook 注册。
	agentHooks []hookSpec
}

// Option is a constructor option for claudecode.Backend.
type Option func(*backendCfg)

func defaultBackendCfg() backendCfg {
	return backendCfg{
		binary:      defaultBinary,
		runner:      &execRunner{},
		killGrace:   10 * time.Second,
		initTimeout: 30 * time.Second,
		eventBuffer: 64,
		maxSteps:    defaultMaxSteps,
	}
}

// Binary sets the claude CLI binary path (default "claude").
func Binary(path string) Option { return func(c *backendCfg) { c.binary = path } }

// Model sets --model.
func Model(m string) Option { return func(c *backendCfg) { c.model = m } }

// Cwd sets the working directory for the claude process.
func Cwd(path string) Option { return func(c *backendCfg) { c.cwd = path } }

// Env sets environment variables for the claude process (merged into os.Environ).
func Env(kv map[string]string) Option { return func(c *backendCfg) { c.env = kv } }

// SystemPrompt sets --append-system-prompt.
func SystemPrompt(s string) Option { return func(c *backendCfg) { c.systemPrompt = s } }

// Permission sets --permission-mode.
func Permission(m PermissionMode) Option { return func(c *backendCfg) { c.permission = m } }

// AllowedTools sets --allowedTools.
func AllowedTools(t []string) Option { return func(c *backendCfg) { c.allowed = t } }

// DisallowedTools sets --disallowedTools.
func DisallowedTools(t []string) Option { return func(c *backendCfg) { c.disallowed = t } }

// MaxTurns sets --max-turns.
func MaxTurns(n int) Option { return func(c *backendCfg) { c.maxTurns = n } }

// MaxSteps sets the maximum agentic steps per run.
func MaxSteps(n int) Option {
	return func(c *backendCfg) {
		if n > 0 {
			c.maxSteps = n
		}
	}
}

// ExtraArgs appends extra argv to the claude CLI invocation.
func ExtraArgs(args []string) Option {
	return func(c *backendCfg) { c.extraArgs = append(c.extraArgs, args...) }
}

// KillGrace sets the SIGTERM→SIGKILL grace period (default 10s).
func KillGrace(d time.Duration) Option { return func(c *backendCfg) { c.killGrace = d } }

// InitTimeout sets the maximum wait for system.init after first user frame (default 30s).
func InitTimeout(d time.Duration) Option {
	return func(c *backendCfg) { c.initTimeout = d }
}

// EventBuffer sets the Stream dispatcher queue capacity (default 64).
func EventBuffer(n int) Option {
	return func(c *backendCfg) {
		if n > 0 {
			c.eventBuffer = n
		}
	}
}

// StrictAllowedTools disables auto-merge of mcp bridge tools into --allowedTools.
func StrictAllowedTools(v bool) Option {
	return func(c *backendCfg) { c.strictAllowed = v }
}

// InteractivePermission enables --permission-prompt-tool stdio, causing the CLI
// to emit control_request frames for tool permission decisions. The Go consumer
// handles these via the EventPermissionRequest event and Stream.RespondPermission.
func InteractivePermission() Option {
	return func(c *backendCfg) { c.interactivePermission = true }
}

// InteractiveMode sets the permission mode to "default" and enables
// --permission-prompt-tool stdio. All tool permission decisions flow
// through EventPermissionRequest events in Go code.
func InteractiveMode() Option {
	return func(c *backendCfg) {
		c.permission = PermissionModeDefault
		c.interactivePermission = true
	}
}

// IncludePartialMessages is a no-op; the runner always passes
// --include-partial-messages since the translator handles partial deltas.
func IncludePartialMessages() Option {
	return func(c *backendCfg) {}
}

// Debug enables --debug with an optional filter (empty = all).
func Debug(filter string) Option {
	if filter == "" {
		return ExtraArgs([]string{string(flagDebug)})
	}
	return ExtraArgs([]string{string(flagDebug), filter})
}

// WithProcessRunnerForTesting injects a fake processRunner for unit tests.
// Not for production use.
func WithProcessRunnerForTesting(r processRunner) Option {
	return func(c *backendCfg) { c.runner = r }
}
