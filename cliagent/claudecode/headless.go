package claudecode

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"

	"github.com/cago-frame/agents/cliagent/internal/runtime"
)

// newRunID generates a random 16-hex-character run ID.
func newRunID() string {
	var buf [8]byte
	_, _ = rand.Read(buf[:])
	return hex.EncodeToString(buf[:])
}

// permissionResponse carries a permission decision from the consumer back to
// the headless loop, which writes it to the CLI stdin as a control_response.
type permissionResponse struct {
	id    string
	allow bool
}

// runHeadless drives a multi-turn headless stream into the supplied
// runtime.Stream. It is launched as a goroutine by processManager.startTurn
// and owns the loop that translates Claude Code stream-json into runtime.Event
// values.
//
// steerCh is the channel populated by Stream.Steer; permCh carries permission
// decisions from Stream.RespondPermission; follow-ups come from sess.DequeueFollowUp.
//
// The first turn uses prompt; subsequent turns drain steerCh + the session's
// follow-up queue at safe points and re-fire startNextTurn.
func (pm *processManager) runHeadless(
	ctx context.Context,
	cfg *backendCfg,
	sess *runtime.Session,
	stream *runtime.Stream,
	prompt string,
	roc runOptCfg,
	steerCh <-chan string,
	permCh <-chan permissionResponse,
) {
	defer func() {
		_ = stream.Close(context.Background())
		sess.ReleaseStream(stream)
	}()

	runID := newRunID()

	spec := buildRunSpec(cfg, prompt, roc, sess.State().ThreadID)

	// MCP wiring (lazy).
	if len(cfg.agentTools) > 0 {
		bridge, err := pm.acquireMCPBridge(ctx, cfg)
		if err != nil {
			stream.Emit(runtime.Event{Kind: runtime.EventError, RunID: runID, Err: err})
			stream.Emit(runtime.Event{Kind: runtime.EventDone, RunID: runID, Stop: runtime.StopError})
			stream.SetResult(&runtime.Result{Stop: runtime.StopError}, err)
			return
		}
		ep := bridge.endpoint()
		spec.mcpConfig = bridge.configJSON()
		names := bridge.toolNames()
		spec.mcpRegisteredNames = make(map[string]struct{}, len(names))
		for _, n := range names {
			spec.mcpRegisteredNames[n] = struct{}{}
		}
		if !cfg.strictAllowed {
			for _, n := range names {
				spec.allowedTools = append(spec.allowedTools, fmt.Sprintf("mcp__%s__%s", ep.ServerName, n))
			}
		}
	}

	// Hook bridge wiring (lazy).
	if len(cfg.agentHooks) > 0 {
		hooks := compileRuntimeHooks(cfg.agentHooks)
		hookBr, err := pm.acquireHookBridge(hooks)
		if err != nil {
			stream.Emit(runtime.Event{Kind: runtime.EventError, RunID: runID, Err: err})
			stream.Emit(runtime.Event{Kind: runtime.EventDone, RunID: runID, Stop: runtime.StopError})
			stream.SetResult(&runtime.Result{Stop: runtime.StopError}, err)
			return
		}
		settings, err := hookBr.settingsJSON()
		if err != nil {
			stream.Emit(runtime.Event{Kind: runtime.EventError, RunID: runID, Err: err})
			stream.Emit(runtime.Event{Kind: runtime.EventDone, RunID: runID, Stop: runtime.StopError})
			stream.SetResult(&runtime.Result{Stop: runtime.StopError}, err)
			return
		}
		spec.settings = settings
	}

	maxSteps := cfg.maxSteps
	if maxSteps <= 0 {
		maxSteps = defaultMaxSteps
	}

	stream.Emit(runtime.Event{
		Kind:   runtime.EventUserPromptSubmit,
		RunID:  runID,
		Prompt: prompt,
	})

	handle, err := pm.acquireOrSpawn(ctx, spec)
	if err != nil {
		stream.Emit(runtime.Event{Kind: runtime.EventError, RunID: runID, Err: err})
		stream.Emit(runtime.Event{Kind: runtime.EventDone, RunID: runID, Stop: runtime.StopError})
		stream.SetResult(&runtime.Result{Stop: runtime.StopError}, err)
		return
	}

	var lastStop runtime.StopReason

	for step := 0; step < maxSteps; step++ {
		var turnStop runtime.StopReason
		for ev := range handle.Events {
			ev.RunID = runID
			if ev.Kind == runtime.EventSessionStart && ev.SessionID != "" {
				sess.UpdateState(func(st *runtime.State) {
					st.ThreadID = ev.SessionID
				})
			}
			if ev.Kind == runtime.EventMessageEnd && ev.Message != nil {
				if ev.Message.Persist {
					sess.AppendHistory(*ev.Message)
				}
			}
			if ev.Kind == runtime.EventPermissionRequest {
				stream.Emit(ev)
				select {
				case resp := <-permCh:
					if err := writePermissionResponse(handle.Stdin, resp.id, resp.allow); err != nil {
						stream.Emit(runtime.Event{Kind: runtime.EventError, RunID: runID, Err: err})
					}
				case <-ctx.Done():
					_ = writePermissionResponse(handle.Stdin, ev.PermissionRequestID, false)
					stream.Emit(runtime.Event{Kind: runtime.EventDone, RunID: runID, Stop: runtime.StopCancelled})
					stream.SetResult(&runtime.Result{Stop: runtime.StopCancelled, ThreadID: sess.State().ThreadID, State: sess.State(), History: sess.History()}, ctx.Err())
					return
				}
				continue
			}
			if ev.Kind == runtime.EventDone {
				turnStop = ev.Stop
				break
			}
			stream.Emit(ev)
		}
		lastStop = turnStop

		if turnStop == runtime.StopError || turnStop == runtime.StopCancelled ||
			turnStop == runtime.StopHook || turnStop == runtime.StopClosed {
			break
		}

		// Safe-point: collect steerCh + Session.followUp.
		pending := collectPending(steerCh, sess)
		if len(pending) == 0 {
			break
		}

		if step+1 >= maxSteps {
			lastStop = runtime.StopMaxSteps
			threadID := handle.ThreadID
			go func() { _ = pm.closeByThreadID(ctx, threadID) }()
			break
		}

		nextMsg := pending[0]
		for _, p := range pending[1:] {
			_ = sess.EnqueueFollowUp(p)
		}

		stream.Emit(runtime.Event{
			Kind:   runtime.EventUserPromptSubmit,
			RunID:  runID,
			Prompt: nextMsg,
		})

		threadID := handle.ThreadID
		nextHandle, nextErr := pm.startNextTurn(ctx, threadID, nextMsg)
		if nextErr != nil {
			stream.Emit(runtime.Event{Kind: runtime.EventError, RunID: runID, Err: nextErr})
			lastStop = runtime.StopError
			break
		}
		handle = nextHandle
	}

	if lastStop == "" {
		lastStop = runtime.StopMaxSteps
	}

	stream.Emit(runtime.Event{Kind: runtime.EventDone, RunID: runID, Stop: lastStop})
	stream.SetResult(&runtime.Result{
		Stop:     lastStop,
		ThreadID: sess.State().ThreadID,
		State:    sess.State(),
		History:  sess.History(),
	}, nil)
}

