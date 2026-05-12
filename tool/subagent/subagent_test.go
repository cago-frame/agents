package subagent_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"

	agent "github.com/cago-frame/agents/agent"
	"github.com/cago-frame/agents/provider"
	"github.com/cago-frame/agents/provider/providertest"
	"github.com/cago-frame/agents/tool/subagent"
)

// newChildAgent returns a *agent.Agent backed by a fresh providertest.Mock.
func newChildAgent() *agent.Agent {
	return agent.New(providertest.New())
}

func TestNewToolPanicsOnEmptyEntries(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatalf("expected panic for empty entries")
		}
	}()
	subagent.NewTool("sub_agent", "desc", nil)
}

func TestNewToolPanicsOnEmptyType(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatalf("expected panic for empty type")
		}
	}()
	subagent.NewTool("sub_agent", "desc", []subagent.Entry{
		{Type: "", Description: "d", Agent: newChildAgent()},
	})
}

func TestNewToolPanicsOnNilAgent(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatalf("expected panic for nil Agent")
		}
	}()
	subagent.NewTool("sub_agent", "desc", []subagent.Entry{
		{Type: "x", Description: "d", Agent: nil},
	})
}

func TestNewToolPanicsOnDuplicateType(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatalf("expected panic for duplicate type")
		}
	}()
	subagent.NewTool("sub_agent", "desc", []subagent.Entry{
		{Type: "x", Description: "d1", Agent: newChildAgent()},
		{Type: "x", Description: "d2", Agent: newChildAgent()},
	})
}

func TestNewToolReturnsValidTool(t *testing.T) {
	tool := subagent.NewTool("sub_agent", "desc", []subagent.Entry{
		{Type: "explore", Description: "explore code", Agent: newChildAgent()},
	})
	if tool.Name() != "sub_agent" {
		t.Fatalf("unexpected name: %q", tool.Name())
	}
	if st, ok := tool.(agent.SerialTool); ok && st.Serial() {
		t.Fatalf("expected Serial=false by default")
	}
}

func TestWithSerialOption(t *testing.T) {
	tool := subagent.NewTool("sub_agent", "desc", []subagent.Entry{
		{Type: "explore", Description: "d", Agent: newChildAgent()},
	}, subagent.WithSerial())
	st, ok := tool.(agent.SerialTool)
	if !ok || !st.Serial() {
		t.Fatalf("expected Serial=true with WithSerial()")
	}
}

func TestSchemaAndDescription(t *testing.T) {
	a1 := newChildAgent()
	a2 := newChildAgent()
	tool := subagent.NewTool("sub_agent", "委派任务给子 agent", []subagent.Entry{
		{Type: "explore", Description: "在仓库里搜关键字", Agent: a1},
		{Type: "search", Description: "在互联网上检索资料", Agent: a2},
	})

	// Description should contain the base + each entry description.
	desc := tool.Description()
	for _, want := range []string{"委派任务给子 agent", "explore:", "search:", "在仓库里搜关键字", "在互联网上检索资料"} {
		if !strings.Contains(desc, want) {
			t.Errorf("description missing %q:\n%s", want, desc)
		}
	}

	// Schema fields: type=object, required=[type, prompt], properties has enum.
	schema := tool.Schema()
	if schema.Type != "object" {
		t.Errorf("schema.Type = %q, want object", schema.Type)
	}
	wantReq := map[string]bool{"type": true, "prompt": true}
	if len(schema.Required) != 2 {
		t.Errorf("required len = %d, want 2: %v", len(schema.Required), schema.Required)
	}
	for _, r := range schema.Required {
		if !wantReq[r] {
			t.Errorf("unexpected required field: %q", r)
		}
	}
	// Verify enum values are present in the properties JSON.
	propsJSON, _ := json.Marshal(schema.Properties)
	propsStr := string(propsJSON)
	for _, enumVal := range []string{"explore", "search"} {
		if !strings.Contains(propsStr, enumVal) {
			t.Errorf("schema properties missing enum value %q: %s", enumVal, propsStr)
		}
	}
}

// ─── helpers ──────────────────────────────────────────────────────────────────

