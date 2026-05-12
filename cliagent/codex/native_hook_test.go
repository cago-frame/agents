package codex

import (
	"context"
	"testing"

	"github.com/cago-frame/agents/agent"
)

// nopTool 是 native_hook_test 用的占位工具。
type nopTool struct{}

func (nopTool) Name() string         { return "nop" }
func (nopTool) Description() string  { return "noop" }
func (nopTool) Schema() agent.Schema { return agent.Schema{Type: "object"} }
func (nopTool) Call(_ context.Context, _ map[string]any) (*agent.ToolResultBlock, error) {
	return &agent.ToolResultBlock{}, nil
}

// TestNativeHook_RegisterViaOption 验证 codex.PreToolUse / PostToolUse /
// UserPromptSubmit 等 Option 真的把 hook 写到 cfg.agentHooks。
func TestNativeHook_RegisterViaOption(t *testing.T) {
	cfg := defaultBackendCfg()
	noopFn := func(_ context.Context, _ HookInput) (*HookOutput, error) { return nil, nil }
	PreToolUse("Bash", noopFn)(&cfg)
	PostToolUse(".*", noopFn)(&cfg)
	UserPromptSubmit(noopFn)(&cfg)

	if len(cfg.agentHooks) != 3 {
		t.Fatalf("agentHooks: got %d want 3", len(cfg.agentHooks))
	}
	if cfg.agentHooks[0].Stage != StagePreToolUse || cfg.agentHooks[0].Matcher != "Bash" {
		t.Errorf("hook[0]: %+v", cfg.agentHooks[0])
	}
	if cfg.agentHooks[1].Stage != StagePostToolUse || cfg.agentHooks[1].Matcher != ".*" {
		t.Errorf("hook[1]: %+v", cfg.agentHooks[1])
	}
	if cfg.agentHooks[2].Stage != StageUserPromptSubmit {
		t.Errorf("hook[2]: %+v", cfg.agentHooks[2])
	}
}

// TestNativeOptions_Tools 验证 codex.Tools 把工具写到 cfg.agentTools。
func TestNativeOptions_Tools(t *testing.T) {
	cfg := defaultBackendCfg()
	Tools(nopTool{})(&cfg)

	if len(cfg.agentTools) != 1 {
		t.Fatalf("agentTools: got %d", len(cfg.agentTools))
	}
}
