package claudecode

import (
	"context"
	"io"
	"os"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/cago-frame/agents/cliagent/internal/runtime"
)

// 本文件负责单个 claude 子进程的整个生命周期:spawn → reader 解析 stream-json →
// per-turn demuxer → 关闭 / 清理。processManager 持有 live[threadID] 索引以及
// shutdown 编排。

// acquireLiveProcess returns an existing liveProcess (by resumeID) or spawns a new one.
func (pm *processManager) acquireLiveProcess(ctx context.Context, spec processSpec) (*liveProcess, bool, error) {
	if spec.resumeID != "" {
		pm.mu.Lock()
		if lp := pm.live[spec.resumeID]; lp != nil {
			pm.mu.Unlock()
			return lp, false, nil
		}
		pm.mu.Unlock()
	}
	lp, err := pm.spawnLiveProcess(ctx, spec)
	if err != nil {
		return nil, false, err
	}
	return lp, true, nil
}

// spawnLiveProcess starts a new claude process and begins the reader goroutine.
func (pm *processManager) spawnLiveProcess(ctx context.Context, spec processSpec) (*liveProcess, error) {
	// Map processSpec fields into runSpec for buildArgs.
	argSpec := runSpec{
		model:                 spec.model,
		cwd:                   spec.cwd,
		env:                   spec.env,
		systemPrompt:          spec.systemPrompt,
		permissionMode:        spec.permissionMode,
		interactivePermission: spec.interactivePermission,
		allowedTools:          spec.allowedTools,
		disallowedTools:       spec.disallowedTools,
		maxTurns:              spec.maxTurns,
		resumeID:              spec.resumeID,
		extraArgs:             spec.extraArgs,
		mcpConfig:             spec.mcpConfig,
		settings:              spec.settings,
	}
	binary := spec.binary
	if binary == "" {
		binary = defaultBinary
	}
	opts := procOptions{
		Binary: binary,
		Args:   buildArgs(binary, argSpec),
		Cwd:    spec.cwd,
	}
	if len(spec.env) > 0 {
		env := os.Environ()
		for k, v := range spec.env {
			env = append(env, k+"="+v)
		}
		opts.Env = env
	}

	proc, err := pm.runner.Start(ctx, opts)
	if err != nil {
		return nil, err
	}

	lp := &liveProcess{
		proc:               proc,
		stdin:              proc.Stdin(),
		events:             make(chan runtime.Event, 64),
		readyCh:            make(chan struct{}),
		closedCh:           make(chan struct{}),
		mcpRegisteredNames: spec.mcpRegisteredNames,
	}

	go func() {
		_, _ = io.Copy(io.Discard, proc.Stderr())
	}()

	//nolint:gosec // G118 — reader is session-scoped by design, not request-scoped
	go pm.runReader(lp)

	return lp, nil
}

// runReader is the long-running goroutine that parses stream-json stdout.
// It intercepts EventSessionStart to fill lp.threadID and register lp in pm.live,
// then forwards all events to lp.events.
func (pm *processManager) runReader(lp *liveProcess) {
	interceptCh := make(chan runtime.Event, 64)
	readerCtx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var initOnce sync.Once
	interceptDone := make(chan struct{})

	go func() {
		defer close(interceptDone)
		for ev := range interceptCh {
			if ev.Kind == runtime.EventSessionStart && ev.SessionID != "" && lp.threadID == "" {
				lp.threadID = ev.SessionID
				pm.mu.Lock()
				pm.live[ev.SessionID] = lp
				pm.mu.Unlock()
				initOnce.Do(func() { close(lp.readyCh) })
			}
			lp.events <- ev
		}
	}()

	terr := translateStream(readerCtx, lp.proc.Stdout(), interceptCh, lp.mcpRegisteredNames)
	if terr != nil {
		lp.exitErr.Store(terr)
	}
	close(interceptCh)
	<-interceptDone

	// Ensure readyCh is closed even if init never arrived.
	initOnce.Do(func() { close(lp.readyCh) })

	if terr != nil {
		sid := lp.threadID
		if sid != "" {
			pm.mu.Lock()
			if !lp.closing.Load() {
				delete(pm.live, sid)
				pm.deadSessions[sid] = struct{}{}
			}
			pm.mu.Unlock()
		}
		lp.events <- runtime.Event{Kind: runtime.EventError, Err: ErrProcessDead}
	}

	if werr := lp.proc.Wait(); werr != nil && lp.exitErr.Load() == nil {
		lp.exitErr.Store(werr)
	}

	if terr == nil && lp.threadID != "" {
		pm.mu.Lock()
		if !lp.closing.Load() {
			delete(pm.live, lp.threadID)
			pm.deadSessions[lp.threadID] = struct{}{}
		}
		pm.mu.Unlock()
	}

	close(lp.events)
	close(lp.closedCh)
}

