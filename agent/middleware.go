package agent

import (
	"context"
	"sync"
)

// ToolMiddleware is a gin-style around handler for tool dispatch. Middleware
// receives a *ToolContext, optionally calls c.Next() to invoke the rest of the
// chain (which terminates in the actual Tool.Call), and may then inspect or
// rewrite c.Output. AbortWithDeny / AbortWithError short-circuits the chain
// and halts dispatch.
type ToolMiddleware func(c *ToolContext)

// ToolContext is the per-dispatch state object passed through the middleware
// chain. Each tool dispatch (one per ToolUseID) gets its own *ToolContext;
// fields are not safe to share across dispatches.
//
// Concurrency: ToolDispatcher.RunBatch may run multiple non-serial tools in
// parallel goroutines, each with its own *ToolContext. The Set/Get/Append
// methods are guarded by an internal mutex so a middleware that spins up its
// own helper goroutines can still write back safely; however, the chain itself
// runs single-threaded per dispatch.
type ToolContext struct {
	ctx       context.Context
	ToolName  string
	ToolUseID string

	// Input is the (possibly modified) tool argument map. Middleware MAY
	// mutate this in place or replace the map; downstream handlers and the
	// terminal tool see the latest value.
	Input map[string]any

	// Output is populated by the terminal handler with the tool's
	// *ToolResultBlock. Post-Next middleware may inspect or replace it.
	Output *ToolResultBlock

	// Conv is the read-only conversation snapshot, equivalent to the field
	// of the same name on the old PreToolUseInput / PostToolUseInput.
	Conv ConversationReader

	// Stop, when set true by middleware, signals the runner to halt the
	// turn after this dispatch (analog of the old
	// PostToolUseOutput.StopRun).
	Stop bool

	handlers    []ToolMiddleware
	index       int
	aborted     bool
	denyMsg     string
	denied      bool // true if AbortWithDeny was called (vs AbortWithError or plain Abort)
	err         error
	terminalRan bool // set true by the terminal handler after Tool.Call returns
	addedCtx    []string
	deltas      []string

	mu    sync.Mutex
	store map[any]any
}

// Context returns the per-dispatch context.Context. Callers reading state from
// ctx (e.g., values placed by middleware via WithContext) should always use
// this getter instead of caching across calls.
func (c *ToolContext) Context() context.Context { return c.ctx }

// WithContext re-binds the dispatch context. Subsequent c.Next() calls and the
// terminal Tool.Call see the new ctx — this is the load-bearing mechanic for
// middleware that needs to surface per-call state to a tool implementation
// (whose Tool.Call signature only takes context.Context, not *ToolContext).
func (c *ToolContext) WithContext(ctx context.Context) {
	if ctx == nil {
		return
	}
	c.ctx = ctx
}

// Next advances the chain to the next handler. If the chain is finished or
// the context has been aborted, Next is a no-op. Middleware that does NOT
// call Next acts as a terminating filter (gin semantics).
func (c *ToolContext) Next() {
	if c.aborted {
		return
	}
	c.index++
	if c.index < len(c.handlers) {
		c.handlers[c.index](c)
	}
}

// Abort halts the chain without setting a deny message. The dispatcher will
// surface c.Output as-is; useful when a middleware has already populated
// c.Output itself (e.g., synthesized a cached response).
func (c *ToolContext) Abort() { c.aborted = true }

// AbortWithDeny halts the chain and produces a deny ToolResultBlock with the
// given reason. The deny block is materialized into c.Output immediately so
// that any post-Next middleware (already on the call stack) can observe it on
// the way back out — this is critical for around-style middleware like an
// auditor that runs `c.Next(); inspect(c.Output)`.
//
// Output bytes are byte-identical to toolDenyResult(id, reason) so model-side
// prompting is unchanged.
func (c *ToolContext) AbortWithDeny(reason string) {
	c.aborted = true
	c.denied = true
	c.denyMsg = reason
	c.Output = &ToolResultBlock{
		ToolUseID: c.ToolUseID,
		IsError:   true,
		Content:   []ContentBlock{TextBlock{Text: "denied: " + reason}},
	}
}

// AbortWithError halts the chain and records err as a HookError. The dispatcher
// turns it into a *HookError surfaced via DispatchResult.HookErrors and the
// turn loop emits it as EventError. The HookStage is inferred from the chain
// position: pre-terminal → HookStagePreToolUse, post-terminal → HookStagePostToolUse.
//
// Pre-terminal aborts also materialize an error ToolResultBlock into c.Output
// so post-Next middleware sees it on the way back out (same rationale as
// AbortWithDeny). Post-terminal aborts leave c.Output as the tool's actual
// output (only StopRun signaling is added).
func (c *ToolContext) AbortWithError(err error) {
	c.aborted = true
	c.err = err
	if c.Output == nil {
		c.Output = &ToolResultBlock{
			ToolUseID: c.ToolUseID,
			IsError:   true,
			Content:   []ContentBlock{TextBlock{Text: err.Error()}},
		}
	}
}

// Set stores a value on the per-dispatch scratch map. The map is private to
// this *ToolContext, not shared across batched dispatches.
func (c *ToolContext) Set(key, value any) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.store == nil {
		c.store = make(map[any]any)
	}
	c.store[key] = value
}

// Get reads a value previously placed by Set. Returns (nil, false) if the key
// was never set.
func (c *ToolContext) Get(key any) (any, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	v, ok := c.store[key]
	return v, ok
}

// AppendContext accumulates extra context strings emitted by middleware. The
// dispatcher returns them via DispatchResult.AddedCtx, equivalent to the old
// AdditionalContext fields on PreToolUseOutput / PostToolUseOutput.
func (c *ToolContext) AppendContext(s string) {
	if s == "" {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.addedCtx = append(c.addedCtx, s)
}

// pushDelta records a streaming-tool text chunk; the dispatcher exposes them
// as DispatchResult.Deltas in input order. Used internally by the terminal
// handler when the resolved tool implements StreamingTool.
func (c *ToolContext) pushDelta(s string) {
	if s == "" {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.deltas = append(c.deltas, s)
}

// stageAtError returns the HookStage that should be reported for c.err.
// Pre-terminal aborts (terminal never ran) are HookStagePreToolUse;
// post-terminal aborts (a middleware fired AbortWithError after c.Next() returned)
// are HookStagePostToolUse.
//
// We track terminalRan explicitly rather than inferring from c.index — after
// the terminal returns, c.index stays at terminalIndex, so an index-based
// check would mis-classify post-Next aborts as Pre.
func (c *ToolContext) stageAtError() HookStage {
	if c.terminalRan {
		return HookStagePostToolUse
	}
	return HookStagePreToolUse
}

// NewToolContextForTest constructs a *ToolContext for use in tests that drive
// a middleware chain manually (without running a full Runner / dispatcher).
// Callers append handlers to the returned context's chain via WithChain and
// call Next() to start. Production code should never use this — it bypasses
// dispatcher safety.
func NewToolContextForTest(ctx context.Context, toolName, toolUseID string, input map[string]any) *ToolContext {
	if ctx == nil {
		ctx = context.Background()
	}
	return &ToolContext{
		ctx:       ctx,
		ToolName:  toolName,
		ToolUseID: toolUseID,
		Input:     input,
		index:     -1,
	}
}

// WithChain installs the given middleware list and resets the chain pointer.
// Test-only helper used together with NewToolContextForTest.
func (c *ToolContext) WithChain(mws ...ToolMiddleware) *ToolContext {
	c.handlers = mws
	c.index = -1
	c.aborted = false
	return c
}
