package claudecode

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/cago-frame/agents/cliagent/internal/runtime"
)

// TestWrapHookFunc_FieldCopy verifies all relevant fields flow from
// runtime.HookInput → claudecode.HookInput.
func TestWrapHookFunc_FieldCopy(t *testing.T) {
	captured := HookInput{}
	fn := func(_ context.Context, in HookInput) (*HookOutput, error) {
		captured = in
		return nil, nil
	}
	wrapped := wrapHookFunc(fn)

	rin := runtime.HookInput{
		Stage:    runtime.StagePreToolUse,
		ToolName: "Bash",
		Input:    json.RawMessage(`{"cmd":"ls"}`),
		Output:   json.RawMessage(`"out"`),
		Prompt:   "do thing",
		Cwd:      "/tmp",
		Raw:      json.RawMessage(`{"raw":1}`),
	}
	out, err := wrapped(context.Background(), rin)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if out.Decision != runtime.DecisionUnset || out.AdditionalContext != "" {
		t.Errorf("expected zero output, got %#v", out)
	}

	if captured.Stage != StagePreToolUse {
		t.Errorf("Stage = %q, want PreToolUse", captured.Stage)
	}
	if captured.ToolName != "Bash" {
		t.Errorf("ToolName = %q", captured.ToolName)
	}
	if string(captured.ToolInput) != `{"cmd":"ls"}` {
		t.Errorf("ToolInput = %s", captured.ToolInput)
	}
	if string(captured.ToolResponse) != `"out"` {
		t.Errorf("ToolResponse = %s", captured.ToolResponse)
	}
	if captured.Prompt != "do thing" {
		t.Errorf("Prompt = %q", captured.Prompt)
	}
	if captured.Cwd != "/tmp" {
		t.Errorf("Cwd = %q", captured.Cwd)
	}
	if string(captured.Raw) != `{"raw":1}` {
		t.Errorf("Raw = %s", captured.Raw)
	}
}

// TestWrapHookFunc_DecisionApprove maps DecisionApprove and Reason through.
func TestWrapHookFunc_DecisionApprove(t *testing.T) {
	wrapped := wrapHookFunc(func(_ context.Context, _ HookInput) (*HookOutput, error) {
		return &HookOutput{
			Decision:          DecisionApprove,
			Reason:            "looks fine",
			AdditionalContext: "extra",
		}, nil
	})
	out, err := wrapped(context.Background(), runtime.HookInput{Stage: runtime.StagePreToolUse})
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if out.Decision != runtime.DecisionApprove {
		t.Errorf("Decision = %v", out.Decision)
	}
	if out.Reason != "looks fine" {
		t.Errorf("Reason = %q", out.Reason)
	}
	if out.AdditionalContext != "extra" {
		t.Errorf("AdditionalContext = %q", out.AdditionalContext)
	}
}

// TestWrapHookFunc_DecisionDeny maps DecisionDeny.
func TestWrapHookFunc_DecisionDeny(t *testing.T) {
	wrapped := wrapHookFunc(func(_ context.Context, _ HookInput) (*HookOutput, error) {
		return &HookOutput{Decision: DecisionDeny, Reason: "no"}, nil
	})
	out, err := wrapped(context.Background(), runtime.HookInput{Stage: runtime.StagePreToolUse})
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if out.Decision != runtime.DecisionDeny {
		t.Errorf("Decision = %v", out.Decision)
	}
	if out.Reason != "no" {
		t.Errorf("Reason = %q", out.Reason)
	}
}

// TestWrapHookFunc_StopRunFromContinueFalse maps Continue=false → StopRun=true,
// preferring StopReason over Reason when set.
func TestWrapHookFunc_StopRunFromContinueFalse(t *testing.T) {
	cont := false
	wrapped := wrapHookFunc(func(_ context.Context, _ HookInput) (*HookOutput, error) {
		return &HookOutput{
			Continue:   &cont,
			StopReason: "user_quit",
			Reason:     "fallback",
		}, nil
	})
	out, err := wrapped(context.Background(), runtime.HookInput{Stage: runtime.StageStop})
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if !out.StopRun {
		t.Errorf("expected StopRun=true")
	}
	if out.Reason != "user_quit" {
		t.Errorf("Reason = %q, want user_quit", out.Reason)
	}
}

// TestWrapHookFunc_NilOutput maps nil → empty HookOutput.
func TestWrapHookFunc_NilOutput(t *testing.T) {
	wrapped := wrapHookFunc(func(_ context.Context, _ HookInput) (*HookOutput, error) { return nil, nil })
	out, err := wrapped(context.Background(), runtime.HookInput{Stage: runtime.StagePreToolUse})
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if out.Decision != runtime.DecisionUnset || out.StopRun || out.AdditionalContext != "" {
		t.Errorf("expected zero output, got %#v", out)
	}
}

// TestWrapHookFunc_ErrorPassthrough propagates the wrapped function's error.
func TestWrapHookFunc_ErrorPassthrough(t *testing.T) {
	want := errors.New("hook bombed")
	wrapped := wrapHookFunc(func(_ context.Context, _ HookInput) (*HookOutput, error) { return nil, want })
	out, err := wrapped(context.Background(), runtime.HookInput{})
	if !errors.Is(err, want) {
		t.Errorf("err = %v, want %v", err, want)
	}
	if out.Decision != runtime.DecisionUnset {
		t.Errorf("expected zero output on err, got %#v", out)
	}
}

// TestWrapHookFunc_SuppressOutput surfaces SuppressOutput=true.
func TestWrapHookFunc_SuppressOutput(t *testing.T) {
	wrapped := wrapHookFunc(func(_ context.Context, _ HookInput) (*HookOutput, error) {
		return &HookOutput{SuppressOutput: true}, nil
	})
	out, _ := wrapped(context.Background(), runtime.HookInput{Stage: runtime.StagePostToolUse})
	if !out.SuppressOutput {
		t.Errorf("SuppressOutput should be true")
	}
}

// TestStageRoundTrip verifies stageRuntimeToCC ∘ stageCCToRuntime is identity
// on every supported stage.
func TestStageRoundTrip(t *testing.T) {
	stages := []runtime.HookStage{
		runtime.StagePreToolUse,
		runtime.StagePostToolUse,
		runtime.StageUserPromptSubmit,
		runtime.StageStop,
		runtime.StageSubagentStop,
		runtime.StageSessionStart,
		runtime.StageSessionEnd,
		runtime.StageNotification,
	}
	for _, s := range stages {
		ccName := stageRuntimeToCC(s)
		round := stageCCToRuntime(HookStage(ccName))
		if round != s {
			t.Errorf("round-trip stage %v: ccName=%q → %v", s, ccName, round)
		}
	}
}

// TestIsFieldApplicable spot-checks the matrix.
func TestIsFieldApplicable(t *testing.T) {
	cases := []struct {
		stage HookStage
		field HookOutputField
		want  bool
	}{
		{StagePreToolUse, FieldDecisionDeny, true},
		{StagePreToolUse, FieldDecisionApprove, true},
		{StagePostToolUse, FieldDecisionApprove, false},
		{StagePostToolUse, FieldSuppressOutput, true},
		{StageUserPromptSubmit, FieldAdditionalContext, true},
		{StageNotification, FieldStopRun, false},
	}
	for _, c := range cases {
		if got := IsFieldApplicable(c.stage, c.field); got != c.want {
			t.Errorf("IsFieldApplicable(%v,%v) = %v, want %v", c.stage, c.field, got, c.want)
		}
	}
}
