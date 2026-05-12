package claudecode

import (
	"context"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/cago-frame/agents/cliagent/internal/runtime"
)

// processSpec holds all parameters needed to start or resume a claude process.
type processSpec struct {
	binary                string // claude CLI binary path; defaults to "claude" if empty
	prompt                string
	model                 string
	cwd                   string
	env                   map[string]string
	systemPrompt          string
	permissionMode        PermissionMode
	interactivePermission bool
	allowedTools          []string
	disallowedTools       []string
	maxTurns              int
	resumeID              string // == State.ThreadID from previous turn
	extraArgs             []string
	mcpConfig             string              // JSON blob for --mcp-config (populated by bridge wiring)
	mcpRegisteredNames    map[string]struct{} // tool names registered via mcp.Bridge; passed to translateStream
	settings              string              // JSON blob for --settings (populated by hook bridge)
}

// turnHandle is the per-turn handle returned by processManager.acquireOrSpawn
// or processManager.startNextTurn.
type turnHandle struct {
	Events   <-chan runtime.Event
	Stdin    io.Writer // write user frames for subsequent turns
	ThreadID string    // session_id assigned after init
	Wait     func() error
	Kill     func() error
}

// processManager manages live and dead claude processes for a Runner.
// It also owns the lazily-allocated mcp bridge / hook bridge.
//
// Per-process spawn / reader / cleanup lives in process_lifecycle.go.
type processManager struct {
	cfg *backendCfg

	mu           sync.Mutex
	live         map[string]*liveProcess
	deadSessions map[string]struct{}

	runner      processRunner
	killGrace   time.Duration
	initTimeout time.Duration

	// Bridges owned by Runner — allocated lazily when first run is started.
	bridge *mcpBridge
	hookBr *hookBridge
}

func newProcessManager(cfg *backendCfg) *processManager {
	return &processManager{
		cfg:          cfg,
		live:         make(map[string]*liveProcess),
		deadSessions: make(map[string]struct{}),
		runner:       cfg.runner,
		killGrace:    cfg.killGrace,
		initTimeout:  cfg.initTimeout,
	}
}

