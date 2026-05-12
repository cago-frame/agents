package codex

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/cago-frame/agents/agent"
	"github.com/cago-frame/agents/tool"
)

// --- options.go (native): Stop / SubagentStop / SessionStart / SessionEnd / Notification ---

func TestNativeOptions_AllStageOptions(t *testing.T) {
	cfg := defaultBackendCfg()
	noop := func(_ context.Context, _ HookInput) (*HookOutput, error) { return nil, nil }
	Stop(noop)(&cfg)
	SubagentStop(noop)(&cfg)
	SessionStart(noop)(&cfg)
	SessionEnd(noop)(&cfg)
	Notification(noop)(&cfg)

	wantStages := []HookStage{
		StageStop, StageSubagentStop, StageSessionStart, StageSessionEnd, StageNotification,
	}
	if len(cfg.agentHooks) != len(wantStages) {
		t.Fatalf("agentHooks len = %d, want %d", len(cfg.agentHooks), len(wantStages))
	}
	for i, want := range wantStages {
		if cfg.agentHooks[i].Stage != want {
			t.Errorf("hook[%d].Stage = %q, want %q", i, cfg.agentHooks[i].Stage, want)
		}
	}
}

// --- IsFieldApplicable matrix ---

func TestIsFieldApplicable(t *testing.T) {
	cases := []struct {
		stage HookStage
		field HookOutputField
		want  bool
	}{
		{StagePreToolUse, FieldDecisionDeny, true},
		{StagePreToolUse, FieldDecisionApprove, true},
		{StagePreToolUse, FieldStopRun, true},
		{StagePostToolUse, FieldStopRun, true},
		{StageUserPromptSubmit, FieldDecisionDeny, true},
		{StageUserPromptSubmit, FieldStopRun, true},
		{StageSessionStart, FieldStopRun, true},
		{StageSubagentStop, FieldStopRun, true},
		{StagePreToolUse, FieldAdditionalContext, true},
		{StagePostToolUse, FieldAdditionalContext, true},
		{StageUserPromptSubmit, FieldAdditionalContext, true},
		{StageSessionStart, FieldAdditionalContext, true},
		{StagePostToolUse, FieldSuppressOutput, true},
		// negatives
		{StageStop, FieldDecisionDeny, false},
		{StageNotification, FieldStopRun, false},
		{StageSessionEnd, FieldAdditionalContext, false},
	}
	for _, tc := range cases {
		if got := IsFieldApplicable(tc.stage, tc.field); got != tc.want {
			t.Errorf("IsFieldApplicable(%q,%q) = %v, want %v", tc.stage, tc.field, got, tc.want)
		}
	}
}

// --- State.Clone ---

func TestState_Clone(t *testing.T) {
	src := State{ThreadID: "t", Values: map[string]string{"a": "1"}}
	dst := src.Clone()
	if dst.ThreadID != "t" || dst.Values["a"] != "1" {
		t.Errorf("dst = %+v", dst)
	}
	dst.Values["a"] = "2"
	if src.Values["a"] != "1" {
		t.Error("Clone should yield independent Values")
	}

	empty := State{ThreadID: "x"}.Clone()
	if empty.Values != nil {
		t.Errorf("empty.Values = %v, want nil", empty.Values)
	}
}

// --- ExitError ---

func TestExitError(t *testing.T) {
	var nilErr *ExitError
	if got := nilErr.Error(); got != "" {
		t.Errorf("nil Error = %q, want empty", got)
	}
	if got := nilErr.Unwrap(); got != nil {
		t.Errorf("nil Unwrap = %v, want nil", got)
	}

	inner := errors.New("boom")
	noStderr := &ExitError{Err: inner}
	if !strings.Contains(noStderr.Error(), "boom") {
		t.Errorf("no-stderr Error = %q", noStderr.Error())
	}
	if !errors.Is(noStderr, inner) {
		t.Error("Unwrap should expose inner")
	}

	withStderr := &ExitError{Err: inner, Stderr: "  stderr line  "}
	got := withStderr.Error()
	if !strings.Contains(got, "boom") || !strings.Contains(got, "stderr line") {
		t.Errorf("with-stderr Error = %q", got)
	}
	if strings.Contains(got, "  stderr line  ") {
		t.Errorf("Error should TrimSpace stderr, got %q", got)
	}
}

// --- rpcError ---

func TestRPCErrorError(t *testing.T) {
	var nilErr *rpcError
	if got := nilErr.Error(); got != "" {
		t.Errorf("nil Error = %q", got)
	}

	plain := &rpcError{Code: -32600, Message: "invalid"}
	if got := plain.Error(); !strings.Contains(got, "-32600") || !strings.Contains(got, "invalid") {
		t.Errorf("plain Error = %q", got)
	}

	withData := &rpcError{Code: 1, Message: "x", Data: json.RawMessage(`{"k":"v"}`)}
	if got := withData.Error(); !strings.Contains(got, "k") {
		t.Errorf("with-data Error = %q", got)
	}
}

