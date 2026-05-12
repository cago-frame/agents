package codex

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/cago-frame/agents/cliagent/internal/runtime"
	"github.com/cago-frame/agents/provider"
)

const defaultInterruptTimeout = 250 * time.Millisecond

// startTurn wires up steerCh / closeFn on stream and kicks off the headless
// goroutine that drives the codex app-server JSON-RPC for one or more turns.
//
// Steering goes through stream.SetInjectFn — Steer pushes "user" onto steerCh,
// FollowUp goes through sess.EnqueueFollowUp. Pushes are best-effort; the
// runHeadless loop will drain on safe-points.
func (pm *processManager) startTurn(ctx context.Context, sess *runtime.Session, stream *runtime.Stream, prompt string, roc runOptCfg) error {
	steerCh := make(chan steerMsg, 16)
	stream.SetInjectFn(func(injectCtx context.Context, kind, payload string) error {
		switch kind {
		case "user":
			select {
			case steerCh <- steerMsg{text: payload}:
				return nil
			case <-stream.Closed():
				return runtime.ErrStreamClosed
			case <-injectCtx.Done():
				return injectCtx.Err()
			}
		case "follow_up":
			return sess.EnqueueFollowUp(payload)
		}
		return nil
	})
	go pm.runHeadless(ctx, sess, stream, prompt, roc, steerCh)
	return nil
}

// steerMsg is a steer request to be drained at the next safe point.
type steerMsg struct {
	text string
}

// headlessRun encapsulates per-turn state for runHeadless. It is created fresh
// in each run goroutine, never shared.
type headlessRun struct {
	pm     *processManager
	cfg    *backendCfg
	sess   *runtime.Session
	stream *runtime.Stream

	roc     runOptCfg
	runID   string
	steerCh chan steerMsg

	mu           sync.Mutex
	lc           *liveClient
	threadID     string
	activeTurnID string
	released     bool
	forcedStop   runtime.StopReason

	preSeen     map[string]struct{}
	partials    map[string]struct{}
	planSeq     int
	usage       provider.Usage
	usageByTurn map[string]provider.Usage
	lastStop    runtime.StopReason
	stopEmitted bool
}

func (pm *processManager) runHeadless(ctx context.Context, sess *runtime.Session, stream *runtime.Stream, prompt string, roc runOptCfg, steerCh chan steerMsg) {
	r := &headlessRun{
		pm:          pm,
		cfg:         &pm.cfg,
		sess:        sess,
		stream:      stream,
		roc:         roc,
		runID:       newRunID(),
		steerCh:     steerCh,
		preSeen:     map[string]struct{}{},
		partials:    map[string]struct{}{},
		usageByTurn: map[string]provider.Usage{},
	}
	stream.SetCloseFn(r.close)
	defer func() {
		_ = stream.Close(context.Background())
		sess.ReleaseStream(stream)
	}()

	spec := r.buildRunSpec(prompt)
	if err := r.prepareTools(ctx, &spec); err != nil {
		r.emitRunError(err)
		return
	}

	lc, err := pm.acquire(ctx, spec)
	if err != nil {
		r.emitRunError(err)
		return
	}
	r.setLiveClient(lc)
	client := lc.client
	defer func() {
		r.releaseClient(false)
	}()

	stopReason := runtime.StopReason("")
	defer func() {
		if stopReason == runtime.StopError {
			r.releaseClient(true)
		}
	}()

	if stop, err := r.startOrResumeThread(ctx, client, spec); err != nil {
		stopReason = runtime.StopError
		r.emitRunError(err)
		return
	} else if stop != "" {
		stopReason = stop
		r.endRun(ctx, stop)
		return
	}

	pending := []string{prompt}
	maxSteps := r.cfg.maxSteps
	if maxSteps <= 0 {
		maxSteps = defaultMaxSteps
	}

	stop := runtime.StopEndTurn
	for step := 0; step < maxSteps; step++ {
		if len(pending) == 0 {
			break
		}
		msg := pending[0]
		pending = append([]string(nil), pending[1:]...)

		turnStop, err := r.startTurnAndWait(ctx, client, msg)
		stop = turnStop
		if err != nil {
			r.emitEvent(runtime.Event{Kind: runtime.EventError, Err: err})
			stop = runtime.StopError
			break
		}
		if stop == runtime.StopError || stop == runtime.StopCancelled || stop == runtime.StopHook || stop == runtime.StopClosed {
			break
		}

		pending = append(pending, r.drainFollowUps()...)
		if len(pending) > 0 && step+1 >= maxSteps {
			stop = runtime.StopMaxSteps
			break
		}
	}
	stopReason = stop
	r.endRun(ctx, stop)
}

