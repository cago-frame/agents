package claudecode

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/cago-frame/agents/cliagent/claudecode/internal/clihook"
	"github.com/cago-frame/agents/cliagent/internal/runtime"
)

// TestNewHookBridge_RegistersStageHook verifies a single PreToolUse hook with
// matcher renders into the clihook.Server registry.
func TestNewHookBridge_RegistersStageHook(t *testing.T) {
	hooks := []runtime.Hook{{
		Stage:   runtime.StagePreToolUse,
		Matcher: "Bash",
		Fn: func(_ context.Context, _ runtime.HookInput) (runtime.HookOutput, error) {
			return runtime.HookOutput{}, nil
		},
	}}
	hb, err := newHookBridge(hooks)
	if err != nil {
		t.Fatalf("newHookBridge: %v", err)
	}
	defer func() { _ = hb.shutdown(context.Background()) }()

	snap := hb.srv.SnapshotHooks()
	if len(snap) != 1 {
		t.Fatalf("expected 1 hook, got %d", len(snap))
	}
	if snap[0].Stage != clihook.PreToolUse {
		t.Errorf("Stage = %v, want PreToolUse", snap[0].Stage)
	}
	if snap[0].Matcher != "Bash" {
		t.Errorf("Matcher = %q", snap[0].Matcher)
	}
}

// TestNewHookBridge_SettingsJSONShape verifies BuildSettingsJSON yields a JSON
// object containing the right matchers and curl commands.
func TestNewHookBridge_SettingsJSONShape(t *testing.T) {
	hooks := []runtime.Hook{
		{
			Stage:   runtime.StagePreToolUse,
			Matcher: "Bash",
			Fn: func(_ context.Context, _ runtime.HookInput) (runtime.HookOutput, error) {
				return runtime.HookOutput{}, nil
			},
		},
		{
			Stage: runtime.StagePostToolUse,
			Fn: func(_ context.Context, _ runtime.HookInput) (runtime.HookOutput, error) {
				return runtime.HookOutput{}, nil
			},
		},
	}
	hb, err := newHookBridge(hooks)
	if err != nil {
		t.Fatalf("newHookBridge: %v", err)
	}
	defer func() { _ = hb.shutdown(context.Background()) }()

	// Need to start so the server has a UDS path for HookCommand to embed.
	if err := hb.start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	js, err := hb.settingsJSON()
	if err != nil {
		t.Fatalf("settingsJSON: %v", err)
	}
	if js == "" {
		t.Fatalf("expected non-empty settings JSON")
	}

	var settings map[string]any
	if err := json.Unmarshal([]byte(js), &settings); err != nil {
		t.Fatalf("settings JSON not valid: %v", err)
	}
	hooksField, ok := settings["hooks"].(map[string]any)
	if !ok {
		t.Fatalf("missing top-level hooks field: %v", settings)
	}
	pre, ok := hooksField[string(clihook.PreToolUse)].([]any)
	if !ok || len(pre) != 1 {
		t.Fatalf("missing PreToolUse list: %v", hooksField)
	}
	preEntry := pre[0].(map[string]any)
	if preEntry["matcher"] != "Bash" {
		t.Errorf("matcher = %v", preEntry["matcher"])
	}
	cmds := preEntry["hooks"].([]any)
	if len(cmds) != 1 {
		t.Fatalf("expected 1 command")
	}
	cmd := cmds[0].(map[string]any)
	cmdStr, _ := cmd["command"].(string)
	if !strings.Contains(cmdStr, "--unix-socket") {
		t.Errorf("expected curl --unix-socket in command, got %q", cmdStr)
	}
	if !strings.Contains(cmdStr, "X-Hook:") {
		t.Errorf("expected X-Hook header, got %q", cmdStr)
	}

	post, ok := hooksField[string(clihook.PostToolUse)].([]any)
	if !ok || len(post) != 1 {
		t.Fatalf("missing PostToolUse list: %v", hooksField)
	}
}

// TestNewHookBridge_EmptyHooks yields empty settings JSON.
func TestNewHookBridge_EmptyHooks(t *testing.T) {
	hb, err := newHookBridge(nil)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	defer func() { _ = hb.shutdown(context.Background()) }()

	js, err := hb.settingsJSON()
	if err != nil {
		t.Fatalf("settingsJSON: %v", err)
	}
	if js != "" {
		t.Errorf("expected empty settings JSON for no hooks, got %q", js)
	}
}

// TestTranslateHookOutput_DenyPreTool maps Decision=Deny on PreToolUse → "block".
func TestTranslateHookOutput_DenyPreTool(t *testing.T) {
	cli := translateHookOutput(runtime.StagePreToolUse, runtime.HookOutput{
		Decision: runtime.DecisionDeny,
		Reason:   "no",
	})
	if cli == nil {
		t.Fatalf("nil output")
	}
	if cli.Decision != "block" {
		t.Errorf("Decision = %q, want block", cli.Decision)
	}
	if cli.Reason != "no" {
		t.Errorf("Reason = %q", cli.Reason)
	}
}