// wrapSession / wrapStream nil short-circuits no longer exist (Phase 5
// removed the agent.Session/agent.Stream wrappers). Tests are dropped.

// --- Runner.Session basic delegation (ID, State, History) ---

func TestRunner_SessionGetters(t *testing.T) {
	r := New()
	defer r.Close(context.Background()) //nolint:errcheck

	sess := r.Session()
	if sess == nil {
		t.Fatal("Session is nil")
	}
	if sess.ID() == "" {
		t.Error("ID is empty")
	}
	st := sess.State()
	_ = st // initial state has no ThreadID; just verify it doesn't panic.
	if got := sess.History(); len(got) != 0 {
		t.Errorf("History initial = %v", got)
	}
	// Steer / FollowUp on inactive session should return non-nil error (no active stream).
	if err := sess.Steer(context.Background(), "x"); err == nil {
		t.Error("Steer on idle session: want error, got nil")
	}
	if err := sess.FollowUp(context.Background(), "y"); err != nil {
		t.Logf("FollowUp idle: %v", err)
	}
	// Clear / Close should not panic.
	if err := sess.Clear(context.Background()); err != nil {
		t.Errorf("Clear: %v", err)
	}
	if err := sess.Close(context.Background()); err != nil {
		t.Errorf("Close: %v", err)
	}
}

// --- mcp_bridge ---

type bridgeTestTool struct {
	name string
}

func (t *bridgeTestTool) Name() string         { return t.name }
func (t *bridgeTestTool) Description() string  { return "test" }
func (t *bridgeTestTool) Schema() agent.Schema { return agent.Schema{Type: "object"} }
func (t *bridgeTestTool) Call(_ context.Context, _ map[string]any) (*agent.ToolResultBlock, error) {
	return &agent.ToolResultBlock{
		Content: []agent.ContentBlock{agent.TextBlock{Text: "ok"}},
	}, nil
}

func TestMCPBridge_RegisterStartShutdownApplyToSpec(t *testing.T) {
	mb := newMCPBridge()
	if mb == nil {
		t.Fatal("newMCPBridge returned nil")
	}

	// ensureRegistered: idempotent on duplicate.
	tt := &bridgeTestTool{name: "echo"}
	if err := mb.ensureRegistered([]tool.Tool{tt}); err != nil {
		t.Fatalf("ensureRegistered first: %v", err)
	}
	if err := mb.ensureRegistered([]tool.Tool{tt}); err != nil {
		t.Fatalf("ensureRegistered duplicate: %v", err)
	}

	ctx := context.Background()
	ep, err := mb.start(ctx)
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	if ep.URL == "" || ep.Token == "" {
		t.Errorf("endpoint missing fields: %+v", ep)
	}

	// Re-call start: idempotent.
	ep2, err := mb.start(ctx)
	if err != nil || ep2.URL != ep.URL {
		t.Errorf("start idempotent: ep=%v ep2=%v err=%v", ep, ep2, err)
	}

	// applyToSpec should populate spec fields.
	spec := &runSpec{}
	mb.applyToSpec(spec)
	if spec.mcpURL != ep.URL || spec.mcpServerName == "" || spec.mcpTokenEnv == "" {
		t.Errorf("applyToSpec: %+v", spec)
	}
	if spec.env == nil || spec.env[spec.mcpTokenEnv] != ep.Token {
		t.Errorf("applyToSpec env: %+v", spec.env)
	}

	if err := mb.shutdown(ctx); err != nil {
		t.Errorf("shutdown: %v", err)
	}
}

func TestMCPBridge_ApplyToSpecBeforeStartPanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic when applyToSpec is called before start")
		}
	}()
	mb := newMCPBridge()
	mb.applyToSpec(&runSpec{})
}

func TestMCPBridge_ShutdownNilBridgeNoOp(t *testing.T) {
	mb := &mcpBridge{} // bridge nil
	if err := mb.shutdown(context.Background()); err != nil {
		t.Errorf("shutdown nil-bridge: %v", err)
	}
}

func TestMCPBridge_RegisterDuplicateNameStillReturnsErrBridgeStart(t *testing.T) {
	mb := newMCPBridge()
	a := &bridgeTestTool{name: "dup"}
	if err := mb.ensureRegistered([]tool.Tool{a}); err != nil {
		t.Fatalf("first register: %v", err)
	}
	// Force the underlying bridge to consider this a brand-new tool by removing from cache.
	mb.tools = map[string]struct{}{} // clear our seen-set so we reach bridge.Register again
	if err := mb.ensureRegistered([]tool.Tool{a}); !errors.Is(err, ErrBridgeStart) {
		t.Errorf("err = %v, want wraps ErrBridgeStart", err)
	}
}