// runTurnDemuxer forwards events from lp.events to out until EventDone.
// EventError is forwarded but does NOT terminate the turn — the translator
// emits [EventError, EventDone] for error_during_execution, and the demuxer
// must drain both so that runHeadless sees exactly one EventError followed by
// one EventDone, with no synthetic duplicates at higher layers.
func (pm *processManager) runTurnDemuxer(ctx context.Context, cancel context.CancelFunc, lp *liveProcess, out chan<- runtime.Event, done chan<- struct{}) {
	defer close(done)
	defer close(out)
	defer lp.turnMu.Unlock()
	defer cancel()

	canceled := false
	for ev := range lp.events {
		if !canceled {
			select {
			case out <- ev:
			case <-ctx.Done():
				canceled = true
			}
		}
		// Only EventDone terminates a turn. EventError is forwarded but the
		// turn continues to drain through the EventDone that should follow.
		if ev.Kind == runtime.EventDone {
			return
		}
	}
	// Channel closed without EventDone — process died.
	if !canceled {
		select {
		case out <- runtime.Event{Kind: runtime.EventError, Err: ErrProcessDead}:
		case <-ctx.Done():
			return
		}
		select {
		case out <- runtime.Event{Kind: runtime.EventDone, Stop: runtime.StopError}:
		case <-ctx.Done():
		}
	}
}

// markDead removes sid from live and adds it to deadSessions.
func (pm *processManager) markDead(sid string) {
	if sid == "" {
		return
	}
	pm.mu.Lock()
	delete(pm.live, sid)
	pm.deadSessions[sid] = struct{}{}
	pm.mu.Unlock()
}

// closeByThreadID gracefully shuts down a session: close stdin → grace →
// SIGTERM → SIGKILL on the underlying proc.
func (pm *processManager) closeByThreadID(_ context.Context, sid string) error {
	if sid == "" {
		return nil
	}
	pm.mu.Lock()
	lp := pm.live[sid]
	delete(pm.live, sid)
	delete(pm.deadSessions, sid)
	if lp != nil {
		lp.closing.Store(true)
	}
	pm.mu.Unlock()
	if lp == nil {
		return nil
	}

	_ = lp.stdin.Close()

	timer := time.NewTimer(pm.killGrace)
	defer timer.Stop()
	select {
	case <-lp.closedCh:
		return nil
	case <-timer.C:
	}
	_ = lp.proc.Signal(syscall.SIGTERM)
	timer2 := time.NewTimer(pm.killGrace / 2)
	defer timer2.Stop()
	select {
	case <-lp.closedCh:
		return nil
	case <-timer2.C:
	}
	return lp.proc.Kill()
}

// shutdownAll closes all live processes concurrently.
func (pm *processManager) shutdownAll(ctx context.Context) error {
	pm.mu.Lock()
	snapshot := make(map[string]*liveProcess, len(pm.live))
	for k, v := range pm.live {
		snapshot[k] = v
	}
	pm.mu.Unlock()
	if len(snapshot) == 0 {
		return nil
	}
	var wg sync.WaitGroup
	var firstErr atomic.Value
	for sid := range snapshot {
		wg.Add(1)
		go func(sid string) {
			defer wg.Done()
			if err := pm.closeByThreadID(ctx, sid); err != nil && firstErr.Load() == nil {
				firstErr.Store(err)
			}
		}(sid)
	}
	wg.Wait()
	if v := firstErr.Load(); v != nil {
		if err, ok := v.(error); ok {
			return err
		}
	}
	return nil
}

// startTurnHandle constructs a turnHandle wrapping a fresh demuxer goroutine
// for the given live process. Caller must hold lp.turnMu (released by demuxer
// on exit).
func (pm *processManager) startTurnHandle(ctx context.Context, lp *liveProcess, threadID string) *turnHandle {
	out := make(chan runtime.Event, 64)
	turnDone := make(chan struct{})
	turnCtx, turnCancel := context.WithCancel(ctx)

	go pm.runTurnDemuxer(turnCtx, turnCancel, lp, out, turnDone)

	return &turnHandle{
		Events:   out,
		Stdin:    lp.stdin,
		ThreadID: threadID,
		Wait: func() error {
			<-turnDone
			return nil
		},
		Kill: func() error {
			turnCancel()
			<-turnDone
			return nil
		},
	}
}
