package agent

import (
	"context"
	"encoding/json"
	"errors"
	"iter"

	"github.com/cago-frame/agents/provider"
)

type toolUseAccum struct {
	ID, Name string
	argBuf   string
}

func (r *Runner) runTurn(ctx context.Context, _ []ContentBlock) iter.Seq[Event] {
	ctx, cancel := context.WithCancel(ctx)
	r.mu.Lock()
	r.cancelFn = cancel
	r.steerOpen = true
	r.mu.Unlock()

	return func(yield func(Event) bool) {
		defer func() {
			r.mu.Lock()
			r.cancelFn = nil
			r.steerOpen = false
			r.mu.Unlock()
			cancel()
		}()

		// Orphan-tool_use guard. After tool_uses are committed to the conv
		// (line ~200) the runner owes a tool_result for each one — every
		// provider rejects an assistant message whose tool_use blocks aren't
		// followed by matching tool_results. If yield returns false between
		// commit and result-append (consumer abandoned the iter), this defer
		// pads the missing entries with synthetic results so the next turn's
		// request stays valid. Real results are reused when dispatch already
		// ran; otherwise a synthetic IsError block stands in.
		var pendingToolUses []ToolUseBlock
		var pendingResults []DispatchResult
		var appendedCount int
		defer func() {
			for i := appendedCount; i < len(pendingToolUses); i++ {
				tu := pendingToolUses[i]
				var result *ToolResultBlock
				if i < len(pendingResults) && pendingResults[i].Output != nil {
					result = pendingResults[i].Output
				} else {
					result = &ToolResultBlock{
						ToolUseID: tu.ID,
						IsError:   true,
						Content:   []ContentBlock{TextBlock{Text: "tool dispatch aborted"}},
					}
				}
				r.conv.Append(Message{Role: RoleTool, Content: []ContentBlock{*result}})
			}
		}()

		turnID := newTurnID()

		// emitDone is called at all exit paths (normal, error, cancel).
		emitDone := func() {
			doneEv := Event{Kind: EventDone, TurnID: turnID}
			r.dispatcher.emit(ctx, doneEv)
			yield(doneEv)
		}

		td := &ToolDispatcher{
			Tools:      r.agent.cfg.tools,
			Middleware: asToolHookEntries(r.agent.cfg.toolMiddleware),
		}

		for step := 0; step < r.agent.cfg.maxSteps; step++ {
			// Drain any steered messages before building the next LLM request.
			if !r.consumeSteerQueue(ctx, turnID, yield) {
				return
			}

			var accs []toolUseAccum
			stop := StopEndTurn
			var lastUsage *provider.Usage
			done := false
			retryAttempt := 0

		retryLoop:
			for {
				accs = nil
				stop = StopEndTurn
				lastUsage = nil
				done = false

				r.openPartial()

				spec := RequestSpec{
					System:              r.agent.cfg.system,
					Extra:               r.agent.cfg.extraSystem,
					Messages:            r.conv.Messages(),
					Tools:               r.agent.cfg.tools,
					Model:               r.agent.cfg.model,
					Thinking:            r.agent.cfg.thinking,
					StripPartialReasons: r.agent.cfg.stripPartialReasonsOverride,
				}
				req := BuildRequest(spec)

				chunks, err := r.agent.cfg.prov.ChatStream(ctx, req)
				if err != nil {
					retryAttempt++
					if delay, ok := r.shouldRetry(err, retryAttempt); ok {
						if !r.handleRetry(ctx, turnID, retryAttempt, delay, err, lastUsage, yield) {
							return
						}
						continue retryLoop
					}
					r.errorPartial(ctx, turnID, err, nil, yield)
					emitDone()
					return
				}

				for !done {
					select {
					case <-ctx.Done():
						r.cancelPartial(ctx, turnID, lastUsage, yield)
						emitDone()
						return
					case chunk, ok := <-chunks:
						if !ok {
							// Channel closed. If ctx was canceled, treat as cancel; else end-of-stream.
							if ctx.Err() != nil {
								r.cancelPartial(ctx, turnID, lastUsage, yield)
							} else {
								r.closeSteerWindow()
								r.finalizePartial(StopEndTurn, lastUsage)
								extras := r.runTurnEndHooks(ctx, StopEndTurn, lastUsage)
								teEv := Event{Kind: EventTurnEnd, TurnID: turnID, StopReason: StopEndTurn, Usage: lastUsage}
								r.dispatcher.emit(ctx, teEv)
								if yield(teEv) {
									for _, ev := range extras {
										ev.TurnID = turnID
										r.dispatcher.emit(ctx, ev)
										if !yield(ev) {
											break
										}
									}
								}
							}
							emitDone()
							return
						}
						if chunk.Err != nil {
							retryAttempt++
							if delay, ok := r.shouldRetry(chunk.Err, retryAttempt); ok {
								if !r.handleRetry(ctx, turnID, retryAttempt, delay, chunk.Err, lastUsage, yield) {
									return
								}
								continue retryLoop
							}
							r.errorPartial(ctx, turnID, chunk.Err, lastUsage, yield)
							emitDone()
							return
						}
						if chunk.ContentDelta != "" {
							r.appendPartialText(chunk.ContentDelta)
							ev := Event{Kind: EventTextDelta, TurnID: turnID, Delta: chunk.ContentDelta}
							r.dispatcher.emit(ctx, ev)
							if !yield(ev) {
								return
							}
						}
						if chunk.ThinkingDelta != nil {
							r.appendPartialThinking(chunk.ThinkingDelta.Text, chunk.ThinkingDelta.Signature)
							if chunk.ThinkingDelta.Text != "" {
								ev := Event{Kind: EventThinkingDelta, TurnID: turnID, Delta: chunk.ThinkingDelta.Text}
								r.dispatcher.emit(ctx, ev)
								if !yield(ev) {
									return
								}
							}
						}
						// Usage is a cumulative snapshot per provider docs — always
						// overwrite so a mid-stream cancel/error preserves the
						// latest token counts.
						if chunk.Usage != nil {
							lastUsage = chunk.Usage
						}
						if chunk.ToolCallDelta != nil {
							accs = mergeToolCallDelta(accs, *chunk.ToolCallDelta)
							cur := accs[chunk.ToolCallDelta.Index]
							// Mirror current accumulation into the partial so UIs can render
							// the in-flight tool call before args parse.
							if cur.ID != "" {
								r.upsertPartialToolUse(ToolUseBlock{
									ID:      cur.ID,
									Name:    cur.Name,
									RawArgs: cur.argBuf,
									State:   ToolUseStreaming,
								})
							}
							if chunk.ToolCallDelta.ArgsDelta != "" && cur.ID != "" {
								ev := Event{Kind: EventToolDelta, TurnID: turnID, Delta: chunk.ToolCallDelta.ArgsDelta, Tool: &ToolEvent{
									ToolUseID: cur.ID, Name: cur.Name,
								}}
								r.dispatcher.emit(ctx, ev)
								if !yield(ev) {
									return
								}
							}
						}
						if chunk.FinishReason != "" {
							stop = mapFinishReason(chunk.FinishReason)
							done = true
						}
					}
				}
				// Stream completed without a retryable error; exit the retry loop.
				break retryLoop
			}

			// Materialize tool uses and attach to partial assistant before finalizing.
			toolUses := finalizeToolUses(accs)
			if len(toolUses) > 0 {
				r.appendPartialToolUses(toolUses)
			}
			r.finalizePartial(stop, lastUsage)

			// Hand the just-committed tool_uses to the orphan guard. From
			// here on, any early return must leave behind a tool_result for
			// each one (the defer takes care of that).
			pendingToolUses = toolUses
			pendingResults = nil
			appendedCount = 0
			meEv := Event{Kind: EventMessageEnd, TurnID: turnID, StopReason: stop, Usage: lastUsage}
			r.dispatcher.emit(ctx, meEv)
			if !yield(meEv) {
				return
			}

			// If no tools to dispatch, the LLM has produced its final answer.
			// Before declaring the turn over, give pending steer messages a
			// chance to be consumed: a Steer accepted while the LLM was still
			// streaming should be honored even if the model decided to stop.
			// Auto-continue with the queued steers as user messages so the
			// model can react. If nothing's queued, close the steer window
			// atomically with the exit so any concurrent Steer cleanly fails.
			//
			// Note: we gate purely on len(toolUses), not on finishReason. Some
			// Anthropic-compatible endpoints (e.g. DeepSeek's /anthropic/v1)
			// report stop_reason=end_turn even when emitting tool_use blocks,
			// in violation of Anthropic's spec. Treating tool_use presence as
			// the dispatch signal makes the runner robust to such providers
			// without model-specific workarounds, and also prevents an orphan
			// tool_use from leaking through the line-262 continue path when a
			// concurrent Steer is queued.
			if len(toolUses) == 0 {
				if pending := r.drainSteer(); len(pending) > 0 {
					if !r.appendAndEmitSteers(ctx, turnID, pending, yield) {
						return
					}
					continue // next step starts a fresh LLM call with the steer(s) appended
				}
				r.closeSteerWindow()
				extras := r.runTurnEndHooks(ctx, stop, lastUsage)
				teEv := Event{Kind: EventTurnEnd, TurnID: turnID, StopReason: stop, Usage: lastUsage}
				r.dispatcher.emit(ctx, teEv)
				if yield(teEv) {
					for _, ev := range extras {
						ev.TurnID = turnID
						r.dispatcher.emit(ctx, ev)
						if !yield(ev) {
							break
						}
					}
				}
				emitDone()
				return
			}

			// Emit Pre events for all tools in input order, then dispatch batch
			// (parallel by default; serial if any SerialTool is in the batch).
			// Post events emit in input order regardless of goroutine completion.
			for _, tu := range toolUses {
				preEv := Event{Kind: EventPreToolUse, TurnID: turnID, Tool: &ToolEvent{
					ToolUseID: tu.ID, Name: tu.Name, Input: tu.Input,
				}}
				r.dispatcher.emit(ctx, preEv)
				if !yield(preEv) {
					return
				}
			}

			batch := make([]DispatchInput, len(toolUses))
			for i, tu := range toolUses {
				batch[i] = DispatchInput{
					ToolName:  tu.Name,
					ToolUseID: tu.ID,
					Input:     tu.Input,
					Conv:      r.conv,
				}
			}
			results := td.RunBatch(ctx, batch)
			pendingResults = results

			stopHook := false
			for i, tu := range toolUses {
				res := results[i]
				for _, herr := range res.HookErrors {
					hev := Event{Kind: EventError, TurnID: turnID, Error: herr, Tool: &ToolEvent{
						ToolUseID: tu.ID, Name: tu.Name,
					}}
					r.dispatcher.emit(ctx, hev)
					if !yield(hev) {
						return
					}
				}
				for _, d := range res.Deltas {
					deltaEv := Event{Kind: EventToolDelta, TurnID: turnID, Delta: d, Tool: &ToolEvent{
						ToolUseID: tu.ID, Name: tu.Name,
					}}
					r.dispatcher.emit(ctx, deltaEv)
					if !yield(deltaEv) {
						return
					}
				}
				postEv := Event{Kind: EventPostToolUse, TurnID: turnID, Tool: &ToolEvent{
					ToolUseID: tu.ID, Name: tu.Name, Input: tu.Input, Output: res.Output,
				}}
				r.dispatcher.emit(ctx, postEv)
				if !yield(postEv) {
					return
				}
				r.conv.Append(Message{Role: RoleTool, Content: []ContentBlock{*res.Output}})
				appendedCount = i + 1
				if res.Stop {
					stopHook = true
				}
			}
			if stopHook {
				r.closeSteerWindow()
				extras := r.runTurnEndHooks(ctx, StopHook, nil)
				teEv := Event{Kind: EventTurnEnd, TurnID: turnID, StopReason: StopHook}
				r.dispatcher.emit(ctx, teEv)
				if yield(teEv) {
					for _, ev := range extras {
						ev.TurnID = turnID
						r.dispatcher.emit(ctx, ev)
						if !yield(ev) {
							break
						}
					}
				}
				emitDone()
				return
			}
			// Drain steer queue after tool dispatch, before next LLM call.
			if !r.consumeSteerQueue(ctx, turnID, yield) {
				return
			}
			// Loop back to next LLM call (next step).
		}

		// Hit MaxSteps.
		r.closeSteerWindow()
		extras := r.runTurnEndHooks(ctx, StopMaxSteps, nil)
		teEv := Event{Kind: EventTurnEnd, TurnID: turnID, StopReason: StopMaxSteps}
		r.dispatcher.emit(ctx, teEv)
		if yield(teEv) {
			for _, ev := range extras {
				ev.TurnID = turnID
				r.dispatcher.emit(ctx, ev)
				if !yield(ev) {
					break
				}
			}
		}
		emitDone()
	}
}

