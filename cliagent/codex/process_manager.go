package codex

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"sort"
	"sync"
	"sync/atomic"
)

// liveClient 持有一个活跃 codex app-server 进程及其状态。
//
// 一个 liveClient 在同一时刻最多服务一个 appRun(由 occupied 守卫)。
// initialized 一次后,跨多次 acquire 复用,避免反复执行 initialize/initialized 握手。
// threadID 在第一次 thread/start 成功后绑定,之后该 liveClient 终生服务该 thread:
// 同进程上再起第二个 thread 在协议上可行,但跟 ClaudeCode 的 per-thread 进程语义对齐
// 更便于上层池化逻辑统一管理。
type liveClient struct {
	pm         *processManager
	client     *appClient
	specKey    string
	mu         sync.Mutex
	threadID   string
	occupied   bool
	dead       bool
	initOnce   sync.Once
	initErr    error
	idleStopCh chan struct{} // closed by tryOccupy to signal idle-watcher to exit
}

// processManager 是 codex backend 内部的 client 复用池。
//
// bySpecKey:同 spec(进程命令行参数)且尚未绑定 thread 的 idle client,可被新 thread/start 命中。
// byThreadID:thread 已绑定的 client,被对应 resumeID 优先命中,跨 Run 复用同一 codex 进程。
type processManager struct {
	cfg backendCfg

	mu         sync.Mutex
	bySpecKey  map[string][]*liveClient
	byThreadID map[string]*liveClient
	all        map[*liveClient]struct{}
	closed     bool

	// bridge 是惰性初始化的 mcp bridge,被 Runner 内多个 Stream 共享。
	bridgeMu sync.Mutex
	bridge   *mcpBridge

	// spawnCount 用于测试断言"实际启了几个进程"。
	spawnCount atomic.Int64
}

func newProcessManager(cfg backendCfg) *processManager {
	return &processManager{
		cfg:        cfg,
		bySpecKey:  map[string][]*liveClient{},
		byThreadID: map[string]*liveClient{},
		all:        map[*liveClient]struct{}{},
	}
}

// acquireMCPBridge lazy-initializes the shared mcp bridge.
func (pm *processManager) acquireMCPBridge() (*mcpBridge, error) {
	pm.bridgeMu.Lock()
	defer pm.bridgeMu.Unlock()
	if pm.bridge == nil {
		pm.bridge = newMCPBridge()
	}
	return pm.bridge, nil
}

// acquire 返回一个可立即用于一次 Run 的 liveClient。
//
// 选择顺序:
//  1. spec.resumeID != "" → 命中 byThreadID 的 idle client(命中即跳过 thread/start);
//  2. 否则在 bySpecKey 里挑一个 thread 未绑定的 idle client(命中后由调用方 thread/start);
//  3. 都没有就 spawn 新进程并 initialize 一次。
//
// 返回的 liveClient 已被标记 occupied;调用方必须在用完后调用 pm.release(lc, threadID)。
func (pm *processManager) acquire(ctx context.Context, spec runSpec) (*liveClient, error) {
	key := specKey(spec)

	pm.mu.Lock()
	if pm.closed {
		pm.mu.Unlock()
		return nil, errPoolClosed
	}
	if spec.resumeID != "" {
		if lc := pm.byThreadID[spec.resumeID]; lc != nil && pm.tryOccupyLocked(lc) {
			pm.mu.Unlock()
			if err := lc.ensureInitialized(ctx); err != nil {
				pm.markDead(lc)
				return nil, err
			}
			return lc, nil
		}
	}
	if list := pm.bySpecKey[key]; len(list) > 0 {
		for i, lc := range list {
			if lc.threadID != "" {
				continue
			}
			if !pm.tryOccupyLocked(lc) {
				continue
			}
			pm.bySpecKey[key] = append(list[:i], list[i+1:]...)
			pm.mu.Unlock()
			if err := lc.ensureInitialized(ctx); err != nil {
				pm.markDead(lc)
				return nil, err
			}
			return lc, nil
		}
	}
	pm.mu.Unlock()

	return pm.spawn(ctx, spec, key)
}