func (r *headlessRun) close(ctx context.Context) error {
	r.mu.Lock()
	lc := r.lc
	threadID := r.threadID
	turnID := r.activeTurnID
	r.mu.Unlock()
	if lc == nil {
		return nil
	}

	var firstErr error
	if threadID != "" && turnID != "" {
		err := interruptTurn(ctx, lc.client, threadID, turnID)
		if err != nil && !errors.Is(err, ErrProcessDead) {
			firstErr = err
		}
		if errors.Is(err, ErrProcessDead) {
			r.releaseClient(true)
			return firstErr
		}
	}
	r.releaseClient(false)
	return firstErr
}

func (r *headlessRun) prepareTools(ctx context.Context, spec *runSpec) error {
	tools := r.cfg.agentTools
	if len(tools) == 0 {
		return nil
	}
	bridge, err := r.pm.acquireMCPBridge()
	if err != nil {
		return err
	}
	if err := bridge.ensureRegistered(tools); err != nil {
		return err
	}
	if _, err := bridge.start(ctx); err != nil {
		return err
	}
	bridge.applyToSpec(spec)
	return nil
}

func (r *headlessRun) startOrResumeThread(ctx context.Context, c *appClient, spec runSpec) (runtime.StopReason, error) {
	var (
		raw []byte
		err error
	)
	params := threadParams(spec, r.cfg.systemPrompt)
	if spec.resumeID != "" {
		delete(params, "ephemeral")
		params["threadId"] = spec.resumeID
		params["excludeTurns"] = true
		raw, err = c.Call(ctx, appMethodThreadResume, params)
		if err != nil {
			return "", err
		}
		var res appThreadResumeResponse
		if err := json.Unmarshal(raw, &res); err != nil {
			return "", err
		}
		r.setThreadID(res.Thread.ID)
		return r.emitSessionStart(ctx, res.Thread.ID, res.Thread.Cwd), nil
	}
	raw, err = c.Call(ctx, appMethodThreadStart, params)
	if err != nil {
		return "", err
	}
	var res appThreadStartResponse
	if err := json.Unmarshal(raw, &res); err != nil {
		return "", err
	}
	r.setThreadID(res.Thread.ID)
	return r.emitSessionStart(ctx, res.Thread.ID, res.Thread.Cwd), nil
}

func (r *headlessRun) emitSessionStart(ctx context.Context, threadID, cwd string) runtime.StopReason {
	r.sess.UpdateState(func(st *runtime.State) {
		st.ThreadID = threadID
	})
	r.emitEvent(runtime.Event{
		Kind:      runtime.EventSessionStart,
		SessionID: threadID,
		Cwd:       cwd,
	})
	out, err := r.runHooks(ctx, runtime.StageSessionStart, runtime.HookInput{
		Stage: runtime.StageSessionStart,
		Cwd:   cwd,
	})
	if err != nil {
		r.emitEvent(runtime.Event{Kind: runtime.EventError, Err: err})
		return runtime.StopError
	}
	if out.StopRun {
		return runtime.StopHook
	}
	return ""
}

func (r *headlessRun) startTurnAndWait(ctx context.Context, c *appClient, msg string) (runtime.StopReason, error) {
	text, stop, err := r.acceptUserMessage(ctx, msg, false)
	if err != nil || stop != "" {
		return stop, err
	}
	raw, err := c.Call(ctx, appMethodTurnStart, map[string]any{
		"threadId": r.threadID,
		"input":    userInput(text),
	})
	if err != nil {
		return runtime.StopError, err
	}
	var res appTurnStartResponse
	if err := json.Unmarshal(raw, &res); err != nil {
		return runtime.StopError, err
	}
	r.setActiveTurn(res.Turn.ID)
	return r.waitTurn(ctx, c, res.Turn.ID)
}