func (r *Runner) cancelPartial(ctx context.Context, turnID string, usage *provider.Usage, yield func(Event) bool) {
	r.mu.Lock()
	idx := r.partialIndex
	r.partialIndex = -1
	r.steerOpen = false
	reason := r.cancelReason
	r.cancelReason = ""
	r.mu.Unlock()

	// Distinguish explicit cancel from a context deadline expiry.
	stopReason := StopCancelled
	partialReason := PartialCancelled
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		stopReason = StopTimeout
		partialReason = PartialTimeout
	}

	if idx >= 0 {
		r.conv.finalizePartial(idx, partialReason, reason, usage)
	}
	r.dropSteerQueue()
	ev := Event{Kind: EventCancelled, TurnID: turnID, StopReason: stopReason, PartialReason: reason, Usage: usage}
	r.dispatcher.emit(ctx, ev)
	if !yield(ev) {
		return
	}
	extras := r.runTurnEndHooks(ctx, stopReason, usage)
	teEv := Event{Kind: EventTurnEnd, TurnID: turnID, StopReason: stopReason, Usage: usage}
	r.dispatcher.emit(ctx, teEv)
	if yield(teEv) {
		for _, extra := range extras {
			extra.TurnID = turnID
			r.dispatcher.emit(ctx, extra)
			if !yield(extra) {
				break
			}
		}
	}
}