// release 把 lc 还回池子。threadID 非空且 lc 尚未绑定 thread 时建立 byThreadID 映射。
// drain 用来抛弃 idle 期间残留的 inbound 事件,避免污染下一轮 turn。
func (pm *processManager) release(lc *liveClient, threadID string) {
	if lc == nil {
		return
	}
	lc.mu.Lock()
	if lc.dead {
		lc.occupied = false
		lc.mu.Unlock()
		return
	}
	if lc.threadID == "" && threadID != "" {
		lc.threadID = threadID
	}
	bindThread := lc.threadID
	lc.occupied = false
	lc.mu.Unlock()

	pm.drainIncoming(lc)

	pm.mu.Lock()
	if pm.closed {
		pm.mu.Unlock()
		_ = lc.client.terminate(context.Background(), pm.cfg.killGrace)
		return
	}
	if bindThread != "" {
		pm.byThreadID[bindThread] = lc
	} else {
		pm.bySpecKey[lc.specKey] = append(pm.bySpecKey[lc.specKey], lc)
	}
	pm.mu.Unlock()

	pm.startWatcher(lc)
}

// markDead 把 lc 从所有索引中移除并 terminate;幂等。
func (pm *processManager) markDead(lc *liveClient) {
	if lc == nil {
		return
	}
	lc.mu.Lock()
	if lc.dead {
		lc.mu.Unlock()
		return
	}
	lc.dead = true
	threadID := lc.threadID
	lc.mu.Unlock()

	pm.mu.Lock()
	if threadID != "" && pm.byThreadID[threadID] == lc {
		delete(pm.byThreadID, threadID)
	}
	if list := pm.bySpecKey[lc.specKey]; len(list) > 0 {
		filtered := list[:0]
		for _, x := range list {
			if x != lc {
				filtered = append(filtered, x)
			}
		}
		pm.bySpecKey[lc.specKey] = filtered
	}
	delete(pm.all, lc)
	pm.mu.Unlock()

	_ = lc.client.terminate(context.Background(), pm.cfg.killGrace)
}

// shutdownAll 关闭池中所有 client。同时阻止后续 acquire。
func (pm *processManager) shutdownAll(ctx context.Context) error {
	pm.mu.Lock()
	pm.closed = true
	all := make([]*liveClient, 0, len(pm.all))
	for lc := range pm.all {
		all = append(all, lc)
	}
	pm.byThreadID = map[string]*liveClient{}
	pm.bySpecKey = map[string][]*liveClient{}
	pm.all = map[*liveClient]struct{}{}
	pm.mu.Unlock()

	var firstErr atomic.Value
	var wg sync.WaitGroup
	for _, lc := range all {
		wg.Add(1)
		go func(lc *liveClient) {
			defer wg.Done()
			if err := lc.client.terminate(ctx, pm.cfg.killGrace); err != nil && firstErr.Load() == nil {
				firstErr.Store(err)
			}
		}(lc)
	}
	wg.Wait()

	// Shut down the shared mcp bridge after all clients are gone.
	pm.bridgeMu.Lock()
	bridge := pm.bridge
	pm.bridge = nil
	pm.bridgeMu.Unlock()
	if bridge != nil {
		if err := bridge.shutdown(ctx); err != nil && firstErr.Load() == nil {
			firstErr.Store(err)
		}
	}

	if v := firstErr.Load(); v != nil {
		if err, ok := v.(error); ok {
			return err
		}
	}
	return nil
}