func (r *headlessRun) waitTurn(ctx context.Context, c *appClient, turnID string) (runtime.StopReason, error) {
	doneCh := c.Done()
	for {
		select {
		case in, ok := <-c.Incoming():
			if !ok {
				return runtime.StopError, ErrProcessDead
			}
			stop, done, err := r.handleInbound(ctx, c, in, turnID)
			if err != nil {
				return runtime.StopError, err
			}
			if done {
				r.setActiveTurn("")
				return stop, nil
			}
		case req := <-r.steerCh:
			_ = r.handleSteer(ctx, c, req.text)
		case <-doneCh:
			err := c.Err()
			if err == nil {
				doneCh = nil
				continue
			}
			return runtime.StopError, err
		case <-ctx.Done():
			return runtime.StopCancelled, ctx.Err()
		}
	}
}

func (r *headlessRun) handleInbound(ctx context.Context, c *appClient, in appInbound, turnID string) (runtime.StopReason, bool, error) {
	if in.Kind == appInboundRequest {
		return "", false, r.handleServerRequest(ctx, c, in)
	}

	n, err := parseNotification(in.Method, in.Params)
	if err != nil {
		return "", false, err
	}
	switch in.Method {
	case appMethodThreadStarted:
		if n.Thread != nil && n.Thread.ID != r.threadID {
			r.emitSessionStart(ctx, n.Thread.ID, n.Thread.Cwd)
		}
	case appMethodItemAgentMessageDelta:
		r.partials[n.ItemID] = struct{}{}
		r.emitEvent(runtime.Event{Kind: runtime.EventTextDelta, Text: n.Delta, Raw: in.Params})
	case appMethodItemReasoningTextDelta, appMethodItemReasoningSummaryTextDelta:
		r.emitEvent(runtime.Event{Kind: runtime.EventThinkingDelta, Text: n.Delta, Raw: in.Params})
	case appMethodThreadTokenUsageUpdated:
		usage := appUsageToProvider(n.Usage)
		r.recordUsage(n.TurnID, usage)
		r.emitEvent(runtime.Event{Kind: runtime.EventUsage, Usage: usage, Raw: in.Params})
	case appMethodTurnPlanUpdated:
		r.handlePlanUpdated(n, in.Params)
	case appMethodItemStarted:
		r.handleItemStarted(ctx, c, n, in.Params)
	case appMethodItemCompleted:
		r.handleItemCompleted(ctx, c, n, in.Params)
	case appMethodItemFileChangePatchUpdated:
		r.handleFileChangePatchUpdated(n, in.Params)
	case appMethodTurnCompleted:
		if n.Turn == nil || n.Turn.ID != turnID {
			return "", false, nil
		}
		stop := r.overrideStop(appStopForTurn(n.Turn))
		if err := appTurnErr(n.Turn); err != nil {
			r.emitEvent(runtime.Event{Kind: runtime.EventError, Err: err, Raw: in.Params})
		}
		r.emitStop(stop)
		return stop, true, nil
	case appMethodError:
		r.emitEvent(runtime.Event{Kind: runtime.EventError, Err: fmt.Errorf("codex app-server: %s", string(in.Params)), Raw: in.Params})
	}
	return "", false, nil
}

func (r *headlessRun) handleItemStarted(ctx context.Context, c *appClient, n appNotification, raw json.RawMessage) {
	if n.Item == nil {
		return
	}
	if n.Item.Type == appItemUserMessage {
		return
	}
	if _, ok := r.preSeen[n.Item.ID]; ok {
		return
	}
	switch n.Item.Type {
	case appItemAgentMessage, appItemReasoning, appItemPlan:
		return
	}
	if n.Item.Type == appItemFileChange && len(n.Item.Changes) == 0 {
		return
	}
	name := toolNameForItem(n.Item)
	if name == "" {
		return
	}
	r.handlePreToolUse(ctx, c, n.Item, raw)
}

