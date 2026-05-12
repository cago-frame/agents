package claudecode

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/cago-frame/agents/cliagent/internal/runtime"
)

// TestTranslateRawEvent_SystemInit verifies system.init frames produce
// EventSessionStart populated with SessionID + Cwd.
func TestTranslateRawEvent_SystemInit(t *testing.T) {
	st := &translateState{toolCalls: map[string]toolCallState{}}
	raw := rawEvent{
		Type:      "system",
		Subtype:   "init",
		SessionID: "sess-123",
		Cwd:       "/tmp/repo",
		Model:     "claude-3-5-sonnet",
	}

	got := translateRawEvent(raw, st)
	if len(got) != 1 {
		t.Fatalf("expected 1 event, got %d", len(got))
	}
	ev := got[0]
	if ev.Kind != runtime.EventSessionStart {
		t.Fatalf("kind = %v, want EventSessionStart", ev.Kind)
	}
	if ev.SessionID != "sess-123" {
		t.Errorf("SessionID = %q, want sess-123", ev.SessionID)
	}
	if ev.Cwd != "/tmp/repo" {
		t.Errorf("Cwd = %q, want /tmp/repo", ev.Cwd)
	}
	if st.sessionID != "sess-123" {
		t.Errorf("state.sessionID not updated")
	}
}

// TestTranslateRawEvent_StreamTextDelta covers stream_event with text_delta.
func TestTranslateRawEvent_StreamTextDelta(t *testing.T) {
	st := &translateState{toolCalls: map[string]toolCallState{}}
	raw := rawEvent{
		Type: "stream_event",
		Event: &rawStreamEvent{
			Type: "content_block_delta",
			Delta: &rawStreamDelta{
				Type: "text_delta",
				Text: "hello",
			},
		},
	}

	got := translateRawEvent(raw, st)
	if len(got) != 1 || got[0].Kind != runtime.EventTextDelta {
		t.Fatalf("expected one EventTextDelta, got %#v", got)
	}
	if got[0].Text != "hello" {
		t.Errorf("Text = %q, want hello", got[0].Text)
	}
	if !st.partialActive {
		t.Errorf("partialActive should be true after a delta")
	}
}

// TestTranslateRawEvent_StreamThinkingDelta covers stream_event with thinking_delta.
func TestTranslateRawEvent_StreamThinkingDelta(t *testing.T) {
	st := &translateState{toolCalls: map[string]toolCallState{}}
	raw := rawEvent{
		Type: "stream_event",
		Event: &rawStreamEvent{
			Delta: &rawStreamDelta{
				Type:     "thinking_delta",
				Thinking: "thinking out loud",
			},
		},
	}

	got := translateRawEvent(raw, st)
	if len(got) != 1 || got[0].Kind != runtime.EventThinkingDelta {
		t.Fatalf("expected EventThinkingDelta, got %#v", got)
	}
	if got[0].Text != "thinking out loud" {
		t.Errorf("Text = %q", got[0].Text)
	}
}

// TestTranslateRawEvent_StreamEmptyDelta drops empty deltas without emitting.
func TestTranslateRawEvent_StreamEmptyDelta(t *testing.T) {
	st := &translateState{toolCalls: map[string]toolCallState{}}
	raw := rawEvent{
		Type:  "stream_event",
		Event: &rawStreamEvent{Delta: &rawStreamDelta{Type: "text_delta", Text: ""}},
	}
	if got := translateRawEvent(raw, st); len(got) != 0 {
		t.Fatalf("expected no events for empty delta, got %#v", got)
	}
}

// TestTranslateRawEvent_AssistantText emits EventTextDelta + EventMessageEnd
// when partialActive is false (no preceding stream_event).
func TestTranslateRawEvent_AssistantText(t *testing.T) {
	st := &translateState{toolCalls: map[string]toolCallState{}}
	raw := rawEvent{
		Type: "assistant",
		Message: &rawMessage{
			ID: "msg-1",
			Content: []rawContentPart{
				{Type: "text", Text: "hi there"},
			},
		},
	}

	got := translateRawEvent(raw, st)
	if len(got) != 2 {
		t.Fatalf("expected 2 events, got %d: %#v", len(got), got)
	}
	if got[0].Kind != runtime.EventTextDelta || got[0].Text != "hi there" {
		t.Errorf("first event = %#v, want TextDelta hi there", got[0])
	}
	if got[1].Kind != runtime.EventMessageEnd {
		t.Errorf("second event kind = %v, want MessageEnd", got[1].Kind)
	}
	if got[1].Message == nil || got[1].Message.Text != "hi there" {
		t.Errorf("MessageEnd payload wrong: %#v", got[1].Message)
	}
	if got[1].Message.Role != runtime.RoleAssistant {
		t.Errorf("Role = %v, want assistant", got[1].Message.Role)
	}
}