func (pm *processManager) spawn(ctx context.Context, spec runSpec, key string) (*liveClient, error) {
	pm.mu.Lock()
	if pm.closed {
		pm.mu.Unlock()
		return nil, errPoolClosed
	}
	pm.mu.Unlock()

	client, err := newAppClient(ctx, pm.cfg.appRunner, procOptions{
		Binary: spec.binary,
		Args:   buildAppServerArgs(spec),
		Cwd:    spec.cwd,
		Env:    buildEnv(spec.env),
	})
	if err != nil {
		return nil, err
	}
	pm.spawnCount.Add(1)
	lc := &liveClient{
		pm:       pm,
		client:   client,
		specKey:  key,
		occupied: true,
	}
	pm.mu.Lock()
	pm.all[lc] = struct{}{}
	pm.mu.Unlock()
	if err := lc.ensureInitialized(ctx); err != nil {
		pm.markDead(lc)
		return nil, err
	}
	return lc, nil
}

// tryOccupyLocked 调用前持有 pm.mu。返回 true 表示成功标记 occupied。
// dead 或已 occupied 的 lc 返回 false。
func (pm *processManager) tryOccupyLocked(lc *liveClient) bool {
	lc.mu.Lock()
	defer lc.mu.Unlock()
	if lc.dead || lc.occupied {
		return false
	}
	lc.occupied = true
	if lc.idleStopCh != nil {
		close(lc.idleStopCh)
		lc.idleStopCh = nil
	}
	return true
}

// drainIncoming 抛弃 idle 期间从 codex 进程到来的旧 turn 通知,避免污染下一轮。
func (pm *processManager) drainIncoming(lc *liveClient) {
	for {
		select {
		case _, ok := <-lc.client.Incoming():
			if !ok {
				return
			}
		default:
			return
		}
	}
}

// startWatcher 监控 idle 期间进程是否挂掉;挂掉就 markDead。
// 若 lc 被再次 occupy,tryOccupyLocked 会关 idleStopCh,watcher 立刻退出,
// 不再干涉新 run 的生命周期。
func (pm *processManager) startWatcher(lc *liveClient) {
	lc.mu.Lock()
	stopCh := make(chan struct{})
	lc.idleStopCh = stopCh
	lc.mu.Unlock()
	go func() {
		select {
		case <-stopCh:
			return
		case <-lc.client.Done():
			pm.markDead(lc)
		}
	}()
}

func (lc *liveClient) ensureInitialized(ctx context.Context) error {
	lc.initOnce.Do(func() {
		_, err := lc.client.Call(ctx, appMethodInitialize, map[string]any{
			"clientInfo": map[string]any{
				"name":    appClientName,
				"title":   appClientTitle,
				"version": appClientVersion,
			},
			"capabilities": map[string]any{
				"experimentalApi": true,
			},
		})
		if err != nil {
			lc.initErr = err
			return
		}
		lc.initErr = lc.client.Notify(ctx, appMethodInitialized, nil)
	})
	return lc.initErr
}

var errPoolClosed = errors.New("codex: process pool closed")

// specKey 计算复用 key。只包含影响 codex app-server 子进程命令行/环境的字段。
// model/sandbox/approval/ephemeral 等是 per-thread 参数,不影响进程,留给 thread/start 区分。
func specKey(spec runSpec) string {
	h := sha256.New()
	writeStr := func(s string) {
		_, _ = h.Write([]byte(s))
		_, _ = h.Write([]byte{0})
	}
	writeStr(spec.binary)
	writeStr(spec.cwd)

	envKeys := make([]string, 0, len(spec.env))
	for k := range spec.env {
		envKeys = append(envKeys, k)
	}
	sort.Strings(envKeys)
	for _, k := range envKeys {
		writeStr(k)
		writeStr(spec.env[k])
	}

	cfgs := append([]string(nil), spec.config...)
	sort.Strings(cfgs)
	for _, c := range cfgs {
		writeStr(c)
	}
	for _, a := range spec.extraArgs {
		writeStr(a)
	}
	writeStr(spec.mcpServerName)
	writeStr(spec.mcpURL)
	writeStr(spec.mcpTokenEnv)

	return hex.EncodeToString(h.Sum(nil))
}