func (r *headlessRun) handlePreToolUse(ctx context.Context, c *appClient, item *appThreadItem, raw json.RawMessage) {
	if item == nil {
		return
	}
	name := toolNameForItem(item)
	if name == "" {
		return
	}
	if _, ok := r.preSeen[item.ID]; ok {
		return
	}
	r.preSeen[item.ID] = struct{}{}
	r.emitPreToolUse(item, raw)
	out, err := r.runHooks(ctx, runtime.StagePreToolUse, runtime.HookInput{
		Stage:    runtime.StagePreToolUse,
		Cwd:      r.effectiveCwd(),
		ToolName: name,
		ToolID:   item.ID,
		Input:    toolInputForItem(item),
		Raw:      raw,
	})
	if err != nil {
		r.emitEvent(runtime.Event{Kind: runtime.EventError, Err: err})
		return
	}
	if out.StopRun {
		r.forceStop(runtime.StopHook)
		_ = r.interruptActiveTurn(ctx, c)
		return
	}
	if out.Decision == runtime.DecisionDeny {
		_ = r.interruptActiveTurn(ctx, c)
		return
	}
	if out.AdditionalContext != "" {
		_ = r.sendSteer(ctx, c, "Additional context from PreToolUse hook:\n"+out.AdditionalContext)
	}
}

func (r *headlessRun) emitPreToolUseIfMissing(item *appThreadItem, raw json.RawMessage) {
	if item == nil || item.ID == "" {
		return
	}
	if _, ok := r.preSeen[item.ID]; ok {
		return
	}
	r.preSeen[item.ID] = struct{}{}
	r.emitPreToolUse(item, raw)
}

func (r *headlessRun) emitPreToolUse(item *appThreadItem, raw json.RawMessage) {
	name := toolNameForItem(item)
	r.emitEvent(runtime.Event{
		Kind: runtime.EventPreToolUse,
		Tool: &runtime.ToolEvent{
			ID:     item.ID,
			Name:   name,
			Input:  toolInputForItem(item),
			Source: toolSourceForItem(item),
		},
		Raw: raw,
	})
}

func (r *headlessRun) handleItemCompleted(ctx context.Context, c *appClient, n appNotification, raw json.RawMessage) {
	if n.Item == nil {
		return
	}
	switch n.Item.Type {
	case appItemAgentMessage:
		if _, ok := r.partials[n.Item.ID]; !ok && n.Item.Text != "" {
			r.emitEvent(runtime.Event{Kind: runtime.EventTextDelta, Text: n.Item.Text, Raw: raw})
		}
		msg := appMessageFromItem(n.Item, raw)
		if msg.Persist {
			r.sess.AppendHistory(msg)
		}
		r.emitEvent(runtime.Event{Kind: runtime.EventMessageEnd, Message: &msg, Raw: raw})
	case appItemCommandExecution, appItemFileChange, appItemMCPToolCall, appItemDynamicToolCall, appItemCollabAgentTool:
		r.handleToolCompleted(ctx, c, n.Item, raw)
	}
}

func (r *headlessRun) handleToolCompleted(ctx context.Context, c *appClient, item *appThreadItem, raw json.RawMessage) {
	r.emitPreToolUseIfMissing(item, raw)
	name := toolNameForItem(item)
	tev := &runtime.ToolEvent{
		ID:       item.ID,
		Name:     name,
		Input:    toolInputForItem(item),
		Response: toolResponseForItem(item),
		Err:      toolErrForItem(item),
		Source:   toolSourceForItem(item),
	}
	r.emitEvent(runtime.Event{Kind: runtime.EventPostToolUse, Tool: tev, Raw: raw})
	out, err := r.runHooks(ctx, runtime.StagePostToolUse, runtime.HookInput{
		Stage:    runtime.StagePostToolUse,
		Cwd:      r.effectiveCwd(),
		ToolName: name,
		ToolID:   item.ID,
		Input:    tev.Input,
		Output:   tev.Response,
		Raw:      raw,
	})
	if err != nil {
		r.emitEvent(runtime.Event{Kind: runtime.EventError, Err: err})
		return
	}
	if out.StopRun {
		r.forceStop(runtime.StopHook)
		_ = r.interruptActiveTurn(ctx, c)
		return
	}
	if out.AdditionalContext != "" {
		_ = r.sendSteer(ctx, c, "Additional context from PostToolUse hook:\n"+out.AdditionalContext)
	}
}

func (r *headlessRun) handlePlanUpdated(n appNotification, raw json.RawMessage) {
	if len(n.Plan) == 0 && n.Explanation == nil {
		return
	}
	r.planSeq++
	id := fmt.Sprintf("%s:plan:%d", n.TurnID, r.planSeq)
	tool := &runtime.ToolEvent{
		ID:       id,
		Name:     string(appToolUpdatePlan),
		Input:    append(json.RawMessage(nil), raw...),
		Response: append(json.RawMessage(nil), raw...),
		Source:   runtime.ToolSourceBuiltin,
	}
	r.emitEvent(runtime.Event{Kind: runtime.EventPreToolUse, Tool: tool, Raw: raw})
	r.emitEvent(runtime.Event{Kind: runtime.EventPostToolUse, Tool: tool, Raw: raw})
}