// runAndGetAllToolResults runs the parent agent and returns a map of
// ToolUseID → *agent.ToolResultBlock collected from EventPostToolUse.
func runAndGetAllToolResults(t *testing.T, prov *providertest.Mock, dispatchTool agent.Tool) map[string]*agent.ToolResultBlock {
	t.Helper()
	conv := agent.NewConversation()
	a := agent.New(prov, agent.Tools(dispatchTool))
	runner := a.Runner(conv)
	defer func() { _ = runner.Close() }()

	var mu sync.Mutex
	results := map[string]*agent.ToolResultBlock{}
	unsub := runner.OnEvent(agent.OnlyKinds(agent.EventPostToolUse), func(_ context.Context, ev agent.Event) {
		if ev.Tool == nil {
			return
		}
		mu.Lock()
		results[ev.Tool.ToolUseID] = ev.Tool.Output
		mu.Unlock()
	})
	defer unsub()

	if err := runner.Wait(context.Background(), "go"); err != nil {
		t.Fatalf("runner.Wait err: %v", err)
	}
	return results
}

// textOf extracts the text content from a ToolResultBlock.
func textOf(rb *agent.ToolResultBlock) string {
	if rb == nil {
		return ""
	}
	var sb strings.Builder
	for _, c := range rb.Content {
		if t, ok := c.(agent.TextBlock); ok {
			sb.WriteString(t.Text)
		}
	}
	return sb.String()
}

// ─── happy-path test ──────────────────────────────────────────────────────────

// TestNewTool_HappyPath verifies the basic dispatch flow: parent calls
// dispatch tool → child runs → tool result contains child assistant text.
func TestNewTool_HappyPath(t *testing.T) {
	parentMock := providertest.New()
	parentMock.QueueStream(
		provider.StreamChunk{ToolCallDelta: &provider.ToolCallDelta{Index: 0, ID: "np1", Name: "sub_agent"}},
		provider.StreamChunk{ToolCallDelta: &provider.ToolCallDelta{Index: 0, ArgsDelta: `{"type":"explore","prompt":"find Y"}`}},
		provider.StreamChunk{FinishReason: provider.FinishToolCalls},
	)
	parentMock.QueueStream(
		provider.StreamChunk{ContentDelta: "done"},
		provider.StreamChunk{FinishReason: provider.FinishStop},
	)

	childMock := providertest.New()
	childMock.QueueStream(
		provider.StreamChunk{ContentDelta: "found Y"},
		provider.StreamChunk{FinishReason: provider.FinishStop},
	)

	child := agent.New(childMock)
	dispatchTool := subagent.NewTool("sub_agent", "dispatch", []subagent.Entry{
		{Type: "explore", Description: "explore", Agent: child},
	})

	results := runAndGetAllToolResults(t, parentMock, dispatchTool)
	rb := results["np1"]
	if rb == nil {
		t.Fatalf("expected a tool result for np1; got nil")
	}
	if rb.IsError {
		t.Errorf("unexpected error result: %s", textOf(rb))
	}
	got := textOf(rb)
	if got != "found Y" {
		t.Errorf("tool result text = %q, want %q", got, "found Y")
	}
}

// ─── unknown type ─────────────────────────────────────────────────────────────

func TestSubAgent_UnknownType(t *testing.T) {
	parentMock := providertest.New()
	parentMock.QueueStream(
		provider.StreamChunk{ToolCallDelta: &provider.ToolCallDelta{Index: 0, ID: "ut1", Name: "sub_agent"}},
		provider.StreamChunk{ToolCallDelta: &provider.ToolCallDelta{Index: 0, ArgsDelta: `{"type":"unknownX","prompt":"x"}`}},
		provider.StreamChunk{FinishReason: provider.FinishToolCalls},
	)
	parentMock.QueueStream(
		provider.StreamChunk{ContentDelta: "done"},
		provider.StreamChunk{FinishReason: provider.FinishStop},
	)

	dispatchTool := subagent.NewTool("sub_agent", "委派", []subagent.Entry{
		{Type: "explore", Description: "d", Agent: newChildAgent()},
	})

	results := runAndGetAllToolResults(t, parentMock, dispatchTool)
	rb := results["ut1"]
	if rb == nil {
		t.Fatalf("expected a tool result for ut1")
	}
	if !rb.IsError {
		t.Errorf("expected IsError=true for unknown type")
	}
	got := textOf(rb)
	if !strings.Contains(got, "unknown type:") || !strings.Contains(got, "unknownX") {
		t.Errorf("tool result = %q, want to contain 'unknown type:' and 'unknownX'", got)
	}
}

