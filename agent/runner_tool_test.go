package agent_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	agent "github.com/cago-frame/agents/agent"
	"github.com/cago-frame/agents/provider"
	"github.com/cago-frame/agents/provider/providertest"
)

type echoTool struct{}

func (echoTool) Name() string         { return "Echo" }
func (echoTool) Description() string  { return "echo input" }
func (echoTool) Schema() agent.Schema { return agent.Schema{Type: "object"} }
func (echoTool) Call(ctx context.Context, input map[string]any) (*agent.ToolResultBlock, error) {
	txt, _ := input["text"].(string)
	return &agent.ToolResultBlock{Content: []agent.ContentBlock{agent.TextBlock{Text: "echoed: " + txt}}}, nil
}

func TestRunner_ToolCallRoundTrip(t *testing.T) {
	args, _ := json.Marshal(map[string]any{"text": "hi"})
	prov := providertest.New().
		QueueStream(
			provider.StreamChunk{ToolCallDelta: &provider.ToolCallDelta{
				Index:     0,
				ID:        "tu_1",
				Name:      "Echo",
				ArgsDelta: string(args),
			}},
			provider.StreamChunk{FinishReason: provider.FinishToolCalls},
		).
		QueueStream(
			provider.StreamChunk{ContentDelta: "done"},
			provider.StreamChunk{FinishReason: provider.FinishStop},
		)

	a := agent.New(prov, agent.Tools(echoTool{}))
	conv := agent.NewConversation()
	r := a.Runner(conv)
	defer func() { _ = r.Close() }()

	var sawPre, sawPost bool
	events, err := r.Send(context.Background(), "say echo")
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	for ev := range events {
		switch ev.Kind {
		case agent.EventPreToolUse:
			sawPre = true
			if ev.Tool.Name != "Echo" {
				t.Fatalf("pre tool name = %q", ev.Tool.Name)
			}
		case agent.EventPostToolUse:
			sawPost = true
		}
	}
	if !sawPre || !sawPost {
		t.Fatalf("expected Pre & Post tool events, got pre=%v post=%v", sawPre, sawPost)
	}

	// Conv must contain: user, assistant(toolUse), tool(result), assistant(text)
	if conv.Len() != 4 {
		t.Fatalf("conv len = %d, want 4", conv.Len())
	}
}

func TestRunner_PreToolUse_Deny(t *testing.T) {
	args, _ := json.Marshal(map[string]any{"text": "bad"})
	prov := providertest.New().
		QueueStream(
			provider.StreamChunk{ToolCallDelta: &provider.ToolCallDelta{
				Index: 0, ID: "tu_1", Name: "Echo", ArgsDelta: string(args),
			}},
			provider.StreamChunk{FinishReason: provider.FinishToolCalls},
		).
		QueueStream(
			provider.StreamChunk{ContentDelta: "skipping"},
			provider.StreamChunk{FinishReason: provider.FinishStop},
		)

	a := agent.New(prov,
		agent.Tools(echoTool{}),
		agent.Use("Echo", func(c *agent.ToolContext) {
			c.AbortWithDeny("policy")
		}),
	)
	conv := agent.NewConversation()
	r := a.Runner(conv)
	defer func() { _ = r.Close() }()

	events, _ := r.Send(context.Background(), "go")
	for range events {
	}
	last := conv.Messages()[2] // user, assistant(toolUse), tool(result with deny)
	if last.Role != agent.RoleTool {
		t.Fatalf("third message should be tool result, got %v", last.Role)
	}
	tr, ok := last.Content[0].(agent.ToolResultBlock)
	if !ok || !tr.IsError {
		t.Fatalf("tool result should be IsError, got %#v", last.Content[0])
	}
}

// errTool returns an error from Call so the dispatcher's error-result path is exercised.
type errTool struct{}

func (errTool) Name() string         { return "ErrTool" }
func (errTool) Description() string  { return "tool that errors" }
func (errTool) Schema() agent.Schema { return agent.Schema{Type: "object"} }
func (errTool) Call(ctx context.Context, input map[string]any) (*agent.ToolResultBlock, error) {
	return nil, errors.New("tool failed")
}

