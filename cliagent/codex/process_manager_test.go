package codex

import (
	"context"
	"testing"
)

// TestSpecKey_StableForSameSpec verifies the spec hash is deterministic
// across two distinct specs that share the same field values.
func TestSpecKey_StableForSameSpec(t *testing.T) {
	mk := func() runSpec {
		return runSpec{
			binary:    "/usr/bin/codex",
			cwd:       "/work",
			env:       map[string]string{"A": "1", "B": "2"},
			config:    []string{"x=1", "y=2"},
			extraArgs: []string{"--verbose"},
		}
	}
	k1, k2 := specKey(mk()), specKey(mk())
	if k1 != k2 {
		t.Fatalf("specKey not deterministic: %s vs %s", k1, k2)
	}
}

// TestSpecKey_IgnoresEnvInsertionOrder verifies the env map is sorted before hashing.
func TestSpecKey_IgnoresEnvInsertionOrder(t *testing.T) {
	a := runSpec{env: map[string]string{"A": "1", "B": "2", "C": "3"}}
	b := runSpec{env: map[string]string{"C": "3", "A": "1", "B": "2"}}
	if specKey(a) != specKey(b) {
		t.Errorf("specKey differs across env insertion order")
	}
}

// TestSpecKey_IgnoresConfigOrder verifies config slice is sorted.
func TestSpecKey_IgnoresConfigOrder(t *testing.T) {
	a := runSpec{config: []string{"x=1", "y=2", "z=3"}}
	b := runSpec{config: []string{"z=3", "x=1", "y=2"}}
	if specKey(a) != specKey(b) {
		t.Errorf("specKey differs across config slice order")
	}
}

// TestSpecKey_DifferentSpecsDiffer verifies meaningful diffs change the hash.
func TestSpecKey_DifferentSpecsDiffer(t *testing.T) {
	base := runSpec{binary: "/a", cwd: "/b"}
	cases := []runSpec{
		{binary: "/different", cwd: "/b"},
		{binary: "/a", cwd: "/different"},
		{binary: "/a", cwd: "/b", env: map[string]string{"X": "1"}},
		{binary: "/a", cwd: "/b", config: []string{"k=v"}},
		{binary: "/a", cwd: "/b", extraArgs: []string{"--x"}},
		{binary: "/a", cwd: "/b", mcpServerName: "n"},
		{binary: "/a", cwd: "/b", mcpURL: "http://x"},
		{binary: "/a", cwd: "/b", mcpTokenEnv: "T"},
	}
	baseKey := specKey(base)
	for i, c := range cases {
		if specKey(c) == baseKey {
			t.Errorf("case %d: spec hashes equal base unexpectedly", i)
		}
	}
}

// TestSpecKey_IgnoresPromptAndResume confirms the cache key only depends on
// process-startup-relevant fields (binary/cwd/env/config/extraArgs/mcp), not
// per-turn fields like prompt or resumeID.
func TestSpecKey_IgnoresPromptAndResume(t *testing.T) {
	a := runSpec{binary: "/a", prompt: "hello", resumeID: "tid-1"}
	b := runSpec{binary: "/a", prompt: "world", resumeID: "tid-2"}
	if specKey(a) != specKey(b) {
		t.Errorf("specKey unexpectedly varies with prompt/resumeID")
	}
}

// TestNewProcessManager_EmptyPool confirms initial state.
func TestNewProcessManager_EmptyPool(t *testing.T) {
	pm := newProcessManager(backendCfg{})
	if pm == nil {
		t.Fatal("newProcessManager returned nil")
	}
	if len(pm.bySpecKey) != 0 || len(pm.byThreadID) != 0 || len(pm.all) != 0 {
		t.Errorf("expected empty pool maps, got bySpecKey=%d byThreadID=%d all=%d",
			len(pm.bySpecKey), len(pm.byThreadID), len(pm.all))
	}
	if pm.closed {
		t.Error("pool should not start closed")
	}
	if pm.spawnCount.Load() != 0 {
		t.Error("spawnCount should start at 0")
	}
	if pm.bridge != nil {
		t.Error("mcp bridge should be lazy-init, not eager")
	}
}

// TestAcquireMCPBridge_LazyAndIdempotent verifies the bridge is created once.
func TestAcquireMCPBridge_LazyAndIdempotent(t *testing.T) {
	pm := newProcessManager(backendCfg{})

	b1, err := pm.acquireMCPBridge()
	if err != nil {
		t.Fatalf("acquireMCPBridge: %v", err)
	}
	if b1 == nil {
		t.Fatal("expected non-nil bridge")
	}
	b2, err := pm.acquireMCPBridge()
	if err != nil {
		t.Fatalf("second acquireMCPBridge: %v", err)
	}
	if b1 != b2 {
		t.Error("acquireMCPBridge returned a different bridge on second call")
	}
}

// TestShutdownAll_EmptyPool is a no-op.
func TestShutdownAll_EmptyPool(t *testing.T) {
	pm := newProcessManager(backendCfg{})
	if err := pm.shutdownAll(context.Background()); err != nil {
		t.Errorf("shutdownAll on empty pool: %v", err)
	}
	if !pm.closed {
		t.Error("expected closed after shutdownAll")
	}
}

// TestShutdownAll_Idempotent verifies a second shutdown is safe.
func TestShutdownAll_Idempotent(t *testing.T) {
	pm := newProcessManager(backendCfg{})
	_ = pm.shutdownAll(context.Background())
	if err := pm.shutdownAll(context.Background()); err != nil {
		t.Errorf("second shutdownAll: %v", err)
	}
}

// TODO(phase5/task10b): exercise pool reuse / spawn semantics end-to-end. The
// previous TestProcessReuseAcrossRuns / TestProcessSpawnsForDifferentSpec drove
// agent.Backend.Start with a *fakeRunner. Equivalent at the Runner level needs
// a fake app-server child that responds to JSON-RPC initialize/initialized, and
// a way to inject it via processManager.spawn. Tracked separately from the
// Phase 5 cutover gate.
