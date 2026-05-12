package coding

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cago-frame/agents/agent"
	"github.com/cago-frame/agents/provider"
	"github.com/cago-frame/agents/provider/providertest"
)

func TestSystem_New_BuildsDynamicSystemPrompt(t *testing.T) {
	mock := providertest.New()
	cwd := t.TempDir()
	mustWrite(t, filepath.Join(cwd, "CLAUDE.md"), "Project: cago/agents")

	sys, err := New(t.Context(), mock, cwd, WithoutCompaction(), WithoutSkills(), WithoutSlashCommands())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sys.Close(context.Background()) })

	got := sys.parentSystemPromptForTest()
	if !strings.Contains(got, "Project: cago/agents") {
		t.Errorf("system prompt missing context: %q", got)
	}
}

func TestSystem_Compact_UsedBySlashBuiltin(t *testing.T) {
	mock := providertest.New()
	sys, err := New(t.Context(), mock, t.TempDir(), WithoutContextFiles(), WithoutSkills())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sys.Close(context.Background()) })

	reg := sys.SlashRegistry()
	if reg == nil {
		t.Fatal("slash registry should be non-nil by default")
	}
	res, err := reg.Resolve("/help")
	if err != nil {
		t.Fatal(err)
	}
	if !res.IsBuiltin || res.BuiltinName != "help" {
		t.Fatalf("res=%+v", res)
	}
	out, err := res.Run(context.Background(), agent.NewConversation())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.Notice, "/help") || !strings.Contains(out.Notice, "/compact") {
		t.Errorf("help output missing builtins: %q", out.Notice)
	}
}

func TestSystem_WithoutCompaction_ReturnsErr(t *testing.T) {
	mock := providertest.New()
	sys, err := New(t.Context(), mock, t.TempDir(), WithoutCompaction(), WithoutContextFiles(), WithoutSkills(), WithoutSlashCommands())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sys.Close(context.Background()) })

	_, err = sys.Compact(context.Background(), agent.NewConversation())
	if !errors.Is(err, ErrCompactionDisabled) {
		t.Fatalf("expected ErrCompactionDisabled, got %v", err)
	}
}

// TestSystem_AutoCompact_NoOpWhenThresholdZero verifies that New does not panic
// and wires compactor correctly when no threshold is set (default 0 = no auto-trigger).
func TestSystem_AutoCompact_NoOpWhenThresholdZero(t *testing.T) {
	mock := providertest.New()
	sys, err := New(t.Context(), mock, t.TempDir(), WithoutContextFiles(), WithoutSkills(), WithoutSlashCommands())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sys.Close(context.Background()) })

	if sys.compactor == nil {
		t.Errorf("compactor should be created when compaction enabled (default)")
	}
	if sys.compactor.strategy.threshold != 0 {
		t.Errorf("threshold should default to 0 (no auto-trigger)")
	}
}

// TestSystem_AutoCompact_TurnEndHookFires drives a turn through Runner.Send
// and confirms the compactor.WithStrategy hook (registered when
// WithCompactionThreshold > 0) emits EventCompacted after the turn.
//
// The compactStrategy.ShouldCompact gate requires:
//
//	(a) most-recent assistant Usage.PromptTokens >= threshold
//	(b) len(messages) - keepRecent (default 6) > 0 after walking back over
//	    consecutive tool messages
//
// Runner.Send only adds 2 messages per turn (1 user + 1 assistant), so we
// pre-seed the conversation with 6 plain user/assistant text messages. After
// the turn runs we have 8 messages: startKeep = 8 - 6 = 2 > 0, gate passes.
func TestSystem_AutoCompact_TurnEndHookFires(t *testing.T) {
	mock := providertest.New()
	// Turn 1: assistant streams text + Usage above threshold.
	mock.QueueStream(
		provider.StreamChunk{ContentDelta: "first"},
		provider.StreamChunk{Usage: &provider.Usage{PromptTokens: 200}, FinishReason: provider.FinishStop},
	)
	// Compaction summarizer call (manual ChatCompletion, not stream).
	mock.Queue(&provider.CompletionResponse{
		Role:         provider.RoleAssistant,
		Content:      "compacted summary",
		FinishReason: provider.FinishStop,
	})

	sys, err := New(t.Context(), mock, t.TempDir(),
		WithoutContextFiles(), WithoutSkills(), WithoutSlashCommands(),
		WithCompactionThreshold(100),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sys.Close(context.Background()) })

	a := sys.Agent()
	conv := agent.NewConversation()
	// Pre-seed enough plain messages so post-turn message count clears the
	// keepRecent (default 6) gate inside compactStrategy.ShouldCompact.
	for i := 0; i < 3; i++ {
		conv.Append(agent.Message{
			Role:    agent.RoleUser,
			Content: []agent.ContentBlock{agent.TextBlock{Text: "old user msg"}},
		})
		conv.Append(agent.Message{
			Role:    agent.RoleAssistant,
			Content: []agent.ContentBlock{agent.TextBlock{Text: "old assistant msg"}},
		})
	}

	r := a.Runner(conv)
	defer func() { _ = r.Close() }()

	events, err := r.Send(context.Background(), "ping")
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	var sawCompacted bool
	for ev := range events {
		if ev.Kind == agent.EventCompacted {
			sawCompacted = true
		}
	}
	if !sawCompacted {
		t.Fatal("EventCompacted not observed")
	}
}

func TestSystem_GP_InheritsContextAndSkills(t *testing.T) {
	mock := providertest.New()
	cwd := t.TempDir()
	mustWrite(t, filepath.Join(cwd, "CLAUDE.md"), "Project: cago/agents-gp-test")
	mustMkdir(t, filepath.Join(cwd, ".git"))

	skillDir := filepath.Join(cwd, ".claude", "skills", "demo")
	mustMkdir(t, skillDir)
	mustWrite(t, filepath.Join(skillDir, "SKILL.md"),
		"---\nname: demo\ndescription: Use for demos\n---\nbody")

	sys, err := New(t.Context(), mock, cwd, WithoutCompaction(), WithoutSlashCommands())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = sys.Close(context.Background()) }()

	gpSysPrompt := sys.gpSystemPromptForTest()
	if gpSysPrompt == "" {
		t.Fatal("gpSystemPromptForTest returned empty string — GP may be disabled or system not cached")
	}
	if !strings.Contains(gpSysPrompt, "Project: cago/agents-gp-test") {
		t.Errorf("GP system prompt missing project context; got (first 400): %q", truncate(gpSysPrompt, 400))
	}
	if !strings.Contains(gpSysPrompt, "demo") {
		t.Errorf("GP system prompt missing skill name 'demo'; got (first 400): %q", truncate(gpSysPrompt, 400))
	}
	// Also verify GP-specific guidance is present (GeneralPurposePrompt appended).
	if !strings.Contains(gpSysPrompt, "general-purpose") && !strings.Contains(gpSysPrompt, "subtask") {
		t.Errorf("GP system prompt missing GP-specific guidance; got (first 400): %q", truncate(gpSysPrompt, 400))
	}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