// TestTranslateRawEvent_AssistantTextWithPartialActive suppresses TextDelta
// because the streamed deltas already covered it.
func TestTranslateRawEvent_AssistantTextWithPartialActive(t *testing.T) {
	st := &translateState{
		toolCalls:     map[string]toolCallState{},
		partialActive: true,
	}
	raw := rawEvent{
		Type: "assistant",
		Message: &rawMessage{
			ID:      "msg-1",
			Content: []rawContentPart{{Type: "text", Text: "hi"}},
		},
	}

	got := translateRawEvent(raw, st)
	if len(got) != 1 || got[0].Kind != runtime.EventMessageEnd {
		t.Fatalf("expected only MessageEnd, got %#v", got)
	}
}

// TestTranslateRawEvent_AssistantToolUse_Builtin classifies a built-in tool name
// and emits PreToolUse + MessageEnd containing the ToolCall.
func TestTranslateRawEvent_AssistantToolUse_Builtin(t *testing.T) {
	st := &translateState{toolCalls: map[string]toolCallState{}}
	raw := rawEvent{
		Type:            "assistant",
		ParentToolUseID: "parent-7",
		Message: &rawMessage{
			ID: "msg-2",
			Content: []rawContentPart{{
				Type:  "tool_use",
				ID:    "tu-1",
				Name:  "Bash",
				Input: json.RawMessage(`{"cmd":"ls"}`),
			}},
		},
	}

	got := translateRawEvent(raw, st)
	if len(got) != 2 {
		t.Fatalf("expected 2 events, got %d: %#v", len(got), got)
	}
	pre := got[0]
	if pre.Kind != runtime.EventPreToolUse {
		t.Fatalf("first kind = %v, want PreToolUse", pre.Kind)
	}
	if pre.Tool == nil {
		t.Fatalf("Tool nil")
	}
	if pre.Tool.ID != "tu-1" || pre.Tool.Name != "Bash" {
		t.Errorf("tool id/name = %q/%q", pre.Tool.ID, pre.Tool.Name)
	}
	if string(pre.Tool.Input) != `{"cmd":"ls"}` {
		t.Errorf("Input = %s", pre.Tool.Input)
	}
	if pre.Tool.ParentID != "parent-7" {
		t.Errorf("ParentID = %q, want parent-7", pre.Tool.ParentID)
	}
	if pre.Tool.Source != runtime.ToolSourceBuiltin {
		t.Errorf("Source = %v, want Builtin", pre.Tool.Source)
	}
	if got[1].Kind != runtime.EventMessageEnd || len(got[1].Message.ToolCalls) != 1 {
		t.Errorf("expected MessageEnd with one ToolCall, got %#v", got[1])
	}
	// state must have stashed the tool_use for matching
	if state, ok := st.toolCalls["tu-1"]; !ok {
		t.Errorf("toolCalls missing entry")
	} else if state.Source != runtime.ToolSourceBuiltin {
		t.Errorf("stashed source = %v", state.Source)
	}
}

// TestTranslateRawEvent_AssistantToolUse_MCP classifies "mcp__*" as MCP tool.
func TestTranslateRawEvent_AssistantToolUse_MCP(t *testing.T) {
	st := &translateState{toolCalls: map[string]toolCallState{}}
	raw := rawEvent{
		Type: "assistant",
		Message: &rawMessage{
			Content: []rawContentPart{{
				Type:  "tool_use",
				ID:    "tu-2",
				Name:  "mcp__cago-bridge__add",
				Input: json.RawMessage(`{}`),
			}},
		},
	}
	got := translateRawEvent(raw, st)
	if len(got) < 1 || got[0].Tool == nil {
		t.Fatalf("expected tool event, got %#v", got)
	}
	if got[0].Tool.Source != runtime.ToolSourceMCP {
		t.Errorf("Source = %v, want MCP", got[0].Tool.Source)
	}
}