func (r *headlessRun) handleFileChangePatchUpdated(n appNotification, raw json.RawMessage) {
	if n.ItemID == "" || len(n.Changes) == 0 {
		return
	}
	r.emitPreToolUseIfMissing(&appThreadItem{
		Type:    appItemFileChange,
		ID:      n.ItemID,
		Status:  appStatusInProgress,
		Changes: n.Changes,
	}, raw)
}

func (r *headlessRun) handleSteer(ctx context.Context, c *appClient, text string) error {
	cleaned, stop, err := r.acceptUserMessage(ctx, text, true)
	if err != nil {
		return err
	}
	if stop == runtime.StopHook {
		r.forceStop(runtime.StopHook)
		return r.interruptActiveTurn(ctx, c)
	}
	return r.sendSteer(ctx, c, cleaned)
}

func (r *headlessRun) sendSteer(ctx context.Context, c *appClient, text string) error {
	r.mu.Lock()
	threadID := r.threadID
	turnID := r.activeTurnID
	r.mu.Unlock()
	if threadID == "" || turnID == "" {
		return runtime.ErrSteeringUnavailable
	}
	_, err := c.Call(ctx, appMethodTurnSteer, map[string]any{
		"threadId":       threadID,
		"expectedTurnId": turnID,
		"input":          userInput(text),
	})
	return err
}

// errHookDeny matches the legacy soft-deny semantics for steer.
var errHookDeny = errors.New("codex: hook denied request")

func (r *headlessRun) acceptUserMessage(ctx context.Context, text string, softDeny bool) (string, runtime.StopReason, error) {
	r.emitEvent(runtime.Event{
		Kind:   runtime.EventUserPromptSubmit,
		Prompt: text,
	})
	out, err := r.runHooks(ctx, runtime.StageUserPromptSubmit, runtime.HookInput{
		Stage:  runtime.StageUserPromptSubmit,
		Cwd:    r.effectiveCwd(),
		Prompt: text,
	})
	if err != nil {
		r.emitEvent(runtime.Event{Kind: runtime.EventError, Err: err})
		return "", runtime.StopError, err
	}
	if out.Decision == runtime.DecisionDeny {
		if softDeny {
			return "", "", errHookDeny
		}
		return "", runtime.StopHook, nil
	}
	if out.StopRun {
		return "", runtime.StopHook, nil
	}
	final := text
	if out.AdditionalContext != "" {
		final = "Additional context from UserPromptSubmit hook:\n" + out.AdditionalContext + "\n\nUser request:\n" + final
	}
	return final, "", nil
}

func (r *headlessRun) handleServerRequest(ctx context.Context, c *appClient, in appInbound) error {
	switch in.Method {
	case appMethodItemCommandApprovalRequest:
		return r.handleApprovalRequest(ctx, c, in, string(appToolCommandExecution), commandApprovalInput(in.Params), commandApprovalAccept, commandApprovalDecline)
	case appMethodItemFileApprovalRequest:
		return r.handleApprovalRequest(ctx, c, in, string(appToolFileChange), in.Params, fileApprovalAccept, fileApprovalDecline)
	case appMethodItemPermissionsRequest:
		return c.Respond(ctx, in.ID, map[string]any{"permissions": map[string]any{}, "scope": "turn"})
	case appMethodItemToolRequestUserInput:
		r.emitEvent(runtime.Event{Kind: runtime.EventNotification, Text: "codex requested user input", Raw: in.Params})
		return c.Respond(ctx, in.ID, map[string]any{"answers": map[string]any{}})
	case appMethodMCPServerElicitationRequest:
		return c.Respond(ctx, in.ID, map[string]any{"action": appElicitationCancel, "content": nil, "_meta": nil})
	case appMethodItemToolCall:
		return c.Respond(ctx, in.ID, map[string]any{"contentItems": []any{}, "success": false})
	default:
		return c.Respond(ctx, in.ID, map[string]any{})
	}
}