// TestRunner_ToolCallError_ResultIsError verifies the dispatcher synthesizes a
// ToolResultBlock with IsError=true when Tool.Call returns an error.
func TestRunner_ToolCallError_ResultIsError(t *testing.T) {
	args, _ := json.Marshal(map[string]any{})
	prov := providertest.New().
		QueueStream(
			provider.StreamChunk{ToolCallDelta: &provider.ToolCallDelta{
				Index:     0,
				ID:        "tu_1",
				Name:      "ErrTool",
				ArgsDelta: string(args),
			}},
			provider.StreamChunk{FinishReason: provider.FinishToolCalls},
		).
		QueueStream(
			provider.StreamChunk{FinishReason: provider.FinishStop},
		)

	a := agent.New(prov, agent.Tools(errTool{}))
	conv := agent.NewConversation()
	r := a.Runner(conv)
	defer func() { _ = r.Close() }()

	events, _ := r.Send(context.Background(), "go")
	for range events {
	}

	if conv.Len() < 3 {
		t.Fatalf("conv len too short: %d", conv.Len())
	}
	tr, ok := conv.Messages()[2].Content[0].(agent.ToolResultBlock)
	if !ok {
		t.Fatalf("conv[2].Content[0] should be ToolResultBlock, got %T", conv.Messages()[2].Content[0])
	}
	if !tr.IsError {
		t.Fatalf("ToolResultBlock.IsError should be true after tool error")
	}
}

func TestRunner_ParallelToolDispatch_StableOrdering(t *testing.T) {
	t.Parallel()
	args1, _ := json.Marshal(map[string]any{"text": "first"})
	args2, _ := json.Marshal(map[string]any{"text": "second"})
	prov := providertest.New().
		QueueStream(
			provider.StreamChunk{ToolCallDelta: &provider.ToolCallDelta{
				Index: 0, ID: "tu_1", Name: "Echo", ArgsDelta: string(args1),
			}},
			provider.StreamChunk{ToolCallDelta: &provider.ToolCallDelta{
				Index: 1, ID: "tu_2", Name: "Echo", ArgsDelta: string(args2),
			}},
			provider.StreamChunk{FinishReason: provider.FinishToolCalls},
		).
		QueueStream(
			provider.StreamChunk{ContentDelta: "all done"},
			provider.StreamChunk{FinishReason: provider.FinishStop},
		)

	a := agent.New(prov, agent.Tools(echoTool{}))
	conv := agent.NewConversation()
	r := a.Runner(conv)
	defer func() { _ = r.Close() }()

	events, err := r.Send(context.Background(), "two echoes")
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	var preIDs, postIDs []string
	for ev := range events {
		switch ev.Kind {
		case agent.EventPreToolUse:
			preIDs = append(preIDs, ev.Tool.ToolUseID)
		case agent.EventPostToolUse:
			postIDs = append(postIDs, ev.Tool.ToolUseID)
		}
	}
	want := []string{"tu_1", "tu_2"}
	if !equalStrings(preIDs, want) {
		t.Fatalf("Pre order = %v, want %v", preIDs, want)
	}
	if !equalStrings(postIDs, want) {
		t.Fatalf("Post order = %v, want %v", postIDs, want)
	}
}

// TestRunner_OrphanToolUse_OnEarlyExitBeforeDispatch_GetsSyntheticResult
// pins the API invariant that every committed tool_use must be paired with a
// tool_result in the conversation, even when the consumer abandons the iter
// before the dispatcher runs. Otherwise the next turn replays an orphan
// tool_use to the LLM and providers reject the request (Anthropic 400
// "tool_result does not follow tool_use").
func TestRunner_OrphanToolUse_OnEarlyExitBeforeDispatch_GetsSyntheticResult(t *testing.T) {
	args, _ := json.Marshal(map[string]any{"text": "hi"})
	prov := providertest.New().
		QueueStream(
			provider.StreamChunk{ToolCallDelta: &provider.ToolCallDelta{
				Index: 0, ID: "tu_orphan", Name: "Echo", ArgsDelta: string(args),
			}},
			provider.StreamChunk{FinishReason: provider.FinishToolCalls},
		)

	a := agent.New(prov, agent.Tools(echoTool{}))
	conv := agent.NewConversation()
	r := a.Runner(conv)
	defer func() { _ = r.Close() }()

	events, err := r.Send(context.Background(), "go")
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	for ev := range events {
		if ev.Kind == agent.EventPreToolUse {
			break // consumer gives up before dispatch — yield returns false next call
		}
	}

	msgs := conv.Messages()
	// user, assistant(tool_use), tool(synthetic tool_result)
	if len(msgs) < 3 {
		t.Fatalf("expected synthetic tool_result appended, got %d msgs: %#v", len(msgs), msgs)
	}
	last := msgs[len(msgs)-1]
	if last.Role != agent.RoleTool {
		t.Fatalf("last msg role = %q, want tool", last.Role)
	}
	tr, ok := last.Content[0].(agent.ToolResultBlock)
	if !ok {
		t.Fatalf("last content type = %T, want ToolResultBlock", last.Content[0])
	}
	if tr.ToolUseID != "tu_orphan" {
		t.Fatalf("synthetic tool_result.ToolUseID = %q, want tu_orphan", tr.ToolUseID)
	}
	if !tr.IsError {
		t.Fatalf("synthetic tool_result must have IsError=true")
	}
}