// TestTranslateRawEvent_UserToolResult pairs a previously-seen tool_use and emits
// PostToolUse + MessageEnd.
func TestTranslateRawEvent_UserToolResult(t *testing.T) {
	st := &translateState{
		toolCalls: map[string]toolCallState{
			"tu-3": {Name: "Bash", ParentID: "p-9", Source: runtime.ToolSourceBuiltin},
		},
	}
	raw := rawEvent{
		Type: "user",
		Message: &rawMessage{
			Content: []rawContentPart{{
				Type:      "tool_result",
				ToolUseID: "tu-3",
				Content:   "stdout",
			}},
		},
	}

	got := translateRawEvent(raw, st)
	if len(got) != 2 {
		t.Fatalf("expected 2 events, got %d: %#v", len(got), got)
	}
	post := got[0]
	if post.Kind != runtime.EventPostToolUse {
		t.Fatalf("first kind = %v", post.Kind)
	}
	if post.Tool == nil || post.Tool.ID != "tu-3" || post.Tool.Name != "Bash" {
		t.Errorf("tool fields = %#v", post.Tool)
	}
	if post.Tool.ParentID != "p-9" {
		t.Errorf("ParentID = %q", post.Tool.ParentID)
	}
	if post.Tool.Source != runtime.ToolSourceBuiltin {
		t.Errorf("Source = %v", post.Tool.Source)
	}
	if string(post.Tool.Response) != "stdout" {
		t.Errorf("Response = %s", post.Tool.Response)
	}
	if got[1].Kind != runtime.EventMessageEnd || got[1].Message.ToolResult == nil {
		t.Errorf("MessageEnd should carry ToolResult, got %#v", got[1])
	}
	if got[1].Message.ToolResult.ToolCallID != "tu-3" {
		t.Errorf("ToolResult.ToolCallID = %q", got[1].Message.ToolResult.ToolCallID)
	}
	// state should have evicted the tool_use
	if _, ok := st.toolCalls["tu-3"]; ok {
		t.Errorf("toolCalls should evict matched tool_use")
	}
}

// TestTranslateRawEvent_UserToolResult_Error sets Err and serializes the
// error into the runtime.ToolResult.
func TestTranslateRawEvent_UserToolResult_Error(t *testing.T) {
	st := &translateState{
		toolCalls: map[string]toolCallState{
			"tu-err": {Name: "Bash"},
		},
	}
	raw := rawEvent{
		Type: "user",
		Message: &rawMessage{
			Content: []rawContentPart{{
				Type:      "tool_result",
				ToolUseID: "tu-err",
				Content:   "boom",
				IsError:   true,
			}},
		},
	}
	got := translateRawEvent(raw, st)
	if len(got) != 2 {
		t.Fatalf("expected 2 events, got %d", len(got))
	}
	if got[0].Tool.Err == nil {
		t.Errorf("expected Tool.Err to be set on error result")
	}
	if got[1].Message.ToolResult == nil || got[1].Message.ToolResult.Err == "" {
		t.Errorf("expected ToolResult.Err non-empty")
	}
}

// TestTranslateRawEvent_ResultSuccess emits EventStop + EventDone with
// StopEndTurn.
func TestTranslateRawEvent_ResultSuccess(t *testing.T) {
	raw := rawEvent{
		Type:    "result",
		Subtype: "success",
		Usage:   &rawUsage{InputTokens: 10, OutputTokens: 20},
	}
	got := translateRawEvent(raw, &translateState{})
	if len(got) != 2 {
		t.Fatalf("expected 2 events, got %d", len(got))
	}
	if got[0].Kind != runtime.EventStop || got[0].Stop != runtime.StopEndTurn {
		t.Errorf("got[0] = %#v", got[0])
	}
	if got[1].Kind != runtime.EventDone || got[1].Stop != runtime.StopEndTurn {
		t.Errorf("got[1] = %#v", got[1])
	}
	if got[0].Usage.PromptTokens != 10 || got[0].Usage.CompletionTokens != 20 {
		t.Errorf("usage not propagated: %+v", got[0].Usage)
	}
}

// TestTranslateRawEvent_ResultMaxTurns yields StopMaxSteps.
func TestTranslateRawEvent_ResultMaxTurns(t *testing.T) {
	got := translateRawEvent(rawEvent{Type: "result", Subtype: "error_max_turns"}, &translateState{})
	if len(got) != 2 || got[0].Stop != runtime.StopMaxSteps || got[1].Stop != runtime.StopMaxSteps {
		t.Fatalf("unexpected events: %#v", got)
	}
}

// TestTranslateRawEvent_ResultErrorDuringExecution yields EventError + StopError Done.
func TestTranslateRawEvent_ResultErrorDuringExecution(t *testing.T) {
	got := translateRawEvent(rawEvent{Type: "result", Subtype: "error_during_execution", Error: "fail"}, &translateState{})
	if len(got) != 2 {
		t.Fatalf("expected 2 events, got %d", len(got))
	}
	if got[0].Kind != runtime.EventError {
		t.Errorf("kind = %v", got[0].Kind)
	}
	if got[0].Err == nil || !strings.Contains(got[0].Err.Error(), "fail") {
		t.Errorf("Err missing fail substring: %v", got[0].Err)
	}
	if got[1].Kind != runtime.EventDone || got[1].Stop != runtime.StopError {
		t.Errorf("done event = %#v", got[1])
	}
}