// TestTranslateHookOutput_ApprovePreTool maps Decision=Approve on PreToolUse → "approve".
func TestTranslateHookOutput_ApprovePreTool(t *testing.T) {
	cli := translateHookOutput(runtime.StagePreToolUse, runtime.HookOutput{
		Decision: runtime.DecisionApprove,
		Reason:   "ok",
	})
	if cli == nil || cli.Decision != "approve" || cli.Reason != "ok" {
		t.Fatalf("unexpected: %#v", cli)
	}
}

// TestTranslateHookOutput_DropsApproveOnNonPreTool — Approve only valid on PreToolUse.
func TestTranslateHookOutput_DropsApproveOnNonPreTool(t *testing.T) {
	cli := translateHookOutput(runtime.StagePostToolUse, runtime.HookOutput{
		Decision: runtime.DecisionApprove,
	})
	if cli != nil {
		t.Errorf("expected nil (no applicable fields), got %#v", cli)
	}
}

// TestTranslateHookOutput_StopRun maps StopRun=true → Continue=false on stages
// that allow it.
func TestTranslateHookOutput_StopRun(t *testing.T) {
	cli := translateHookOutput(runtime.StageUserPromptSubmit, runtime.HookOutput{
		StopRun: true,
		Reason:  "abort",
	})
	if cli == nil || cli.Continue == nil || *cli.Continue != false {
		t.Fatalf("unexpected: %#v", cli)
	}
	if cli.StopReason != "abort" {
		t.Errorf("StopReason = %q", cli.StopReason)
	}
}

// TestTranslateHookOutput_AdditionalContextOnPostTool wraps additionalContext
// into HookSpecificOutput.
func TestTranslateHookOutput_AdditionalContextOnPostTool(t *testing.T) {
	cli := translateHookOutput(runtime.StagePostToolUse, runtime.HookOutput{
		AdditionalContext: "some extra",
	})
	if cli == nil {
		t.Fatalf("nil output")
	}
	if cli.HookSpecificOutput == nil {
		t.Fatalf("HookSpecificOutput nil")
	}
	if got := cli.HookSpecificOutput["additionalContext"]; got != "some extra" {
		t.Errorf("additionalContext = %v", got)
	}
	if got := cli.HookSpecificOutput["hookEventName"]; got != string(clihook.PostToolUse) {
		t.Errorf("hookEventName = %v", got)
	}
}

// TestTranslateHookOutput_SuppressOutput only valid on PostToolUse.
func TestTranslateHookOutput_SuppressOutput(t *testing.T) {
	if cli := translateHookOutput(runtime.StagePostToolUse, runtime.HookOutput{SuppressOutput: true}); cli == nil || !cli.SuppressOutput {
		t.Errorf("expected SuppressOutput=true on PostToolUse, got %#v", cli)
	}
	if cli := translateHookOutput(runtime.StagePreToolUse, runtime.HookOutput{SuppressOutput: true}); cli != nil {
		t.Errorf("SuppressOutput should be dropped on PreToolUse, got %#v", cli)
	}
}

// TestStageToCLIStage covers all eight stages and unknown.
func TestStageToCLIStage(t *testing.T) {
	pairs := []struct {
		in   runtime.HookStage
		want clihook.Stage
		ok   bool
	}{
		{runtime.StagePreToolUse, clihook.PreToolUse, true},
		{runtime.StagePostToolUse, clihook.PostToolUse, true},
		{runtime.StageUserPromptSubmit, clihook.UserPromptSubmit, true},
		{runtime.StageStop, clihook.Stop, true},
		{runtime.StageSubagentStop, clihook.SubagentStop, true},
		{runtime.StageSessionStart, clihook.SessionStart, true},
		{runtime.StageSessionEnd, clihook.SessionEnd, true},
		{runtime.StageNotification, clihook.Notification, true},
		{runtime.HookStage("nonsense"), "", false},
	}
	for _, p := range pairs {
		got, ok := stageToCLIStage(p.in)
		if got != p.want || ok != p.ok {
			t.Errorf("stageToCLIStage(%q) = (%q,%v), want (%q,%v)", p.in, got, ok, p.want, p.ok)
		}
	}
}

// TestNewHookBridge_UnknownStageSkipped — unknown runtime.HookStage shouldn't
// land in the clihook server registry.
func TestNewHookBridge_UnknownStageSkipped(t *testing.T) {
	hooks := []runtime.Hook{{
		Stage: runtime.HookStage("not_a_stage"),
		Fn: func(_ context.Context, _ runtime.HookInput) (runtime.HookOutput, error) {
			return runtime.HookOutput{}, nil
		},
	}}
	hb, err := newHookBridge(hooks)
	if err != nil {
		t.Fatalf("newHookBridge: %v", err)
	}
	defer func() { _ = hb.shutdown(context.Background()) }()

	if got := len(hb.srv.SnapshotHooks()); got != 0 {
		t.Errorf("expected 0 hooks (unknown stage skipped), got %d", got)
	}
}