func (r *headlessRun) handleApprovalRequest(
	ctx context.Context,
	c *appClient,
	in appInbound,
	toolName string,
	toolInput json.RawMessage,
	accept func() map[string]any,
	decline func() map[string]any,
) error {
	var itemID string
	var meta struct {
		ItemID string `json:"itemId"`
	}
	_ = json.Unmarshal(in.Params, &meta)
	itemID = meta.ItemID
	if itemID != "" {
		r.preSeen[itemID] = struct{}{}
	}
	r.emitEvent(runtime.Event{
		Kind: runtime.EventPreToolUse,
		Tool: &runtime.ToolEvent{ID: itemID, Name: toolName, Input: toolInput, Source: runtime.ToolSourceBuiltin},
		Raw:  in.Params,
	})
	out, err := r.runHooks(ctx, runtime.StagePreToolUse, runtime.HookInput{
		Stage:    runtime.StagePreToolUse,
		Cwd:      r.effectiveCwd(),
		ToolName: toolName,
		ToolID:   itemID,
		Input:    toolInput,
		Raw:      in.Params,
	})
	if err != nil {
		r.emitEvent(runtime.Event{Kind: runtime.EventError, Err: err})
		return c.Respond(ctx, in.ID, decline())
	}
	if out.Decision == runtime.DecisionDeny {
		return c.Respond(ctx, in.ID, decline())
	}
	if out.StopRun {
		r.forceStop(runtime.StopHook)
		if err := c.Respond(ctx, in.ID, decline()); err != nil {
			return err
		}
		_ = r.interruptActiveTurn(ctx, c)
		return nil
	}
	if out.AdditionalContext != "" {
		_ = r.sendSteer(ctx, c, "Additional context from PreToolUse hook:\n"+out.AdditionalContext)
	}
	if out.Decision == runtime.DecisionApprove {
		return c.Respond(ctx, in.ID, accept())
	}
	return c.Respond(ctx, in.ID, decline())
}

// drainFollowUps drains pending steer messages (non-blocking) plus session
// follow-ups, in FIFO order.
func (r *headlessRun) drainFollowUps() []string {
	var out []string
	for {
		select {
		case req := <-r.steerCh:
			out = append(out, req.text)
		default:
			goto doneSteer
		}
	}
doneSteer:
	if r.sess != nil {
		for {
			next, ok := r.sess.DequeueFollowUp()
			if !ok {
				break
			}
			out = append(out, next)
		}
	}
	return out
}

func (r *headlessRun) endRun(ctx context.Context, stop runtime.StopReason) {
	if stop == "" {
		stop = runtime.StopEndTurn
	}
	if !r.stopEmitted || r.lastStop != stop {
		r.emitStop(stop)
	}
	if _, err := r.runHooks(ctx, runtime.StageStop, runtime.HookInput{
		Stage: runtime.StageStop,
		Cwd:   r.effectiveCwd(),
	}); err != nil {
		r.emitEvent(runtime.Event{Kind: runtime.EventError, Err: err})
	}
	r.emitEvent(runtime.Event{
		Kind: runtime.EventSessionEnd,
		Stop: stop,
	})
	if _, err := r.runHooks(ctx, runtime.StageSessionEnd, runtime.HookInput{
		Stage: runtime.StageSessionEnd,
		Cwd:   r.effectiveCwd(),
	}); err != nil {
		r.emitEvent(runtime.Event{Kind: runtime.EventError, Err: err})
	}
	r.emitEvent(runtime.Event{
		Kind:  runtime.EventDone,
		Stop:  stop,
		Usage: r.usage,
	})
	r.stream.SetResult(&runtime.Result{
		Stop:     stop,
		ThreadID: r.threadID,
		State:    r.sess.State(),
		History:  r.sess.History(),
		Usage:    r.usage,
	}, nil)
}

func (r *headlessRun) emitStop(stop runtime.StopReason) {
	if stop == "" {
		return
	}
	r.lastStop = stop
	r.stopEmitted = true
	r.emitEvent(runtime.Event{
		Kind: runtime.EventStop,
		Stop: stop,
	})
}

func (r *headlessRun) runHooks(ctx context.Context, stage runtime.HookStage, in runtime.HookInput) (runtime.HookOutput, error) {
	hooks := compileRuntimeHooks(r.cfg.agentHooks)
	if len(hooks) == 0 {
		return runtime.HookOutput{}, nil
	}
	return runtime.RunHookChain(ctx, hooks, stage, in)
}

