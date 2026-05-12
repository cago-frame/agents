package agent_test

import (
	"context"
	"encoding/json"
	"errors"
	"iter"
	"sync/atomic"
	"testing"
	"time"

	agent "github.com/cago-frame/agents/agent"
	"github.com/cago-frame/agents/provider"
	"github.com/cago-frame/agents/provider/providertest"
)

// slowEchoTool sleeps a configurable amount before returning. We use it to
// detect parallelism — two slow calls run in parallel finish in ~one sleep,
// not two.
type slowEchoTool struct {
	name  string
	sleep time.Duration
	count *atomic.Int32
}

func (t *slowEchoTool) Name() string         { return t.name }
func (t *slowEchoTool) Description() string  { return "slow echo for parallelism testing" }
func (t *slowEchoTool) Schema() agent.Schema { return agent.Schema{Type: "object"} }
func (t *slowEchoTool) Call(ctx context.Context, input map[string]any) (*agent.ToolResultBlock, error) {
	t.count.Add(1)
	select {
	case <-time.After(t.sleep):
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	return &agent.ToolResultBlock{Content: []agent.ContentBlock{agent.TextBlock{Text: "ok-" + t.name}}}, nil
}

func TestToolDispatcher_RunBatch_RunsInParallel(t *testing.T) {
	t.Parallel()
	var counter atomic.Int32
	a := &slowEchoTool{name: "A", sleep: 80 * time.Millisecond, count: &counter}
	b := &slowEchoTool{name: "B", sleep: 80 * time.Millisecond, count: &counter}

	td := &agent.ToolDispatcher{Tools: []agent.Tool{a, b}}
	calls := []agent.DispatchInput{
		{ToolName: "A", ToolUseID: "u1", Input: map[string]any{}},
		{ToolName: "B", ToolUseID: "u2", Input: map[string]any{}},
	}

	start := time.Now()
	results := td.RunBatch(context.Background(), calls)
	elapsed := time.Since(start)

	if len(results) != 2 {
		t.Fatalf("want 2 results, got %d", len(results))
	}
	if elapsed > 140*time.Millisecond {
		t.Fatalf("RunBatch ran serially (elapsed=%v) — must be parallel by default", elapsed)
	}
	if counter.Load() != 2 {
		t.Fatalf("both tools must have been called, got count=%d", counter.Load())
	}
	if results[0].Output == nil || results[1].Output == nil {
		t.Fatal("results have nil Output")
	}
}

// serialEchoTool implements SerialTool with Serial()==true.
type serialEchoTool struct {
	name  string
	sleep time.Duration
	count *atomic.Int32
}

func (t *serialEchoTool) Name() string         { return t.name }
func (t *serialEchoTool) Description() string  { return "serial echo" }
func (t *serialEchoTool) Schema() agent.Schema { return agent.Schema{Type: "object"} }
func (t *serialEchoTool) Serial() bool         { return true }
func (t *serialEchoTool) Call(ctx context.Context, input map[string]any) (*agent.ToolResultBlock, error) {
	t.count.Add(1)
	select {
	case <-time.After(t.sleep):
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	return &agent.ToolResultBlock{Content: []agent.ContentBlock{agent.TextBlock{Text: "ok-" + t.name}}}, nil
}

func TestToolDispatcher_RunBatch_SerialToolForcesSequential(t *testing.T) {
	t.Parallel()
	var counter atomic.Int32
	a := &slowEchoTool{name: "A", sleep: 80 * time.Millisecond, count: &counter}
	b := &serialEchoTool{name: "B", sleep: 80 * time.Millisecond, count: &counter}

	td := &agent.ToolDispatcher{Tools: []agent.Tool{a, b}}
	calls := []agent.DispatchInput{
		{ToolName: "A", ToolUseID: "u1", Input: map[string]any{}},
		{ToolName: "B", ToolUseID: "u2", Input: map[string]any{}},
	}

	start := time.Now()
	results := td.RunBatch(context.Background(), calls)
	elapsed := time.Since(start)

	if len(results) != 2 {
		t.Fatalf("want 2 results, got %d", len(results))
	}
	// At least one tool serial → entire batch sequential. ~160ms minimum.
	if elapsed < 150*time.Millisecond {
		t.Fatalf("RunBatch ran in parallel (elapsed=%v) but a SerialTool was in the batch", elapsed)
	}
}

// streamingEchoTool implements StreamingTool. It emits N text deltas, then a
// final ToolResultBlock.
type streamingEchoTool struct {
	name  string
	parts []string
}

func (t *streamingEchoTool) Name() string         { return t.name }
func (t *streamingEchoTool) Description() string  { return "streaming echo" }
func (t *streamingEchoTool) Schema() agent.Schema { return agent.Schema{Type: "object"} }
func (t *streamingEchoTool) Call(ctx context.Context, input map[string]any) (*agent.ToolResultBlock, error) {
	full := ""
	for _, p := range t.parts {
		full += p
	}
	return &agent.ToolResultBlock{Content: []agent.ContentBlock{agent.TextBlock{Text: full}}}, nil
}
func (t *streamingEchoTool) CallStream(ctx context.Context, input map[string]any) iter.Seq[agent.ToolDelta] {
	return func(yield func(agent.ToolDelta) bool) {
		full := ""
		for _, p := range t.parts {
			if !yield(agent.ToolDelta{Text: p}) {
				return
			}
			full += p
		}
		_ = yield(agent.ToolDelta{Final: &agent.ToolResultBlock{Content: []agent.ContentBlock{agent.TextBlock{Text: full}}}})
	}
}

func TestRunner_StreamingTool_EmitsDeltas(t *testing.T) {
	t.Parallel()
	args, _ := json.Marshal(map[string]any{})
	prov := providertest.New().
		QueueStream(
			provider.StreamChunk{ToolCallDelta: &provider.ToolCallDelta{
				Index: 0, ID: "tu_1", Name: "StreamEcho", ArgsDelta: string(args),
			}},
			provider.StreamChunk{FinishReason: provider.FinishToolCalls},
		).
		QueueStream(
			provider.StreamChunk{ContentDelta: "done"},
			provider.StreamChunk{FinishReason: provider.FinishStop},
		)

	tool := &streamingEchoTool{name: "StreamEcho", parts: []string{"hello ", "world", "!"}}
	a := agent.New(prov, agent.Tools(tool))
	conv := agent.NewConversation()
	r := a.Runner(conv)
	defer func() { _ = r.Close() }()

	events, _ := r.Send(context.Background(), "stream me")
	var deltas []string
	var sawPost bool
	// EventToolDelta now also fires for args-streaming during ChatStream;
	// only collect deltas that arrive AFTER EventPreToolUse (i.e. tool
	// dispatch deltas).
	preFired := false
	for ev := range events {
		switch ev.Kind {
		case agent.EventPreToolUse:
			preFired = true
		case agent.EventToolDelta:
			if ev.Tool.ToolUseID != "tu_1" {
				t.Fatalf("delta tool_use_id = %q, want tu_1", ev.Tool.ToolUseID)
			}
			if preFired {
				deltas = append(deltas, ev.Delta)
			}
		case agent.EventPostToolUse:
			sawPost = true
		}
	}
	want := []string{"hello ", "world", "!"}
	if len(deltas) != len(want) {
		t.Fatalf("delta count = %d, want %d (deltas=%v)", len(deltas), len(want), deltas)
	}
	for i := range want {
		if deltas[i] != want[i] {
			t.Fatalf("deltas[%d] = %q, want %q", i, deltas[i], want[i])
		}
	}
	if !sawPost {
		t.Fatal("EventPostToolUse not seen after streaming")
	}
}

// idCapturingTool records the ToolUseID it observed via
// agent.ToolUseIDFromContext during Call.
type idCapturingTool struct {
	name string
	got  *string
}

func (t *idCapturingTool) Name() string         { return t.name }
func (t *idCapturingTool) Description() string  { return "captures tool-use id from ctx" }
func (t *idCapturingTool) Schema() agent.Schema { return agent.Schema{Type: "object"} }
func (t *idCapturingTool) Call(ctx context.Context, _ map[string]any) (*agent.ToolResultBlock, error) {
	*t.got = agent.ToolUseIDFromContext(ctx)
	return &agent.ToolResultBlock{Content: []agent.ContentBlock{agent.TextBlock{Text: "ok"}}}, nil
}

func TestToolDispatcher_InjectsToolUseIDIntoContext(t *testing.T) {
	t.Parallel()
	var toolGot, preGot, postGot string
	tool := &idCapturingTool{name: "cap", got: &toolGot}

	td := &agent.ToolDispatcher{
		Tools: []agent.Tool{tool},
		Middleware: []agent.ToolHookEntry[agent.ToolMiddleware]{
			{Matcher: ".*", Fn: func(c *agent.ToolContext) {
				preGot = agent.ToolUseIDFromContext(c.Context())
				c.Next()
				postGot = agent.ToolUseIDFromContext(c.Context())
			}},
		},
	}
	td.Run(context.Background(), agent.DispatchInput{
		ToolName: "cap", ToolUseID: "tu_42", Input: map[string]any{},
	})

	if preGot != "tu_42" {
		t.Fatalf("PreHook got %q, want %q", preGot, "tu_42")
	}
	if toolGot != "tu_42" {
		t.Fatalf("Tool.Call got %q, want %q", toolGot, "tu_42")
	}
	if postGot != "tu_42" {
		t.Fatalf("PostHook got %q, want %q", postGot, "tu_42")
	}
}

func TestToolUseIDFromContext_EmptyWhenAbsent(t *testing.T) {
	t.Parallel()
	if got := agent.ToolUseIDFromContext(context.Background()); got != "" {
		t.Fatalf("ToolUseIDFromContext on bare ctx = %q, want empty", got)
	}
}

// errEmittingTool returns the configured (output, err) from Call. Used to
// exercise post-terminal AbortWithError classification.
type errEmittingTool struct {
	name string
	out  *agent.ToolResultBlock
	err  error
}

func (t *errEmittingTool) Name() string         { return t.name }
func (t *errEmittingTool) Description() string  { return "emits configured result/error" }
func (t *errEmittingTool) Schema() agent.Schema { return agent.Schema{Type: "object"} }
func (t *errEmittingTool) Call(_ context.Context, _ map[string]any) (*agent.ToolResultBlock, error) {
	return t.out, t.err
}

// TestToolDispatcher_AbortWithError_PreVsPostStage verifies HookError stage
// classification: pre-terminal aborts are HookStagePreToolUse, post-Next
// aborts are HookStagePostToolUse. Earlier code mis-classified post-Next
// aborts as Pre because chain index did not advance after terminal returned.
func TestToolDispatcher_AbortWithError_PreVsPostStage(t *testing.T) {
	t.Parallel()
	wantErr := errors.New("boom")

	t.Run("pre-terminal abort -> Pre", func(t *testing.T) {
		td := &agent.ToolDispatcher{
			Tools: []agent.Tool{&errEmittingTool{name: "cap"}},
			Middleware: []agent.ToolHookEntry[agent.ToolMiddleware]{
				{Matcher: ".*", Fn: func(c *agent.ToolContext) {
					c.AbortWithError(wantErr) // before c.Next()
				}},
			},
		}
		res := td.Run(context.Background(), agent.DispatchInput{ToolName: "cap", ToolUseID: "tu_pre"})
		if len(res.HookErrors) != 1 {
			t.Fatalf("want 1 HookError, got %d", len(res.HookErrors))
		}
		var he *agent.HookError
		if !errors.As(res.HookErrors[0], &he) {
			t.Fatalf("HookErrors[0] not *HookError: %T", res.HookErrors[0])
		}
		if he.Stage != agent.HookStagePreToolUse {
			t.Fatalf("stage = %q, want %q", he.Stage, agent.HookStagePreToolUse)
		}
		if res.Stop {
			t.Fatal("pre-terminal abort should not signal Stop")
		}
	})

	t.Run("post-Next abort -> Post + Stop", func(t *testing.T) {
		td := &agent.ToolDispatcher{
			Tools: []agent.Tool{&errEmittingTool{name: "cap", out: &agent.ToolResultBlock{
				Content: []agent.ContentBlock{agent.TextBlock{Text: "ran"}},
			}}},
			Middleware: []agent.ToolHookEntry[agent.ToolMiddleware]{
				{Matcher: ".*", Fn: func(c *agent.ToolContext) {
					c.Next()
					c.AbortWithError(wantErr) // after terminal returned
				}},
			},
		}
		res := td.Run(context.Background(), agent.DispatchInput{ToolName: "cap", ToolUseID: "tu_post"})
		if len(res.HookErrors) != 1 {
			t.Fatalf("want 1 HookError, got %d", len(res.HookErrors))
		}
		var he *agent.HookError
		if !errors.As(res.HookErrors[0], &he) {
			t.Fatalf("HookErrors[0] not *HookError: %T", res.HookErrors[0])
		}
		if he.Stage != agent.HookStagePostToolUse {
			t.Fatalf("stage = %q, want %q", he.Stage, agent.HookStagePostToolUse)
		}
		if !res.Stop {
			t.Fatal("post-terminal abort should signal Stop")
		}
		// The tool's actual output must be preserved (not overwritten).
		if res.Output == nil || len(res.Output.Content) == 0 {
			t.Fatal("post-terminal abort should keep tool's actual Output")
		}
		if tb, ok := res.Output.Content[0].(agent.TextBlock); !ok || tb.Text != "ran" {
			t.Fatalf("tool output not preserved, got %+v", res.Output.Content)
		}
	})
}

// TestToolDispatcher_NeverNext_DoesNotPanic verifies the dispatcher returns a
// non-nil Output even when a middleware silently stops the chain (no Next, no
// AbortWith*, no manual Output). The runner unconditionally dereferences
// *res.Output, so a nil here would panic.
func TestToolDispatcher_NeverNext_DoesNotPanic(t *testing.T) {
	t.Parallel()
	td := &agent.ToolDispatcher{
		Tools: []agent.Tool{&errEmittingTool{name: "cap"}},
		Middleware: []agent.ToolHookEntry[agent.ToolMiddleware]{
			{Matcher: ".*", Fn: func(c *agent.ToolContext) {
				// no Next, no Abort, no Output write
			}},
		},
	}
	res := td.Run(context.Background(), agent.DispatchInput{ToolName: "cap", ToolUseID: "tu_silent"})
	if res.Output == nil {
		t.Fatal("dispatcher must never return nil Output")
	}
	if res.Output.ToolUseID != "tu_silent" {
		t.Fatalf("ToolUseID = %q, want %q", res.Output.ToolUseID, "tu_silent")
	}
}