// TestClassifyToolSource enumerates the three classification branches.
func TestClassifyToolSource(t *testing.T) {
	tests := []struct {
		name string
		want runtime.ToolSource
	}{
		{"Bash", runtime.ToolSourceBuiltin},
		{"mcp__server__tool", runtime.ToolSourceMCP},
		{"", runtime.ToolSourceUnknown},
	}
	for _, tc := range tests {
		if got := classifyToolSource(tc.name); got != tc.want {
			t.Errorf("classifyToolSource(%q) = %v, want %v", tc.name, got, tc.want)
		}
	}
}

// TestTranslateStream_EndToEnd feeds a tiny stream and asserts the resulting
// event sequence over the channel.
func TestTranslateStream_EndToEnd(t *testing.T) {
	lines := []string{
		`{"type":"system","subtype":"init","session_id":"s-1","cwd":"/tmp"}`,
		`{"type":"assistant","message":{"id":"m1","content":[{"type":"text","text":"hi"}]}}`,
		`{"type":"result","subtype":"success","usage":{"input_tokens":1,"output_tokens":2}}`,
	}
	r := strings.NewReader(strings.Join(lines, "\n") + "\n")
	out := make(chan runtime.Event, 16)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		errCh <- translateStream(ctx, r, out, nil)
		close(out)
	}()

	var got []runtime.EventKind
	for ev := range out {
		got = append(got, ev.Kind)
	}
	if err := <-errCh; err != nil {
		t.Fatalf("translateStream err = %v", err)
	}
	want := []runtime.EventKind{
		runtime.EventSessionStart,
		runtime.EventTextDelta,
		runtime.EventMessageEnd,
		runtime.EventStop,
		runtime.EventDone,
	}
	if len(got) != len(want) {
		t.Fatalf("kinds = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("kind[%d] = %v, want %v", i, got[i], want[i])
		}
	}
}

// TestTranslateStream_UnexpectedEOF when no result frame seen.
func TestTranslateStream_UnexpectedEOF(t *testing.T) {
	r := strings.NewReader(`{"type":"system","subtype":"init","session_id":"s"}` + "\n")
	out := make(chan runtime.Event, 4)
	go func() {
		for range out {
		}
	}()
	err := translateStream(context.Background(), r, out, nil)
	close(out)
	if !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("err = %v, want ErrUnexpectedEOF", err)
	}
}

// TestParseLine_Errors covers malformed JSON / missing type field.
func TestParseLine_Errors(t *testing.T) {
	if _, err := parseLine(`{"type":"system"`); err == nil {
		t.Errorf("expected parse error for invalid json")
	}
	if _, err := parseLine(`{"foo":"bar"}`); err == nil {
		t.Errorf("expected parse error for missing type")
	}
	if _, err := parseLine(`{"type":"system","subtype":"init"}`); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestTranslateRawEvent_ControlRequestCanUseTool(t *testing.T) {
	st := &translateState{toolCalls: map[string]toolCallState{}}
	raw := rawEvent{
		Type:      "control_request",
		Subtype:   "can_use_tool",
		ID:        "perm-1",
		ToolName:  "Bash",
		ToolInput: json.RawMessage(`{"command":"rm -rf /"}`),
	}
	got := translateRawEvent(raw, st)
	if len(got) != 1 {
		t.Fatalf("expected 1 event, got %d", len(got))
	}
	ev := got[0]
	if ev.Kind != runtime.EventPermissionRequest {
		t.Fatalf("expected EventPermissionRequest, got %s", ev.Kind)
	}
	if ev.PermissionRequestID != "perm-1" {
		t.Fatalf("expected PermissionRequestID=perm-1, got %s", ev.PermissionRequestID)
	}
	if ev.Tool == nil {
		t.Fatal("expected Tool to be set")
	}
	if ev.Tool.Name != "Bash" {
		t.Fatalf("expected tool name Bash, got %s", ev.Tool.Name)
	}
	if string(ev.Tool.Input) != `{"command":"rm -rf /"}` {
		t.Fatalf("unexpected tool input: %s", ev.Tool.Input)
	}
}

func TestTranslateRawEvent_ControlRequestUnknownSubtype(t *testing.T) {
	st := &translateState{toolCalls: map[string]toolCallState{}}
	raw := rawEvent{
		Type:    "control_request",
		Subtype: "unknown",
		ID:      "perm-2",
	}
	got := translateRawEvent(raw, st)
	if len(got) != 0 {
		t.Fatalf("expected 0 events for unknown subtype, got %d", len(got))
	}
}

func TestTranslateRawEvent_ControlRequestEmptyID(t *testing.T) {
	st := &translateState{toolCalls: map[string]toolCallState{}}
	raw := rawEvent{
		Type:    "control_request",
		Subtype: "can_use_tool",
		ID:      "",
	}
	got := translateRawEvent(raw, st)
	if len(got) != 0 {
		t.Fatalf("expected 0 events for empty ID, got %d", len(got))
	}
}