// TestRunner_OrphanToolUse_PartialDispatch_PadsRemaining covers the case where
// some tool_results were appended before yield returned false. The defer must
// only pad the missing ones (not duplicate already-appended results).
func TestRunner_OrphanToolUse_PartialDispatch_PadsRemaining(t *testing.T) {
	args1, _ := json.Marshal(map[string]any{"text": "first"})
	args2, _ := json.Marshal(map[string]any{"text": "second"})
	prov := providertest.New().
		QueueStream(
			provider.StreamChunk{ToolCallDelta: &provider.ToolCallDelta{
				Index: 0, ID: "tu_a", Name: "Echo", ArgsDelta: string(args1),
			}},
			provider.StreamChunk{ToolCallDelta: &provider.ToolCallDelta{
				Index: 1, ID: "tu_b", Name: "Echo", ArgsDelta: string(args2),
			}},
			provider.StreamChunk{FinishReason: provider.FinishToolCalls},
		)

	a := agent.New(prov, agent.Tools(echoTool{}))
	conv := agent.NewConversation()
	r := a.Runner(conv)
	defer func() { _ = r.Close() }()

	events, err := r.Send(context.Background(), "go")
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	postCount := 0
	for ev := range events {
		if ev.Kind == agent.EventPostToolUse {
			postCount++
			if postCount == 1 {
				break // bail after the first PostToolUse (before second result is appended)
			}
		}
	}

	msgs := conv.Messages()
	// Collect all tool_result IDs from the conv tail.
	gotIDs := map[string]bool{}
	for _, m := range msgs {
		if m.Role != agent.RoleTool {
			continue
		}
		for _, b := range m.Content {
			if tr, ok := b.(agent.ToolResultBlock); ok {
				gotIDs[tr.ToolUseID] = true
			}
		}
	}
	for _, id := range []string{"tu_a", "tu_b"} {
		if !gotIDs[id] {
			t.Fatalf("missing tool_result for %q in conv: gotIDs=%v msgs=%d", id, gotIDs, len(msgs))
		}
	}
}