// ─── child provider error ─────────────────────────────────────────────────────

func TestSubAgent_ChildProviderError(t *testing.T) {
	parentMock := providertest.New()
	parentMock.QueueStream(
		provider.StreamChunk{ToolCallDelta: &provider.ToolCallDelta{Index: 0, ID: "cp1", Name: "sub_agent"}},
		provider.StreamChunk{ToolCallDelta: &provider.ToolCallDelta{Index: 0, ArgsDelta: `{"type":"explore","prompt":"x"}`}},
		provider.StreamChunk{FinishReason: provider.FinishToolCalls},
	)
	parentMock.QueueStream(
		provider.StreamChunk{ContentDelta: "done"},
		provider.StreamChunk{FinishReason: provider.FinishStop},
	)

	childMock := providertest.New()
	childMock.QueueStream(
		provider.StreamChunk{Err: errors.New("child boom")},
	)

	child := agent.New(childMock)
	dispatchTool := subagent.NewTool("sub_agent", "委派", []subagent.Entry{
		{Type: "explore", Description: "d", Agent: child},
	})

	// Child stream errors are surfaced as IsError=true tool results carrying the
	// original error message — the parent agent then sees a normal tool error
	// and can decide to retry / escalate / fail. Earlier behavior silently
	// returned "sub-agent returned no content", which dropped the cause and
	// indistinguishably mimicked a model that just produced nothing.
	results := runAndGetAllToolResults(t, parentMock, dispatchTool)
	rb, ok := results["cp1"]
	if !ok {
		t.Fatalf("expected a tool result for child-error case; got %v", results)
	}
	if !rb.IsError {
		t.Errorf("expected IsError=true on child stream error; got IsError=false (text=%q)", textOf(rb))
	}
	got := textOf(rb)
	if !strings.Contains(got, "sub-agent error") || !strings.Contains(got, "child boom") {
		t.Errorf("expected tool result text to include 'sub-agent error' and original cause 'child boom'; got %q", got)
	}
}

// ─── child max-steps ──────────────────────────────────────────────────────────

// TestSubAgent_ChildMaxSteps verifies that when the child agent emits
// StopMaxSteps, the result is prefixed with "[sub-agent stopped: max_steps]".
func TestSubAgent_ChildMaxSteps(t *testing.T) {
	parentMock := providertest.New()
	parentMock.QueueStream(
		provider.StreamChunk{ToolCallDelta: &provider.ToolCallDelta{Index: 0, ID: "ms1", Name: "sub_agent"}},
		provider.StreamChunk{ToolCallDelta: &provider.ToolCallDelta{Index: 0, ArgsDelta: `{"type":"explore","prompt":"x"}`}},
		provider.StreamChunk{FinishReason: provider.FinishToolCalls},
	)
	parentMock.QueueStream(
		provider.StreamChunk{ContentDelta: "done"},
		provider.StreamChunk{FinishReason: provider.FinishStop},
	)

	// Child: emit assistant text then request a tool call (forcing a second
	// LLM step that exceeds MaxSteps=1). With MaxSteps=1 the runner stops
	// after the first step, emitting StopMaxSteps.
	childMock := providertest.New()
	// First (and only allowed) step: child emits a tool call; MaxSteps=1 so
	// the runner stops with StopMaxSteps without issuing a second request.
	childMock.QueueStream(
		provider.StreamChunk{ContentDelta: "partial"},
		provider.StreamChunk{ToolCallDelta: &provider.ToolCallDelta{Index: 0, ID: "cu1", Name: "some_tool"}},
		provider.StreamChunk{FinishReason: provider.FinishToolCalls},
	)

	child := agent.New(childMock, agent.MaxSteps(1))
	dispatchTool := subagent.NewTool("sub_agent", "委派", []subagent.Entry{
		{Type: "explore", Description: "d", Agent: child},
	})

	results := runAndGetAllToolResults(t, parentMock, dispatchTool)
	rb := results["ms1"]
	if rb == nil {
		t.Fatalf("expected a tool result for ms1")
	}
	got := textOf(rb)
	if !strings.Contains(got, "[sub-agent stopped: max_steps]") {
		t.Errorf("expected max_steps prefix; got %q", got)
	}
}