func mergeToolCallDelta(accs []toolUseAccum, d provider.ToolCallDelta) []toolUseAccum {
	for d.Index >= len(accs) {
		accs = append(accs, toolUseAccum{})
	}
	cur := &accs[d.Index]
	if d.ID != "" {
		cur.ID = d.ID
	}
	if d.Name != "" {
		cur.Name = d.Name
	}
	if d.ArgsDelta != "" {
		cur.argBuf += d.ArgsDelta
	}
	return accs
}

func finalizeToolUses(accs []toolUseAccum) []ToolUseBlock {
	out := make([]ToolUseBlock, 0, len(accs))
	for _, a := range accs {
		var input map[string]any
		state := ToolUseReady
		if a.argBuf != "" {
			if err := json.Unmarshal([]byte(a.argBuf), &input); err != nil {
				state = ToolUseMalformed
			}
		}
		if input == nil && state == ToolUseReady && a.argBuf != "" {
			state = ToolUseMalformed
		}
		out = append(out, ToolUseBlock{ID: a.ID, Name: a.Name, Input: input, RawArgs: a.argBuf, State: state})
	}
	return out
}

func mapFinishReason(fr provider.FinishReason) StopReason {
	switch fr {
	case provider.FinishStop:
		return StopEndTurn
	case provider.FinishToolCalls:
		return StopEndTurn // signals tools to be dispatched; loop continues
	case provider.FinishLength:
		return StopTokenLimit
	default:
		return StopEndTurn
	}
}

func newTurnID() string {
	return "turn-" + newConvID()
}