// TestRunner_ToolCallDelta_WithFinishStop_StillDispatches pins behavior for
// providers that emit tool_use blocks while reporting stop_reason=end_turn
// (e.g. DeepSeek's anthropic-compat endpoint). The runner must treat tool_use
// presence — not finish_reason — as the dispatch signal. Otherwise the
// committed tool_use would only be paired with a synthetic "tool dispatch
// aborted" result, which (a) wastes a turn and (b) leaks an orphan tool_use
// down the steer-continue path.
func TestRunner_ToolCallDelta_WithFinishStop_StillDispatches(t *testing.T) {
	args, _ := json.Marshal(map[string]any{"text": "hi"})
	prov := providertest.New().
		QueueStream(
			provider.StreamChunk{ToolCallDelta: &provider.ToolCallDelta{
				Index:     0,
				ID:        "tu_deepseek",
				Name:      "Echo",
				ArgsDelta: string(args),
			}},
			// FinishStop instead of FinishToolCalls — emulates DeepSeek.
			provider.StreamChunk{FinishReason: provider.FinishStop},
		).
		QueueStream(
			provider.StreamChunk{ContentDelta: "done"},
			provider.StreamChunk{FinishReason: provider.FinishStop},
		)

	a := agent.New(prov, agent.Tools(echoTool{}))
	conv := agent.NewConversation()
	r := a.Runner(conv)
	defer func() { _ = r.Close() }()

	var sawPost bool
	events, err := r.Send(context.Background(), "say echo")
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	for ev := range events {
		if ev.Kind == agent.EventPostToolUse {
			sawPost = true
		}
	}
	if !sawPost {
		t.Fatalf("expected EventPostToolUse — tool should be dispatched even with FinishStop")
	}

	msgs := conv.Messages()
	if len(msgs) != 4 {
		t.Fatalf("conv len = %d, want 4 (user, assistant(tool_use), tool(result), assistant(text)); msgs=%#v", len(msgs), msgs)
	}
	tr, ok := msgs[2].Content[0].(agent.ToolResultBlock)
	if !ok {
		t.Fatalf("msgs[2].Content[0] type = %T, want ToolResultBlock", msgs[2].Content[0])
	}
	if tr.IsError {
		t.Fatalf("tool_result is IsError=true — runner fell through to orphan-guard synthetic abort path instead of real dispatch")
	}
	if tr.ToolUseID != "tu_deepseek" {
		t.Fatalf("tool_result.ToolUseID = %q, want tu_deepseek", tr.ToolUseID)
	}
	gotTxt, _ := tr.Content[0].(agent.TextBlock)
	if gotTxt.Text != "echoed: hi" {
		t.Fatalf("tool_result content = %q, want %q", gotTxt.Text, "echoed: hi")
	}
}

// TestRunner_ToolCallDelta_WithFinishStop_BuildRequestPairs confirms that a
// follow-up Send sees a properly paired tool_use → tool_result sequence in the
// LLM request after a FinishStop-with-tool_use turn. Regression guard for the
// reported DeepSeek 400 "tool_use ids were found without tool_result blocks".
func TestRunner_ToolCallDelta_WithFinishStop_BuildRequestPairs(t *testing.T) {
	args, _ := json.Marshal(map[string]any{"text": "hi"})
	prov := providertest.New().
		QueueStream(
			provider.StreamChunk{ToolCallDelta: &provider.ToolCallDelta{
				Index: 0, ID: "tu_pair", Name: "Echo", ArgsDelta: string(args),
			}},
			provider.StreamChunk{FinishReason: provider.FinishStop},
		).
		QueueStream(
			provider.StreamChunk{ContentDelta: "ok"},
			provider.StreamChunk{FinishReason: provider.FinishStop},
		).
		QueueStream(
			provider.StreamChunk{ContentDelta: "second"},
			provider.StreamChunk{FinishReason: provider.FinishStop},
		)

	a := agent.New(prov, agent.Tools(echoTool{}))
	conv := agent.NewConversation()
	r := a.Runner(conv)
	defer func() { _ = r.Close() }()

	if err := r.Wait(context.Background(), "first"); err != nil {
		t.Fatalf("first send: %v", err)
	}
	if err := r.Wait(context.Background(), "second"); err != nil {
		t.Fatalf("second send: %v", err)
	}

	// Inspect the request produced by the SECOND user turn — that's the one
	// that previously failed because BuildRequest replayed an orphan tool_use.
	req := agent.BuildRequest(agent.RequestSpec{Messages: conv.Messages()})
	for i, m := range req.Messages {
		if len(m.ToolCalls) == 0 {
			continue
		}
		// Every assistant message with ToolCalls must be followed by tool
		// messages whose ToolCallID matches each tool_call.
		want := map[string]bool{}
		for _, tc := range m.ToolCalls {
			want[tc.ID] = false
		}
		for j := i + 1; j < len(req.Messages); j++ {
			next := req.Messages[j]
			if next.Role != provider.RoleTool {
				break
			}
			if next.ToolCallID == "" {
				continue
			}
			if _, ok := want[next.ToolCallID]; ok {
				want[next.ToolCallID] = true
			}
		}
		for id, paired := range want {
			if !paired {
				t.Fatalf("orphan tool_use: req.Messages[%d] has ToolCall ID %q without matching tool_result", i, id)
			}
		}
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
