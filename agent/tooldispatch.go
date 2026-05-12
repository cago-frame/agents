package agent

import (
	"context"
	"regexp"
	"sync"
)

// Hook isolation contract:
//
// Middleware chains may run on different goroutines when RunBatch dispatches
// multiple non-serial tools. Middleware MUST be self-contained — it must not
// assume exclusive access to shared state. Tools that need exclusive access
// for their middleware chain must implement SerialTool with Serial()==true;
// the dispatcher then runs the entire batch sequentially.
//
// Per-dispatch state lives on *ToolContext (Set/Get scratch map, Output,
// AppendContext); each dispatched tool gets its own *ToolContext, so middleware
// closures can safely store call-scoped state without coordinating across
// goroutines.
//
// ToolDispatcher runs the middleware chain (which terminates in Tool.Call)
// for each dispatched tool.
type ToolDispatcher struct {
	Tools      []Tool
	Middleware []ToolHookEntry[ToolMiddleware]
}

type toolUseIDKey struct{}

// ToolUseIDFromContext returns the ToolUseID for the current tool dispatch.
// Returns "" outside a dispatched tool call. Middleware, the tool's Call,
// and any post-Next middleware within the same dispatch share the same value,
// allowing callers to keep per-call state in a sync.Map keyed by ToolUseID
// (though *ToolContext.Set/Get is preferred for new code).
func ToolUseIDFromContext(ctx context.Context) string {
	v, _ := ctx.Value(toolUseIDKey{}).(string)
	return v
}

// WithToolUseID returns a context carrying the given ToolUseID. Mostly internal;
// exposed for tests that exercise tools or hooks outside a Runner.
func WithToolUseID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, toolUseIDKey{}, id)
}

// ToolHookEntry pairs a matcher pattern with a hook/middleware function.
type ToolHookEntry[T any] struct {
	Matcher string
	Fn      T
}

// toolMatch returns true if the matcher (regex) matches the tool name.
// Empty matcher means match-all.
func toolMatch(matcher, name string) bool {
	if matcher == "" || matcher == "*" {
		return true
	}
	re, err := regexp.Compile(matcher)
	if err != nil {
		return false
	}
	return re.MatchString(name)
}

// DispatchInput carries the parameters for a single tool dispatch.
type DispatchInput struct {
	ToolName  string
	ToolUseID string
	Input     map[string]any
	Conv      ConversationReader
}

// DispatchResult carries the outcome of a single tool dispatch.
type DispatchResult struct {
	Output   *ToolResultBlock
	Stop     bool
	AddedCtx []string // accumulated AppendContext from all middleware
	// Deltas, when non-empty, are the text chunks yielded by a StreamingTool
	// during its CallStream iteration. The dispatcher does not emit them
	// itself — the loop emits one EventToolDelta per chunk after the batch
	// completes (in input order across the batch). For non-streaming tools
	// this is nil.
	Deltas []string
	// HookErrors, when non-empty, are *HookError values from middleware that
	// failed during this dispatch. The loop turns each into an EventError so
	// callers can observe hook failures instead of silently inheriting their
	// (deny / stop) semantics.
	HookErrors []error
}

// Run executes the matching middleware chain (terminating in Tool.Call) for
// a single dispatch.
func (d *ToolDispatcher) Run(ctx context.Context, in DispatchInput) DispatchResult {
	ctx = WithToolUseID(ctx, in.ToolUseID)

	// Build chain: matching middleware in registration order, then the
	// terminal handler that performs the actual Tool.Call.
	var chain []ToolMiddleware
	for _, mw := range d.Middleware {
		if toolMatch(mw.Matcher, in.ToolName) {
			chain = append(chain, mw.Fn)
		}
	}
	terminal := d.terminalHandler(in)
	chain = append(chain, terminal)

	tc := &ToolContext{
		ctx:       ctx,
		ToolName:  in.ToolName,
		ToolUseID: in.ToolUseID,
		Input:     in.Input,
		Conv:      in.Conv,
		handlers:  chain,
		index:     -1,
	}
	tc.Next()

	return tc.toDispatchResult()
}