func (r *headlessRun) emitRunError(err error) {
	r.emitEvent(runtime.Event{Kind: runtime.EventError, Err: err})
	r.emitEvent(runtime.Event{Kind: runtime.EventDone, Stop: runtime.StopError})
	r.stream.SetResult(&runtime.Result{Stop: runtime.StopError}, err)
}

func (r *headlessRun) emitEvent(ev runtime.Event) {
	if ev.RunID == "" {
		ev.RunID = r.runID
	}
	if ev.SessionID == "" {
		ev.SessionID = r.threadID
	}
	if ev.Cwd == "" {
		ev.Cwd = r.effectiveCwd()
	}
	r.stream.Emit(ev)
}

func (r *headlessRun) setLiveClient(lc *liveClient) {
	r.mu.Lock()
	r.lc = lc
	r.mu.Unlock()
}

func (r *headlessRun) releaseClient(markDead bool) {
	r.mu.Lock()
	if r.released || r.lc == nil {
		r.released = true
		r.mu.Unlock()
		return
	}
	lc := r.lc
	threadID := r.threadID
	r.released = true
	r.mu.Unlock()

	if markDead {
		r.pm.markDead(lc)
		return
	}
	r.pm.release(lc, threadID)
}

func (r *headlessRun) setThreadID(id string) {
	r.mu.Lock()
	r.threadID = id
	r.mu.Unlock()
}

func (r *headlessRun) setActiveTurn(id string) {
	r.mu.Lock()
	r.activeTurnID = id
	r.mu.Unlock()
}

func (r *headlessRun) forceStop(stop runtime.StopReason) {
	r.mu.Lock()
	r.forcedStop = stop
	r.mu.Unlock()
}

func (r *headlessRun) overrideStop(stop runtime.StopReason) runtime.StopReason {
	r.mu.Lock()
	forced := r.forcedStop
	r.mu.Unlock()
	if forced != "" {
		return forced
	}
	return stop
}

func (r *headlessRun) recordUsage(turnID string, usage provider.Usage) {
	if turnID == "" {
		r.usage = addUsage(r.usage, usage)
		return
	}
	r.usageByTurn[turnID] = usage
	var total provider.Usage
	for _, u := range r.usageByTurn {
		total = addUsage(total, u)
	}
	r.usage = total
}

func (r *headlessRun) interruptActiveTurn(ctx context.Context, c *appClient) error {
	r.mu.Lock()
	threadID := r.threadID
	turnID := r.activeTurnID
	r.mu.Unlock()
	if threadID == "" || turnID == "" {
		return nil
	}
	return interruptTurn(ctx, c, threadID, turnID)
}

func (r *headlessRun) effectiveCwd() string {
	if r.roc.cwd != "" {
		return r.roc.cwd
	}
	if r.cfg.cwd != "" {
		return r.cfg.cwd
	}
	if cwd, err := os.Getwd(); err == nil {
		return cwd
	}
	return ""
}

func (r *headlessRun) buildRunSpec(prompt string) runSpec {
	cfg := r.cfg
	roc := r.roc
	env := cloneMap(cfg.env)
	spec := runSpec{
		binary:    cfg.binary,
		prompt:    prompt,
		resumeID:  r.sess.State().ThreadID,
		model:     cfg.model,
		cwd:       cfg.cwd,
		env:       env,
		sandbox:   cfg.sandbox,
		approval:  cfg.approval,
		bypass:    cfg.bypass,
		ephemeral: cfg.ephemeral,
		config:    append([]string(nil), cfg.config...),
		extraArgs: append([]string(nil), cfg.extraArgs...),
	}
	if roc.cwd != "" {
		spec.cwd = roc.cwd
	}
	if roc.sandbox != "" {
		spec.sandbox = roc.sandbox
	}
	if roc.approval != "" {
		spec.approval = roc.approval
	}
	if roc.ephemeral != nil {
		spec.ephemeral = *roc.ephemeral
	}
	if len(roc.config) > 0 {
		spec.config = append(spec.config, roc.config...)
	}
	if len(roc.extraArgs) > 0 {
		spec.extraArgs = append(spec.extraArgs, roc.extraArgs...)
	}
	if spec.bypass {
		spec.sandbox = SandboxDangerFullAccess
		spec.approval = ApprovalNever
	}
	return spec
}

