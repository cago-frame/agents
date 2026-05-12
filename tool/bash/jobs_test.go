package bash

import (
	"context"
	"runtime"
	"testing"
	"time"
)

func TestJobManager_StopAll_Empty(t *testing.T) {
	m := NewJobManager()
	if err := m.StopAll(); err != nil {
		t.Fatalf("StopAll on empty: %v", err)
	}
}

func TestJobManager_StopAll_KillsAllRegistered(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("posix-only")
	}
	m := NewJobManager()

	// Spawn two real background jobs via the bash tool so Kill() has a real process to terminate.
	tl := New(Jobs(m))
	for range 2 {
		_, err := tl.Call(context.Background(), map[string]any{"command": "sleep 30", "run_in_background": true})
		if err != nil {
			t.Fatalf("spawn background job: %v", err)
		}
	}

	// Wait until both are confirmed running.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		allRunning := true
		for _, snap := range m.List() {
			if snap.Status != StatusRunning {
				allRunning = false
				break
			}
		}
		if allRunning && len(m.List()) == 2 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	if err := m.StopAll(); err != nil {
		t.Fatalf("StopAll: %v", err)
	}

	// Give processes a moment to be killed.
	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		anyRunning := false
		for _, snap := range m.List() {
			if snap.Status == StatusRunning {
				anyRunning = true
				break
			}
		}
		if !anyRunning {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	for _, snap := range m.List() {
		if snap.Status == StatusRunning {
			t.Errorf("job %s still running after StopAll", snap.ID)
		}
	}
}
