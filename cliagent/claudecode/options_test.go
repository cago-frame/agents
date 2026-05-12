package claudecode

import (
	"testing"
	"time"
)

func TestOptions_Model(t *testing.T) {
	cfg := defaultBackendCfg()
	Model("sonnet")(&cfg)
	if cfg.model != "sonnet" {
		t.Fatalf("model = %q, want %q", cfg.model, "sonnet")
	}
}

func TestOptions_Binary(t *testing.T) {
	cfg := defaultBackendCfg()
	Binary("/usr/local/bin/claude")(&cfg)
	if cfg.binary != "/usr/local/bin/claude" {
		t.Fatalf("binary = %q", cfg.binary)
	}
}

func TestOptions_Cwd(t *testing.T) {
	cfg := defaultBackendCfg()
	Cwd("/tmp")(&cfg)
	if cfg.cwd != "/tmp" {
		t.Fatalf("cwd = %q", cfg.cwd)
	}
}

func TestOptions_KillGrace(t *testing.T) {
	cfg := defaultBackendCfg()
	KillGrace(5 * time.Second)(&cfg)
	if cfg.killGrace != 5*time.Second {
		t.Fatalf("killGrace = %v", cfg.killGrace)
	}
}

func TestOptions_InitTimeout(t *testing.T) {
	cfg := defaultBackendCfg()
	InitTimeout(60 * time.Second)(&cfg)
	if cfg.initTimeout != 60*time.Second {
		t.Fatalf("initTimeout = %v", cfg.initTimeout)
	}
}

func TestOptions_EventBuffer(t *testing.T) {
	cfg := defaultBackendCfg()
	EventBuffer(128)(&cfg)
	if cfg.eventBuffer != 128 {
		t.Fatalf("eventBuffer = %d", cfg.eventBuffer)
	}
}

func TestOptions_EventBuffer_Zero_Ignored(t *testing.T) {
	cfg := defaultBackendCfg()
	orig := cfg.eventBuffer
	EventBuffer(0)(&cfg)
	if cfg.eventBuffer != orig {
		t.Fatalf("EventBuffer(0) should not change value, got %d", cfg.eventBuffer)
	}
}

func TestOptions_StrictAllowedTools(t *testing.T) {
	cfg := defaultBackendCfg()
	StrictAllowedTools(true)(&cfg)
	if !cfg.strictAllowed {
		t.Fatalf("strictAllowed should be true")
	}
}

func TestOptions_Permission(t *testing.T) {
	cfg := defaultBackendCfg()
	Permission(PermissionModeBypassPermissions)(&cfg)
	if cfg.permission != PermissionModeBypassPermissions {
		t.Fatalf("permission = %v", cfg.permission)
	}
}

func TestOptions_Env(t *testing.T) {
	cfg := defaultBackendCfg()
	Env(map[string]string{"FOO": "bar"})(&cfg)
	if cfg.env["FOO"] != "bar" {
		t.Fatalf("env[FOO] = %q", cfg.env["FOO"])
	}
}

func TestOptions_SystemPrompt(t *testing.T) {
	cfg := defaultBackendCfg()
	SystemPrompt("be concise")(&cfg)
	if cfg.systemPrompt != "be concise" {
		t.Fatalf("systemPrompt = %q", cfg.systemPrompt)
	}
}

func TestOptions_MaxSteps(t *testing.T) {
	cfg := defaultBackendCfg()
	MaxSteps(10)(&cfg)
	if cfg.maxSteps != 10 {
		t.Fatalf("maxSteps = %d", cfg.maxSteps)
	}
}

func TestOptions_MaxSteps_Zero_Ignored(t *testing.T) {
	cfg := defaultBackendCfg()
	MaxSteps(5)(&cfg)
	MaxSteps(0)(&cfg) // zero ignored
	if cfg.maxSteps != 5 {
		t.Fatalf("MaxSteps(0) should not change value, got %d", cfg.maxSteps)
	}
}

func TestOptions_ExtraArgs(t *testing.T) {
	cfg := defaultBackendCfg()
	ExtraArgs([]string{"--foo", "bar"})(&cfg)
	if len(cfg.extraArgs) != 2 || cfg.extraArgs[0] != "--foo" {
		t.Fatalf("extraArgs = %v", cfg.extraArgs)
	}
}

func TestOptions_IncludePartialMessages(t *testing.T) {
	cfg := defaultBackendCfg()
	IncludePartialMessages()(&cfg)
	// IncludePartialMessages is now a no-op since headless mode always
	// passes --include-partial-messages via buildArgs.
	if cfg.interactivePermission {
		t.Fatal("IncludePartialMessages should not set interactivePermission")
	}
}

func TestOptions_Debug_NoFilter(t *testing.T) {
	cfg := defaultBackendCfg()
	Debug("")(&cfg)
	found := false
	for _, a := range cfg.extraArgs {
		if a == "--debug" {
			found = true
		}
	}
	if !found {
		t.Fatalf("--debug not in extraArgs: %v", cfg.extraArgs)
	}
}

func TestOptions_Debug_WithFilter(t *testing.T) {
	cfg := defaultBackendCfg()
	Debug("api,hooks")(&cfg)
	foundDebug := false
	foundFilter := false
	for i, a := range cfg.extraArgs {
		if a == "--debug" {
			foundDebug = true
			if i+1 < len(cfg.extraArgs) && cfg.extraArgs[i+1] == "api,hooks" {
				foundFilter = true
			}
		}
	}
	if !foundDebug || !foundFilter {
		t.Fatalf("extraArgs = %v", cfg.extraArgs)
	}
}

func TestOptions_AllowedTools(t *testing.T) {
	cfg := defaultBackendCfg()
	AllowedTools([]string{"Bash", "Read"})(&cfg)
	if len(cfg.allowed) != 2 || cfg.allowed[0] != "Bash" {
		t.Fatalf("allowed = %v", cfg.allowed)
	}
}

func TestOptions_DisallowedTools(t *testing.T) {
	cfg := defaultBackendCfg()
	DisallowedTools([]string{"Write"})(&cfg)
	if len(cfg.disallowed) != 1 || cfg.disallowed[0] != "Write" {
		t.Fatalf("disallowed = %v", cfg.disallowed)
	}
}

func TestOptions_MaxTurns(t *testing.T) {
	cfg := defaultBackendCfg()
	MaxTurns(10)(&cfg)
	if cfg.maxTurns != 10 {
		t.Fatalf("maxTurns = %d", cfg.maxTurns)
	}
}

func TestOptions_Defaults(t *testing.T) {
	cfg := defaultBackendCfg()
	if cfg.binary != "claude" {
		t.Fatalf("default binary = %q", cfg.binary)
	}
	if cfg.killGrace != 10*time.Second {
		t.Fatalf("default killGrace = %v", cfg.killGrace)
	}
	if cfg.initTimeout != 30*time.Second {
		t.Fatalf("default initTimeout = %v", cfg.initTimeout)
	}
	if cfg.eventBuffer != 64 {
		t.Fatalf("default eventBuffer = %d", cfg.eventBuffer)
	}
	if cfg.runner == nil {
		t.Fatalf("default runner is nil")
	}
	if cfg.maxSteps != defaultMaxSteps {
		t.Fatalf("default maxSteps = %d, want defaultMaxSteps (%d)", cfg.maxSteps, defaultMaxSteps)
	}
}
