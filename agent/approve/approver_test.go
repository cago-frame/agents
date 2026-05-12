package approve_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	agent "github.com/cago-frame/agents/agent"
	"github.com/cago-frame/agents/agent/approve"
)

// runHookCtx drives the approve middleware to completion using a test
// ToolContext + a no-op terminal. Returns true iff the chain reached the
// terminal handler (i.e. the request was approved). Useful for tests that
// only care about approve-vs-not, not the deny reason.
func runHookCtx(ctx context.Context, toolName, toolUseID string, input map[string]any, hook agent.ToolMiddleware) bool {
	executed := false
	tc := agent.NewToolContextForTest(ctx, toolName, toolUseID, input)
	terminal := func(c *agent.ToolContext) { executed = true }
	tc.WithChain(hook, terminal)
	tc.Next()
	return executed
}

func TestApprover_ApproveResolves(t *testing.T) {
	t.Parallel()
	a := approve.New()
	defer func() { _ = a.Close() }()

	hook := approve.Hook(a)

	go func() {
		for p := range a.Pending() {
			if err := a.Approve(p.ID); err != nil {
				t.Errorf("Approve: %v", err)
				return
			}
		}
	}()

	if !runHookCtx(context.Background(), "Bash", "tu_1", map[string]any{"cmd": "ls"}, hook) {
		t.Fatal("approval should have allowed the chain through to the terminal handler")
	}
}

func TestApprover_DenyResolves(t *testing.T) {
	t.Parallel()
	a := approve.New()
	defer func() { _ = a.Close() }()

	hook := approve.Hook(a)
	go func() {
		for p := range a.Pending() {
			_ = a.Deny(p.ID, "policy")
		}
	}()

	// Build chain manually so we can observe the deny via dispatcher output.
	td := &agent.ToolDispatcher{
		Tools:      []agent.Tool{stubTool{name: "Bash"}},
		Middleware: []agent.ToolHookEntry[agent.ToolMiddleware]{{Matcher: "Bash", Fn: hook}},
	}
	res := td.Run(context.Background(), agent.DispatchInput{
		ToolName: "Bash", ToolUseID: "tu_2", Input: map[string]any{"cmd": "rm -rf /"},
	})
	if res.Output == nil || !res.Output.IsError {
		t.Fatalf("expected deny error result, got %+v", res.Output)
	}
	if !containsTextBlock(res.Output, "denied: policy") {
		t.Fatalf("expected deny reason 'policy' in output, got %+v", res.Output)
	}
}

func TestApprover_CloseDeniesPending(t *testing.T) {
	t.Parallel()
	a := approve.New()
	hook := approve.Hook(a)

	td := &agent.ToolDispatcher{
		Tools:      []agent.Tool{stubTool{name: "X"}},
		Middleware: []agent.ToolHookEntry[agent.ToolMiddleware]{{Matcher: ".*", Fn: hook}},
	}

	type runRes struct {
		out *agent.ToolResultBlock
	}
	resCh := make(chan runRes, 1)
	go func() {
		dr := td.Run(context.Background(), agent.DispatchInput{
			ToolName: "X", ToolUseID: "tu_3",
		})
		resCh <- runRes{dr.Output}
	}()

	time.Sleep(20 * time.Millisecond)
	if err := a.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	select {
	case r := <-resCh:
		if r.out == nil || !r.out.IsError {
			t.Fatalf("expected deny error result after Close, got %+v", r.out)
		}
		if !containsTextBlock(r.out, "denied:") {
			t.Fatalf("expected deny prefix after Close, got %+v", r.out)
		}
	case <-time.After(time.Second):
		t.Fatal("hook did not return within 1s after Close")
	}
}

func TestApprover_ApproveUnknownID(t *testing.T) {
	t.Parallel()
	a := approve.New()
	defer func() { _ = a.Close() }()
	if err := a.Approve("does-not-exist"); !errors.Is(err, approve.ErrUnknownID) {
		t.Fatalf("Approve unknown id: err = %v, want ErrUnknownID", err)
	}
}

func TestApprover_PendingMultipleConsumers(t *testing.T) {
	t.Parallel()
	a := approve.New()
	defer func() { _ = a.Close() }()
	hook := approve.Hook(a)

	var wg sync.WaitGroup
	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			if !runHookCtx(context.Background(), "X", "tu_"+itoa(idx), nil, hook) {
				t.Errorf("hook %d: chain not approved through", idx)
			}
		}(i)
	}

	go func() {
		for p := range a.Pending() {
			_ = a.Approve(p.ID)
		}
	}()

	wg.Wait()
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	return string(rune('0' + i))
}

// stubTool is a no-op tool used to satisfy ToolDispatcher.Tools when the test
// only cares about the middleware chain's deny/allow behavior.
type stubTool struct{ name string }

func (s stubTool) Name() string         { return s.name }
func (s stubTool) Description() string  { return "stub" }
func (s stubTool) Schema() agent.Schema { return agent.Schema{Type: "object"} }
func (s stubTool) Call(_ context.Context, _ map[string]any) (*agent.ToolResultBlock, error) {
	return &agent.ToolResultBlock{Content: []agent.ContentBlock{agent.TextBlock{Text: "executed"}}}, nil
}

func containsTextBlock(b *agent.ToolResultBlock, sub string) bool {
	if b == nil {
		return false
	}
	for _, c := range b.Content {
		if t, ok := c.(agent.TextBlock); ok && containsStr(t.Text, sub) {
			return true
		}
	}
	return false
}

func containsStr(s, sub string) bool {
	if len(sub) == 0 {
		return true
	}
	if len(s) < len(sub) {
		return false
	}
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
