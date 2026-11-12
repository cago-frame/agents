//go:build windows

package bash_test

import (
	"context"
	"os/exec"
	"strings"
	"testing"
	"time"

	agent "github.com/cago-frame/agents/agent"
	"github.com/cago-frame/agents/tool/bash"
)

// countImage returns how many processes with the given image name are running.
func countImage(t *testing.T, image string) int {
	t.Helper()
	out, err := exec.Command("tasklist", "/FI", "IMAGENAME eq "+image, "/NH").Output() //nolint:gosec // test-controlled image name
	if err != nil {
		t.Fatalf("tasklist: %v", err)
	}
	return strings.Count(strings.ToUpper(string(out)), strings.ToUpper(image))
}

// Running inside the job object must not change an uncanceled command's result.
func TestBashRunsChildCommandWindows(t *testing.T) {
	res, err := bash.New().Call(context.Background(), map[string]any{"command": "ping -n 1 127.0.0.1 && echo done-marker"})
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	if res.IsError {
		t.Fatalf("unexpected IsError: %+v", res.Content)
	}
	text := res.Content[0].(agent.TextBlock).Text
	if !strings.Contains(text, "done-marker") {
		t.Fatalf("output missing marker: %q", text)
	}
}

// cmd.exe waits for its child; canceling must end the whole tree (not just cmd.exe,
// which would leave ping running and holding the output pipe) and return promptly.
func TestBashCancelKillsProcessTreeWindows(t *testing.T) {
	before := countImage(t, "PING.EXE")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	time.AfterFunc(500*time.Millisecond, cancel)
	start := time.Now()
	_, err := bash.New().Call(ctx, map[string]any{"command": "ping -n 30 127.0.0.1"})
	dur := time.Since(start)

	if err == nil || !strings.Contains(err.Error(), "Command aborted") {
		t.Fatalf("expected aborted Go error, got %v", err)
	}
	if dur > 5*time.Second {
		t.Fatalf("returned after %v; want within seconds of the 500ms cancel", dur)
	}

	deadline := time.Now().Add(3 * time.Second)
	for countImage(t, "PING.EXE") > before {
		if time.Now().After(deadline) {
			t.Fatalf("ping.exe still running after cancel (before=%d, now=%d)", before, countImage(t, "PING.EXE"))
		}
		time.Sleep(100 * time.Millisecond)
	}
}
