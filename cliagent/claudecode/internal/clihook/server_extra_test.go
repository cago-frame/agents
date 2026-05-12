package clihook

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestInjectAdditionalContext(t *testing.T) {
	out := InjectAdditionalContext(PostToolUse, "extra info")
	if out == nil || out.HookSpecificOutput == nil {
		t.Fatalf("got %+v", out)
	}
	if out.HookSpecificOutput["hookEventName"] != string(PostToolUse) {
		t.Errorf("hookEventName = %v", out.HookSpecificOutput["hookEventName"])
	}
	if out.HookSpecificOutput["additionalContext"] != "extra info" {
		t.Errorf("additionalContext = %v", out.HookSpecificOutput["additionalContext"])
	}
}

func TestDenyTool(t *testing.T) {
	out := DenyTool("policy violation")
	if out == nil || out.Decision != "block" || out.Reason != "policy violation" {
		t.Errorf("got %+v", out)
	}
}

func TestBuildSettingsJSON_EmptyHooks(t *testing.T) {
	s := NewServer()
	got, err := s.BuildSettingsJSON()
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if got != "" {
		t.Errorf("empty hooks: got %q, want empty", got)
	}
}

func TestBuildSettingsJSON_GroupedByStage(t *testing.T) {
	s := NewServer()
	if err := s.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer s.Shutdown(context.Background()) //nolint:errcheck

	noopFn := func(_ context.Context, _ Input) (*Output, error) { return nil, nil }
	s.AddHook(Entry{Stage: PreToolUse, Matcher: "Bash", Fn: noopFn})
	s.AddHook(Entry{Stage: PreToolUse, Matcher: "Read", Fn: noopFn})
	s.AddHook(Entry{Stage: PostToolUse, Fn: noopFn}) // no matcher

	raw, err := s.BuildSettingsJSON()
	if err != nil {
		t.Fatalf("BuildSettingsJSON: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(raw), &got); err != nil {
		t.Fatalf("decode: %v\nraw=%s", err, raw)
	}
	hooks, ok := got["hooks"].(map[string]any)
	if !ok {
		t.Fatalf("hooks field: %#v", got["hooks"])
	}
	preEntries, ok := hooks[string(PreToolUse)].([]any)
	if !ok || len(preEntries) != 2 {
		t.Fatalf("PreToolUse entries: %#v", hooks[string(PreToolUse)])
	}
	postEntries, ok := hooks[string(PostToolUse)].([]any)
	if !ok || len(postEntries) != 1 {
		t.Fatalf("PostToolUse entries: %#v", hooks[string(PostToolUse)])
	}

	// PreToolUse[0] should carry matcher Bash; PostToolUse[0] should carry no matcher.
	pre0 := preEntries[0].(map[string]any)
	if pre0["matcher"] == nil || pre0["matcher"] != "Bash" {
		t.Errorf("Pre[0] matcher: %#v", pre0)
	}
	post0 := postEntries[0].(map[string]any)
	if _, hasMatcher := post0["matcher"]; hasMatcher {
		t.Errorf("Post[0] should have no matcher, got %#v", post0)
	}

	// The command should embed --unix-socket and the hook ID.
	hooksArr := pre0["hooks"].([]any)
	cmd := hooksArr[0].(map[string]any)["command"].(string)
	if !strings.Contains(cmd, "--unix-socket") || !strings.Contains(cmd, "X-Hook: h0") {
		t.Errorf("command = %q", cmd)
	}
}

func TestStart_Idempotent(t *testing.T) {
	s := NewServer()
	if err := s.Start(); err != nil {
		t.Fatalf("first Start: %v", err)
	}
	defer s.Shutdown(context.Background()) //nolint:errcheck
	first := s.SocketPath()
	if err := s.Start(); err != nil {
		t.Errorf("second Start: %v", err)
	}
	if s.SocketPath() != first {
		t.Errorf("path changed across Start calls: %q vs %q", first, s.SocketPath())
	}
}

func TestShutdown_BeforeStartNoOp(t *testing.T) {
	s := NewServer()
	if err := s.Shutdown(context.Background()); err != nil {
		t.Errorf("Shutdown before Start: %v", err)
	}
}

func TestSocketPath_BeforeStart(t *testing.T) {
	s := NewServer()
	if got := s.SocketPath(); got != "" {
		t.Errorf("SocketPath before Start = %q, want empty", got)
	}
}

func TestAddHook_AssignsSequentialIDs(t *testing.T) {
	s := NewServer()
	noopFn := func(_ context.Context, _ Input) (*Output, error) { return nil, nil }
	id1 := s.AddHook(Entry{Stage: PreToolUse, Fn: noopFn})
	id2 := s.AddHook(Entry{Stage: PostToolUse, Fn: noopFn})
	if id1 != "h0" || id2 != "h1" {
		t.Errorf("ids = %q,%q, want h0,h1", id1, id2)
	}
	snap := s.SnapshotHooks()
	if len(snap) != 2 || snap[0].ID != "h0" || snap[1].ID != "h1" {
		t.Errorf("snapshot: %+v", snap)
	}
}
