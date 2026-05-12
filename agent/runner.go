package agent

import (
	"context"
	"iter"
	"sync"

	"github.com/cago-frame/agents/provider"
)

type Runner struct {
	agent        *Agent
	conv         *Conversation
	mu           sync.Mutex
	closed       bool
	dispatcher   *dispatcher
	partialIndex int
	cancelFn     context.CancelFunc
	cancelReason string
	// steerOpen tracks whether the active turn will still consume queued
	// Steer messages. Distinct from cancelFn != nil because cancelFn is
	// cleared in defer (after final yields), creating a window where a
	// turn is "winding down" but Steer would still accept. steerOpen is
	// set false the moment the turn decides to exit, so any subsequent
	// Steer cleanly returns ErrSteerNoActiveTurn.
	steerOpen  bool
	steerMu    sync.Mutex
	steerQueue []steerEntry
}

// SteerOption tunes Steer behavior; sealed.
type SteerOption func(*steerConfig)

type steerConfig struct {
	id          string
	displayText string
}

// WithSteerID attaches a caller-owned identifier to a queued Steer message.
// When the steer is consumed, EventSteerConsumed carries the same ID in
// Event.SteerID so UIs can reconcile their pending queue with runner state.
func WithSteerID(id string) SteerOption {
	return func(c *steerConfig) { c.id = id }
}

// WithSteerDisplay attaches a DisplayTextBlock to the steer'd user
// message. Mirror of WithSendDisplay for steer-time injection: UI sees
// the display form, the LLM never does (DisplayTextBlock's
// Audience excludes blocks.ToLLM, so BuildRequest strips it).
//
// If multiple WithSteerDisplay calls are supplied, the last one wins
// (steerConfig.displayText is a scalar; this differs from WithSendDisplay,
// which accumulates blocks).
func WithSteerDisplay(displayText string) SteerOption {
	return func(c *steerConfig) { c.displayText = displayText }
}

type steerEntry struct {
	id          string
	text        string
	displayText string
}

func (r *Runner) Conversation() *Conversation { return r.conv }

func (r *Runner) Close() error {
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return nil
	}
	r.closed = true
	r.mu.Unlock()
	r.conv.releaseRunner()
	return nil
}

func (r *Runner) isClosed() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.closed
}

func (r *Runner) OnEvent(filter EventFilter, fn func(context.Context, Event)) Unsubscribe {
	if r.isClosed() {
		return func() {}
	}
	return r.dispatcher.addRunnerObserver(filter, fn)
}