// terminalHandler returns the chain-ending middleware that resolves the tool
// by name and invokes Call (or CallStream for streaming tools). The terminal
// handler does NOT call c.Next() — chain unwinds naturally. It sets
// c.terminalRan true so post-Next middleware errors get classified as
// HookStagePostToolUse.
func (d *ToolDispatcher) terminalHandler(in DispatchInput) ToolMiddleware {
	return func(c *ToolContext) {
		var tool Tool
		for _, t := range d.Tools {
			if t.Name() == in.ToolName {
				tool = t
				break
			}
		}
		if tool == nil {
			c.Output = toolErrorResult(in.ToolUseID, ErrToolNotFound)
			return
		}
		// Mark terminal as having run; from this point any AbortWithError
		// from post-Next middleware is classified as PostToolUse.
		defer func() { c.terminalRan = true }()

		if st, ok := tool.(StreamingTool); ok {
			seq := st.CallStream(c.ctx, c.Input)
			var output *ToolResultBlock
			for delta := range seq {
				if delta.Final != nil {
					output = delta.Final
					break
				}
				if delta.Text != "" {
					c.pushDelta(delta.Text)
				}
			}
			if output == nil {
				output = &ToolResultBlock{ToolUseID: in.ToolUseID}
			} else {
				output.ToolUseID = in.ToolUseID
			}
			c.Output = output
			return
		}

		out, err := tool.Call(c.ctx, c.Input)
		if err != nil {
			c.Output = toolErrorResult(in.ToolUseID, err)
			return
		}
		if out == nil {
			c.Output = &ToolResultBlock{ToolUseID: in.ToolUseID}
			return
		}
		out.ToolUseID = in.ToolUseID
		c.Output = out
	}
}

// toDispatchResult collapses the per-call ToolContext state into the public
// DispatchResult shape the loop consumes.
//
// Note: AbortWithDeny / AbortWithError already write c.Output during the
// chain run (so post-Next middleware can see it on the way back out). This
// method only attaches HookErrors and propagates Stop; it does not synthesize
// outputs except as a defensive fallback when a middleware aborted via plain
// Abort() (or simply returned without calling Next()) and never set c.Output —
// the runner unconditionally dereferences *res.Output, so we never return nil.
func (c *ToolContext) toDispatchResult() DispatchResult {
	res := DispatchResult{
		Output:   c.Output,
		Stop:     c.Stop,
		AddedCtx: c.addedCtx,
		Deltas:   c.deltas,
	}

	if c.err != nil {
		stage := c.stageAtError()
		res.HookErrors = append(res.HookErrors, &HookError{
			Stage: stage,
			Tool:  c.ToolName,
			Cause: c.err,
		})
		// Post-terminal error: tool already ran and produced its own output;
		// signal Stop so the runner halts the turn after this dispatch.
		if stage == HookStagePostToolUse {
			res.Stop = true
		}
	}

	if res.Output == nil {
		// Middleware silently stopped the chain (no Next, no AbortWith*, no
		// manual Output write). Return an empty block so the loop's
		// *res.Output dereference doesn't panic.
		res.Output = &ToolResultBlock{ToolUseID: c.ToolUseID}
	}
	return res
}

// RunBatch dispatches a slice of tool calls. Parallel by default; if any tool
// in the batch is registered as a SerialTool with Serial()==true, the entire
// batch falls back to sequential execution (in input order).
//
// Results are returned in the same order as `calls`, regardless of goroutine
// completion order.
func (d *ToolDispatcher) RunBatch(ctx context.Context, calls []DispatchInput) []DispatchResult {
	if len(calls) == 0 {
		return nil
	}
	if d.shouldRunSerial(calls) {
		out := make([]DispatchResult, len(calls))
		for i, c := range calls {
			out[i] = d.Run(ctx, c)
		}
		return out
	}
	out := make([]DispatchResult, len(calls))
	var wg sync.WaitGroup
	for i := range calls {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			out[idx] = d.Run(ctx, calls[idx])
		}(i)
	}
	wg.Wait()
	return out
}

// shouldRunSerial returns true if any tool referenced by the batch is a
// SerialTool whose Serial() returns true. Tools missing from d.Tools or not
// satisfying SerialTool are treated as non-serial.
func (d *ToolDispatcher) shouldRunSerial(calls []DispatchInput) bool {
	for _, c := range calls {
		for _, t := range d.Tools {
			if t.Name() != c.ToolName {
				continue
			}
			if st, ok := t.(SerialTool); ok && st.Serial() {
				return true
			}
			break
		}
	}
	return false
}

func toolErrorResult(toolUseID string, err error) *ToolResultBlock {
	return &ToolResultBlock{
		ToolUseID: toolUseID,
		IsError:   true,
		Content:   []ContentBlock{TextBlock{Text: err.Error()}},
	}
}