// startTurn launches the per-turn reader goroutine, attaching it to stream
// and registering steer / follow-up / permission bridges.
func (pm *processManager) startTurn(ctx context.Context, sess *runtime.Session, stream *runtime.Stream, prompt string, roc runOptCfg) error {
	steerCh := make(chan string, 16)
	permCh := make(chan permissionResponse, 16)
	stream.SetInjectFn(func(ctx context.Context, kind, payload string) error {
		switch kind {
		case "user":
			select {
			case steerCh <- payload:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		case "follow_up":
			return sess.EnqueueFollowUp(payload)
		}
		return nil
	})
	stream.SetPermissionFn(func(permCtx context.Context, requestID string, allow bool) error {
		select {
		case permCh <- permissionResponse{id: requestID, allow: allow}:
			return nil
		case <-permCtx.Done():
			return permCtx.Err()
		}
	})
	go pm.runHeadless(ctx, pm.cfg, sess, stream, prompt, roc, steerCh, permCh)
	return nil
}

// acquireMCPBridge lazy-starts the shared mcp bridge.
func (pm *processManager) acquireMCPBridge(ctx context.Context, cfg *backendCfg) (*mcpBridge, error) {
	pm.mu.Lock()
	if pm.bridge == nil {
		pm.bridge = newMCPBridge()
	}
	bridge := pm.bridge
	pm.mu.Unlock()
	if err := bridge.ensureRegistered(cfg.agentTools); err != nil {
		return nil, err
	}
	if _, err := bridge.start(ctx); err != nil {
		return nil, err
	}
	return bridge, nil
}

// acquireHookBridge lazy-starts the shared CLI hook bridge.
func (pm *processManager) acquireHookBridge(hooks []runtime.Hook) (*hookBridge, error) {
	pm.mu.Lock()
	if pm.hookBr != nil {
		hb := pm.hookBr
		pm.mu.Unlock()
		return hb, nil
	}
	pm.mu.Unlock()
	hb, err := newHookBridge(hooks)
	if err != nil {
		return nil, err
	}
	if err := hb.start(); err != nil {
		return nil, err
	}
	pm.mu.Lock()
	pm.hookBr = hb
	pm.mu.Unlock()
	return hb, nil
}

// Close closes everything owned by the manager: live processes, bridges.
func (pm *processManager) Close(ctx context.Context) error {
	var firstErr error
	if err := pm.shutdownAll(ctx); err != nil {
		firstErr = err
	}
	pm.mu.Lock()
	bridge := pm.bridge
	hookBr := pm.hookBr
	pm.mu.Unlock()
	if bridge != nil {
		if err := bridge.shutdown(ctx); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	if hookBr != nil {
		if err := hookBr.shutdown(ctx); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// acquireOrSpawn finds an existing live process by spec.resumeID or spawns a new one,
// writes the initial user frame, and returns a turnHandle for this turn.
func (pm *processManager) acquireOrSpawn(ctx context.Context, spec processSpec) (*turnHandle, error) {
	// Fail fast on known dead sessions.
	if spec.resumeID != "" {
		pm.mu.Lock()
		_, dead := pm.deadSessions[spec.resumeID]
		pm.mu.Unlock()
		if dead {
			return nil, ErrSessionDead
		}
	}

	lp, justSpawned, err := pm.acquireLiveProcess(ctx, spec)
	if err != nil {
		return nil, err
	}

	// Acquire turn lock (demuxer releases it on exit).
	lp.turnMu.Lock()

	// Write user frame. claude CLI emits system.init only after the first frame.
	data, err := userFrameBytes(spec.prompt)
	if err != nil {
		lp.turnMu.Unlock()
		return nil, err
	}
	if _, werr := lp.stdin.Write(data); werr != nil {
		lp.turnMu.Unlock()
		if justSpawned {
			_ = lp.proc.Kill()
		} else {
			pm.markDead(lp.threadID)
		}
		return nil, fmt.Errorf("claudecode: write user frame: %w", ErrProcessDead)
	}

	// Wait for init on first spawn.
	if justSpawned {
		select {
		case <-lp.readyCh:
			if lp.threadID == "" {
				lp.turnMu.Unlock()
				_ = lp.proc.Kill()
				return nil, ErrProcessDead
			}
		case <-time.After(pm.initTimeout):
			lp.turnMu.Unlock()
			_ = lp.proc.Kill()
			return nil, ErrInitTimeout
		case <-ctx.Done():
			lp.turnMu.Unlock()
			_ = lp.proc.Kill()
			return nil, ctx.Err()
		}
	}

	return pm.startTurnHandle(ctx, lp, lp.threadID), nil
}

// startNextTurn writes a user frame on an existing live process and returns a new
// turn handle. The live process must already be in pm.live[threadID]; calling on a
// dead session returns ErrSessionDead. This mirrors the "already live" branch of
// acquireOrSpawn without re-doing init-wait.
func (pm *processManager) startNextTurn(ctx context.Context, threadID, prompt string) (*turnHandle, error) {
	if threadID == "" {
		return nil, ErrSessionDead
	}
	pm.mu.Lock()
	_, dead := pm.deadSessions[threadID]
	lp := pm.live[threadID]
	pm.mu.Unlock()

	if dead || lp == nil {
		return nil, ErrSessionDead
	}

	// Acquire the turn lock so only one demuxer runs at a time.
	lp.turnMu.Lock()

	if err := writeUserFrame(lp.stdin, prompt); err != nil {
		lp.turnMu.Unlock()
		pm.markDead(threadID)
		return nil, fmt.Errorf("claudecode: startNextTurn: %w", ErrProcessDead)
	}

	return pm.startTurnHandle(ctx, lp, threadID), nil
}