// Send returns an iterator over the events emitted during this turn.
// On setup failure (e.g. ErrRunnerClosed, option validation) returns nil iter and a non-nil error.
// On success the iter is non-nil and emits at least EventDone.
func (r *Runner) Send(ctx context.Context, text string, opts ...SendOption) (iter.Seq[Event], error) {
	if r.isClosed() {
		return nil, ErrRunnerClosed
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	content, err := buildUserContent(text, opts)
	if err != nil {
		return nil, err
	}

	// Run UserPromptSubmit hooks. They cannot prevent setup-success;
	// a denied hook completes setup but the iter immediately emits
	// EventTurnEnd(StopHook) + EventDone.
	finalText := text
	var denied bool
	var denyReason string
	var hookErrors []error
	for _, h := range r.agent.cfg.userPromptHooks {
		out, hookErr := h(ctx, &UserPromptInput{Text: finalText, Conv: r.conv})
		if hookErr != nil {
			// Hook error -> treat as deny + surface as observable EventError.
			denied = true
			denyReason = hookErr.Error()
			hookErrors = append(hookErrors, &HookError{Stage: HookStageUserPromptSubmit, Cause: hookErr})
			break
		}
		if out == nil {
			continue
		}
		if out.ModifiedText != "" {
			finalText = out.ModifiedText
		}
		if out.Decision == DecisionDeny {
			denied = true
			denyReason = out.DenyReason
			break
		}
	}
	if denied {
		return r.deniedTurn(ctx, denyReason, hookErrors), nil
	}
	if finalText != text {
		// Rebuild content with modified text (extra blocks preserved).
		content, _ = buildUserContent(finalText, opts)
	}
	r.conv.Append(Message{Role: RoleUser, Content: content})
	return r.runTurn(ctx, content), nil
}

func (r *Runner) deniedTurn(ctx context.Context, reason string, prelude []error) iter.Seq[Event] {
	return func(yield func(Event) bool) {
		turnID := newTurnID()
		// Surface any hook errors that caused the denial so callers can
		// observe them via EventError before the turn-end frame.
		for _, e := range prelude {
			ev := Event{Kind: EventError, TurnID: turnID, Error: e}
			r.dispatcher.emit(ctx, ev)
			if !yield(ev) {
				return
			}
		}
		extras := r.runTurnEndHooks(ctx, StopHook, nil)
		teEv := Event{Kind: EventTurnEnd, TurnID: turnID, StopReason: StopHook, PartialReason: reason}
		r.dispatcher.emit(ctx, teEv)
		if !yield(teEv) {
			return
		}
		for _, ev := range extras {
			ev.TurnID = turnID
			r.dispatcher.emit(ctx, ev)
			if !yield(ev) {
				return
			}
		}
		doneEv := Event{Kind: EventDone, TurnID: turnID}
		r.dispatcher.emit(ctx, doneEv)
		yield(doneEv)
	}
}

func (r *Runner) Wait(ctx context.Context, text string, opts ...SendOption) error {
	events, err := r.Send(ctx, text, opts...)
	if err != nil {
		return err
	}
	for range events {
	}
	return nil
}

func (r *Runner) openPartial() {
	// Note: Content is left nil so blocks (text / thinking / tool_use) are
	// appended in stream order. Seeding an empty TextBlock here would force
	// thinking to land after text, which violates Anthropic's expected
	// "thinking before content" ordering.
	r.conv.Append(Message{Role: RoleAssistant, PartialReason: PartialStreaming})
	r.mu.Lock()
	r.partialIndex = r.conv.Len() - 1
	r.mu.Unlock()
}

func (r *Runner) appendPartialText(delta string) {
	r.mu.Lock()
	idx := r.partialIndex
	r.mu.Unlock()
	if idx < 0 {
		return
	}
	r.conv.appendDeltaToPartial(idx, delta)
}

func (r *Runner) appendPartialThinking(delta, signature string) {
	r.mu.Lock()
	idx := r.partialIndex
	r.mu.Unlock()
	if idx < 0 {
		return
	}
	r.conv.appendThinkingDeltaToPartial(idx, delta, signature)
}

func (r *Runner) upsertPartialToolUse(tu ToolUseBlock) {
	r.mu.Lock()
	idx := r.partialIndex
	r.mu.Unlock()
	if idx < 0 {
		return
	}
	r.conv.upsertPartialToolUse(idx, tu)
}

func (r *Runner) finalizePartial(stop StopReason, usage *provider.Usage) {
	r.mu.Lock()
	idx := r.partialIndex
	r.partialIndex = -1
	r.mu.Unlock()
	if idx < 0 {
		return
	}
	reason := PartialNone
	if stop == StopTokenLimit {
		// FinishLength: response is intact but truncated. Mark the message
		// so UIs can render a "continue" affordance and so a subsequent turn
		// keeps it in the LLM prompt by default.
		reason = PartialTokenLimit
	}
	r.conv.finalizePartial(idx, reason, "", usage)
}

func (r *Runner) errorPartial(ctx context.Context, turnID string, cause error, usage *provider.Usage, yield func(Event) bool) {
	r.mu.Lock()
	idx := r.partialIndex
	r.partialIndex = -1
	r.mu.Unlock()
	if idx >= 0 {
		detail := ""
		if cause != nil {
			detail = cause.Error()
		}
		r.conv.finalizePartial(idx, PartialErrored, detail, usage)
	}
	r.closeSteerWindow()
	ev := Event{Kind: EventError, TurnID: turnID, Error: cause}
	r.dispatcher.emit(ctx, ev)
	cont := yield(ev)
	extras := r.runTurnEndHooks(ctx, StopError, usage)
	teEv := Event{Kind: EventTurnEnd, TurnID: turnID, StopReason: StopError, Usage: usage}
	r.dispatcher.emit(ctx, teEv)
	if cont {
		if !yield(teEv) {
			return
		}
		for _, extra := range extras {
			extra.TurnID = turnID
			r.dispatcher.emit(ctx, extra)
			if !yield(extra) {
				return
			}
		}
	}
}

// runTurnEndHooks dispatches all TurnEnd hooks in registration order and
// returns the union of any events they wish to emit (after EventTurnEnd).
// Hook errors are surfaced as EventError frames in the returned slice so
// they reach the iter consumer instead of being silently swallowed.
func (r *Runner) runTurnEndHooks(ctx context.Context, stop StopReason, usage *Usage) []Event {
	in := &TurnEndInput{Conv: r.conv, StopReason: stop, Usage: usage}
	var emitted []Event
	for _, h := range r.agent.cfg.turnEndHooks {
		out, err := h(ctx, in)
		if err != nil {
			emitted = append(emitted, Event{
				Kind:  EventError,
				Error: &HookError{Stage: HookStageTurnEnd, Cause: err},
			})
			continue
		}
		if out != nil && len(out.EmittedEvents) > 0 {
			emitted = append(emitted, out.EmittedEvents...)
		}
	}
	return emitted
}

func (r *Runner) appendPartialToolUses(uses []ToolUseBlock) {
	r.mu.Lock()
	idx := r.partialIndex
	r.mu.Unlock()
	if idx < 0 {
		return
	}
	r.conv.appendToolUsesToPartial(idx, uses)
}

func asToolHookEntries[T any](src []hookEntry[T]) []ToolHookEntry[T] {
	out := make([]ToolHookEntry[T], len(src))
	for i, e := range src {
		out[i] = ToolHookEntry[T]{Matcher: e.matcher, Fn: e.fn}
	}
	return out
}

// Steer enqueues a user message to be injected at the next safe point in the
// current turn (after tool dispatch, or after a no-tool LLM completion via
// auto-continuation, before the next LLM call). Returns ErrSteerNoActiveTurn
// if the turn has already decided to exit — at that point use Send to start
// a new turn instead.
//
// SteerOption can attach a display-only block via WithSteerDisplay; the
// block remains in the conversation for UI projection (Audience = ToUI) but
// BuildRequest strips it before the LLM sees it.
func (r *Runner) Steer(ctx context.Context, text string, opts ...SteerOption) error {
	cfg := &steerConfig{}
	for _, o := range opts {
		o(cfg)
	}
	if cfg.id == "" {
		cfg.id = newConvID()
	}
	r.mu.Lock()
	if !r.steerOpen {
		r.mu.Unlock()
		return ErrSteerNoActiveTurn
	}
	r.mu.Unlock()
	r.steerMu.Lock()
	r.steerQueue = append(r.steerQueue, steerEntry{id: cfg.id, text: text, displayText: cfg.displayText})
	r.steerMu.Unlock()
	return nil
}

// RemovePendingSteer removes queued Steer messages matching id before they
// are consumed at a safe point. It returns false when id is empty or no pending
// entry matched; already-consumed steers cannot be removed.
func (r *Runner) RemovePendingSteer(id string) bool {
	if id == "" {
		return false
	}
	r.steerMu.Lock()
	defer r.steerMu.Unlock()
	if len(r.steerQueue) == 0 {
		return false
	}
	next := r.steerQueue[:0]
	removed := false
	for _, e := range r.steerQueue {
		if e.id == id {
			removed = true
			continue
		}
		next = append(next, e)
	}
	for i := len(next); i < len(r.steerQueue); i++ {
		r.steerQueue[i] = steerEntry{}
	}
	r.steerQueue = next
	return removed
}

func (r *Runner) drainSteer() []steerEntry {
	r.steerMu.Lock()
	defer r.steerMu.Unlock()
	if len(r.steerQueue) == 0 {
		return nil
	}
	out := r.steerQueue
	r.steerQueue = nil
	return out
}

// ClearPendingSteers drops all queued Steer messages that have not yet been
// consumed and returns their IDs in queue order.
func (r *Runner) ClearPendingSteers() []string {
	r.steerMu.Lock()
	defer r.steerMu.Unlock()
	if len(r.steerQueue) == 0 {
		return nil
	}
	ids := make([]string, 0, len(r.steerQueue))
	for _, e := range r.steerQueue {
		if e.id != "" {
			ids = append(ids, e.id)
		}
	}
	clear(r.steerQueue)
	r.steerQueue = nil
	return ids
}

// consumeSteerQueue drains the steer queue, appends each entry as a user
// message, and emits one EventSteerConsumed per entry. Returns false iff
// yield returned false (consumer abandoned the iter) so callers can unwind.
func (r *Runner) consumeSteerQueue(ctx context.Context, turnID string, yield func(Event) bool) bool {
	return r.appendAndEmitSteers(ctx, turnID, r.drainSteer(), yield)
}

// appendAndEmitSteers commits an already-drained batch (used at the no-tool
// auto-continue path that needs to inspect len > 0 before deciding to loop).
// Mirrors consumeSteerQueue's emit-after-append ordering.
func (r *Runner) appendAndEmitSteers(ctx context.Context, turnID string, pending []steerEntry, yield func(Event) bool) bool {
	for _, e := range pending {
		content := []ContentBlock{TextBlock{Text: e.text}}
		if e.displayText != "" {
			content = append(content, DisplayTextBlock{Text: e.displayText})
		}
		r.conv.Append(Message{Role: RoleUser, Content: content})

		// Prefer displayText for UIs (e.g. raw @mention form); fall back to
		// the LLM-facing text so the event always carries something.
		shown := e.displayText
		if shown == "" {
			shown = e.text
		}
		ev := Event{Kind: EventSteerConsumed, TurnID: turnID, Delta: shown, SteerID: e.id}
		r.dispatcher.emit(ctx, ev)
		if !yield(ev) {
			return false
		}
	}
	return true
}

// dropSteerQueue discards any queued steers without processing them. Called
// at exit paths where there's no LLM call coming (cancel / error / max-steps
// / stop hook); leaving them queued would leak into a subsequent Send.
func (r *Runner) dropSteerQueue() {
	r.steerMu.Lock()
	r.steerQueue = nil
	r.steerMu.Unlock()
}

// closeSteerWindow shuts the Steer window and drops any unprocessed queue.
// Used at every non-cancel/non-error exit path before emitting EventTurnEnd
// so concurrent Steer calls fail cleanly and stale messages can't bleed into
// the next Send.
func (r *Runner) closeSteerWindow() {
	r.mu.Lock()
	r.steerOpen = false
	r.mu.Unlock()
	r.dropSteerQueue()
}

// Cancel cancels the active turn with the given reason.
// The partial message will be finalized (with PartialCancelled) before the
// iter closes. Callers that need to observe the finalized partial should
// wait for the iter to drain before reading the conversation.
// Returns ErrSteerNoActiveTurn if no turn is currently active.
func (r *Runner) Cancel(reason string) error {
	r.mu.Lock()
	if r.cancelFn == nil {
		r.mu.Unlock()
		return ErrSteerNoActiveTurn
	}
	r.cancelReason = reason
	cf := r.cancelFn
	r.mu.Unlock()
	cf()
	return nil
}

// Resend regenerates from the existing conversation history without appending
// a new user message. The last message in the conversation must be a user message.
// On success, returns an iterator over the events emitted during the regeneration turn.
// Returns ErrCannotResend if the conversation is empty or the last message is not a user message.
// Returns ErrRunnerClosed if the runner has been closed.
func (r *Runner) Resend(ctx context.Context) (iter.Seq[Event], error) {
	if r.isClosed() {
		return nil, ErrRunnerClosed
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if r.conv.Len() == 0 {
		return nil, ErrCannotResend
	}
	last, err := r.conv.MessageAt(r.conv.Len() - 1)
	if err != nil {
		return nil, err
	}
	if last.Role != RoleUser {
		return nil, ErrCannotResend
	}
	return r.runTurn(ctx, last.Content), nil
}
