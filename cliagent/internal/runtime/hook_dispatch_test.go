package runtime

import (
	"context"
	"errors"
	"testing"
)

func TestRunHookChain_OrderAndShortCircuit(t *testing.T) {
	var got []string
	h1 := Hook{Stage: StagePreToolUse, Fn: func(_ context.Context, _ HookInput) (HookOutput, error) {
		got = append(got, "h1")
		return HookOutput{}, nil
	}}
	h2 := Hook{Stage: StagePreToolUse, Fn: func(_ context.Context, _ HookInput) (HookOutput, error) {
		got = append(got, "h2")
		return HookOutput{Decision: DecisionDeny, Reason: "no"}, nil
	}}
	h3 := Hook{Stage: StagePreToolUse, Fn: func(_ context.Context, _ HookInput) (HookOutput, error) {
		got = append(got, "h3")
		return HookOutput{}, nil
	}}
	out, err := RunHookChain(context.Background(), []Hook{h1, h2, h3}, StagePreToolUse, HookInput{})
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if out.Decision != DecisionDeny || out.Reason != "no" {
		t.Errorf("out=%+v", out)
	}
	if len(got) != 2 || got[0] != "h1" || got[1] != "h2" {
		t.Errorf("got=%v", got)
	}
}

func TestRunHookChain_MatcherFiltersByToolName(t *testing.T) {
	called := false
	h := Hook{Stage: StagePreToolUse, Matcher: "^Bash$", Fn: func(_ context.Context, _ HookInput) (HookOutput, error) {
		called = true
		return HookOutput{}, nil
	}}
	_, _ = RunHookChain(context.Background(), []Hook{h}, StagePreToolUse, HookInput{ToolName: "Read"})
	if called {
		t.Fatal("hook fired despite matcher mismatch")
	}
	_, _ = RunHookChain(context.Background(), []Hook{h}, StagePreToolUse, HookInput{ToolName: "Bash"})
	if !called {
		t.Fatal("hook didn't fire for matching tool")
	}
}

func TestRunHookChain_ErrorPropagates(t *testing.T) {
	sentinel := errors.New("boom")
	h := Hook{Stage: StageStop, Fn: func(_ context.Context, _ HookInput) (HookOutput, error) {
		return HookOutput{}, sentinel
	}}
	_, err := RunHookChain(context.Background(), []Hook{h}, StageStop, HookInput{})
	if !errors.Is(err, sentinel) {
		t.Fatalf("err=%v want sentinel", err)
	}
}

func TestRunHookChain_SkipsOtherStages(t *testing.T) {
	called := false
	h := Hook{Stage: StagePostToolUse, Fn: func(_ context.Context, _ HookInput) (HookOutput, error) {
		called = true
		return HookOutput{}, nil
	}}
	_, _ = RunHookChain(context.Background(), []Hook{h}, StagePreToolUse, HookInput{})
	if called {
		t.Fatal("hook fired on wrong stage")
	}
}

func TestRunHookChain_MergesAdditionalContext(t *testing.T) {
	h1 := Hook{Stage: StageUserPromptSubmit, Fn: func(_ context.Context, _ HookInput) (HookOutput, error) {
		return HookOutput{AdditionalContext: "alpha"}, nil
	}}
	h2 := Hook{Stage: StageUserPromptSubmit, Fn: func(_ context.Context, _ HookInput) (HookOutput, error) {
		return HookOutput{AdditionalContext: "beta"}, nil
	}}
	out, err := RunHookChain(context.Background(), []Hook{h1, h2}, StageUserPromptSubmit, HookInput{})
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if out.AdditionalContext != "alpha\nbeta" {
		t.Errorf("AdditionalContext=%q", out.AdditionalContext)
	}
}