// collectPending drains steerCh (non-blocking) then session follow-ups, in FIFO
// order. The runHeadless loop calls this at safe points after each turn.
func collectPending(steerCh <-chan string, sess *runtime.Session) []string {
	var out []string
	for {
		select {
		case msg := <-steerCh:
			out = append(out, msg)
		default:
			goto doneSteer
		}
	}
doneSteer:
	if sess != nil {
		for {
			next, ok := sess.DequeueFollowUp()
			if !ok {
				break
			}
			out = append(out, next)
		}
	}
	return out
}

// buildRunSpec assembles a processSpec from backend cfg + per-run overrides.
func buildRunSpec(cfg *backendCfg, prompt string, roc runOptCfg, resumeID string) processSpec {
	spec := processSpec{
		binary:                cfg.binary,
		prompt:                prompt,
		model:                 cfg.model,
		cwd:                   cfg.cwd,
		env:                   cfg.env,
		systemPrompt:          cfg.systemPrompt,
		permissionMode:        cfg.permission,
		interactivePermission: cfg.interactivePermission,
		allowedTools:          cfg.allowed,
		disallowedTools:       cfg.disallowed,
		maxTurns:              cfg.maxTurns,
		resumeID:              resumeID,
		extraArgs:             cfg.extraArgs,
	}
	if roc.cwd != "" {
		spec.cwd = roc.cwd
	}
	if roc.permission != "" {
		spec.permissionMode = roc.permission
	}
	if roc.interactivePermission {
		spec.interactivePermission = true
	}
	if len(roc.extraArgs) > 0 {
		extra := make([]string, len(spec.extraArgs)+len(roc.extraArgs))
		copy(extra, spec.extraArgs)
		copy(extra[len(spec.extraArgs):], roc.extraArgs)
		spec.extraArgs = extra
	}
	return spec
}

// compileRuntimeHooks turns hookSpec entries into runtime.Hook slice.
func compileRuntimeHooks(hooks []hookSpec) []runtime.Hook {
	out := make([]runtime.Hook, 0, len(hooks))
	for _, h := range hooks {
		out = append(out, runtime.Hook{
			Stage:   stageCCToRuntime(h.Stage),
			Matcher: h.Matcher,
			Fn:      wrapHookFunc(h.Fn),
		})
	}
	return out
}