// compileRuntimeHooks turns native hookSpec entries into runtime.Hook slice.
func compileRuntimeHooks(hooks []hookSpec) []runtime.Hook {
	out := make([]runtime.Hook, 0, len(hooks))
	for _, h := range hooks {
		out = append(out, runtime.Hook{
			Stage:   stageCodexToRuntime(h.Stage),
			Matcher: h.Matcher,
			Fn:      wrapHookFunc(h.Fn),
		})
	}
	return out
}

// buildAppServerArgs builds the argv passed to `codex app-server`.
func buildAppServerArgs(spec runSpec) []string {
	args := []string{appServerCommand, appServerListenFlag, appServerListenStdio}
	for _, c := range spec.config {
		if c != "" {
			args = append(args, appServerConfigFlag, c)
		}
	}
	if spec.mcpURL != "" {
		name := spec.mcpServerName
		if name == "" {
			name = mcpBridgeServerName
		}
		args = append(args,
			appServerConfigFlag, "mcp_servers."+name+".url="+strconvQuote(spec.mcpURL),
			appServerConfigFlag, "mcp_servers."+name+".bearer_token_env_var="+strconvQuote(spec.mcpTokenEnv),
			appServerConfigFlag, "mcp_servers."+name+".default_tools_approval_mode="+strconvQuote(mcpDefaultToolApprovalMode),
		)
	}
	args = append(args, spec.extraArgs...)
	return args
}

func threadParams(spec runSpec, system string) map[string]any {
	params := map[string]any{}
	if spec.model != "" {
		params["model"] = spec.model
	}
	if spec.cwd != "" {
		params["cwd"] = spec.cwd
	}
	if spec.sandbox != "" {
		params["sandbox"] = string(spec.sandbox)
	}
	if spec.approval != "" {
		params["approvalPolicy"] = string(spec.approval)
	}
	if spec.ephemeral {
		params["ephemeral"] = true
	}
	if strings.TrimSpace(system) != "" {
		params["developerInstructions"] = system
	}
	return params
}

func commandApprovalInput(raw json.RawMessage) json.RawMessage {
	var p struct {
		Command string `json:"command"`
		Cwd     string `json:"cwd"`
		Reason  string `json:"reason"`
	}
	if err := json.Unmarshal(raw, &p); err != nil {
		return raw
	}
	return mustRaw(map[string]any{"command": p.Command, "cwd": p.Cwd, "reason": p.Reason})
}

func commandApprovalAccept() map[string]any  { return map[string]any{"decision": appApprovalAccept} }
func commandApprovalDecline() map[string]any { return map[string]any{"decision": appApprovalDecline} }
func fileApprovalAccept() map[string]any     { return map[string]any{"decision": appApprovalAccept} }
func fileApprovalDecline() map[string]any    { return map[string]any{"decision": appApprovalDecline} }

func buildEnv(extra map[string]string) []string {
	if len(extra) == 0 {
		return nil
	}
	env := os.Environ()
	for k, v := range extra {
		env = append(env, k+"="+v)
	}
	return env
}

func cloneMap(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func addUsage(a, b provider.Usage) provider.Usage {
	return provider.Usage{
		PromptTokens:        a.PromptTokens + b.PromptTokens,
		CompletionTokens:    a.CompletionTokens + b.CompletionTokens,
		ReasoningTokens:     a.ReasoningTokens + b.ReasoningTokens,
		CachedTokens:        a.CachedTokens + b.CachedTokens,
		CacheCreationTokens: a.CacheCreationTokens + b.CacheCreationTokens,
		TotalTokens:         a.TotalTokens + b.TotalTokens,
	}
}

func interruptTurn(ctx context.Context, c *appClient, threadID, turnID string) error {
	if c == nil || threadID == "" || turnID == "" {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	callCtx, cancel := context.WithTimeout(ctx, defaultInterruptTimeout)
	defer cancel()
	_, err := c.Call(callCtx, appMethodTurnInterrupt, map[string]any{
		"threadId": threadID,
		"turnId":   turnID,
	})
	if err != nil && errors.Is(err, context.DeadlineExceeded) && ctx.Err() == nil {
		return nil
	}
	return err
}

func strconvQuote(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}
