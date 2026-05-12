package bash_test

import (
	"context"
	"runtime"
	"strings"
	"testing"
	"time"

	agent "github.com/cago-frame/agents/agent"
	"github.com/cago-frame/agents/tool/bash"
)

func runBg(t *testing.T, jm *bash.JobManager, command string) string {
	t.Helper()
	tl := bash.New(bash.Jobs(jm))
	res, err := tl.Call(context.Background(), map[string]any{"command": command, "run_in_background": true})
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	if res.IsError {
		t.Fatalf("unexpected IsError, content=%v", res.Content)
	}
	return res.Content[0].(agent.TextBlock).Text
}

func extractShellID(t *testing.T, msg string) string {
	t.Helper()
	const tag = `shell_id="`
	i := strings.Index(msg, tag)
	if i < 0 {
		t.Fatalf("no shell_id in: %q", msg)
	}
	rest := msg[i+len(tag):]
	end := strings.IndexByte(rest, '"')
	if end < 0 {
		t.Fatalf("malformed shell_id in: %q", msg)
	}
	return rest[:end]
}

func TestBashBackgroundLifecycle(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("posix-only")
	}
	jm := bash.NewJobManager()
	msg := runBg(t, jm, "for i in 1 2 3; do echo line-$i; sleep 0.05; done")
	if !strings.Contains(msg, "running in background") {
		t.Fatalf("missing start notice: %q", msg)
	}
	id := extractShellID(t, msg)

	job, ok := jm.Get(id)
	if !ok {
		t.Fatalf("job not registered")
	}

	out := bash.NewOutput(jm)
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		s := job.Snapshot()
		if s.Status != bash.StatusRunning {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	res, err := out.Call(context.Background(), map[string]any{"shell_id": id})
	if err != nil {
		t.Fatalf("bash_output: %v", err)
	}
	if res.IsError {
		t.Fatalf("unexpected IsError, content=%v", res.Content)
	}
	body := res.Content[0].(agent.TextBlock).Text
	if !strings.Contains(body, "line-1") || !strings.Contains(body, "line-3") {
		t.Fatalf("missing lines, got %q", body)
	}
	if !strings.Contains(body, "status=completed") {
		t.Fatalf("expected completed status, got %q", body)
	}
}

func TestBashBackgroundIncrementalCursor(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("posix-only")
	}
	jm := bash.NewJobManager()
	msg := runBg(t, jm, "echo first; sleep 0.3; echo second")
	id := extractShellID(t, msg)
	out := bash.NewOutput(jm)

	// 第一次读：抓到 first
	time.Sleep(100 * time.Millisecond)
	r1, err := out.Call(context.Background(), map[string]any{"shell_id": id})
	if err != nil {
		t.Fatalf("first bash_output: %v", err)
	}
	body1 := r1.Content[0].(agent.TextBlock).Text
	if !strings.Contains(body1, "first") {
		t.Fatalf("first read missed 'first': %q", body1)
	}

	// 等到 second 也输出后再读：incremental 不应再返回 first
	job, _ := jm.Get(id)
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if job.Snapshot().Status != bash.StatusRunning {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	r2, err := out.Call(context.Background(), map[string]any{"shell_id": id})
	if err != nil {
		t.Fatalf("second bash_output: %v", err)
	}
	body2 := r2.Content[0].(agent.TextBlock).Text
	if strings.Contains(body2, "first") {
		t.Fatalf("incremental should not include 'first': %q", body2)
	}
	if !strings.Contains(body2, "second") {
		t.Fatalf("expected 'second' in incremental: %q", body2)
	}
}

func TestBashBackgroundResetReturnsFull(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("posix-only")
	}
	jm := bash.NewJobManager()
	msg := runBg(t, jm, "echo only")
	id := extractShellID(t, msg)
	job, _ := jm.Get(id)
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if job.Snapshot().Status != bash.StatusRunning {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	out := bash.NewOutput(jm)
	_, _ = out.Call(context.Background(), map[string]any{"shell_id": id}) // drain incremental
	r, err := out.Call(context.Background(), map[string]any{"shell_id": id, "reset": true})
	if err != nil {
		t.Fatalf("bash_output reset: %v", err)
	}
	body := r.Content[0].(agent.TextBlock).Text
	if !strings.Contains(body, "only") {
		t.Fatalf("reset should re-return full output: %q", body)
	}
}

func TestKillShellTerminates(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("posix-only")
	}
	jm := bash.NewJobManager()
	msg := runBg(t, jm, "sleep 30")
	id := extractShellID(t, msg)
	kill := bash.NewKill(jm)
	res, err := kill.Call(context.Background(), map[string]any{"shell_id": id})
	if err != nil {
		t.Fatalf("kill: %v", err)
	}
	if res.IsError {
		t.Fatalf("unexpected IsError, content=%v", res.Content)
	}
	body := res.Content[0].(agent.TextBlock).Text
	if !strings.Contains(body, "killed") {
		t.Fatalf("expected killed status, got %q", body)
	}
}

func TestBashOutputUnknownShell(t *testing.T) {
	jm := bash.NewJobManager()
	out := bash.NewOutput(jm)
	res, err := out.Call(context.Background(), map[string]any{"shell_id": "nope"})
	if err != nil {
		t.Fatalf("expected nil Go error, got: %v", err)
	}
	if !res.IsError {
		t.Fatalf("expected IsError=true for unknown shell_id")
	}
	msg := res.Content[0].(agent.TextBlock).Text
	if !strings.Contains(msg, "unknown shell_id") {
		t.Fatalf("expected unknown id message, got %q", msg)
	}
}

func TestKillShellUnknownID(t *testing.T) {
	jm := bash.NewJobManager()
	kill := bash.NewKill(jm)
	res, err := kill.Call(context.Background(), map[string]any{"shell_id": "does-not-exist"})
	if err != nil {
		t.Fatalf("expected nil Go error, got: %v", err)
	}
	if !res.IsError {
		t.Fatalf("expected IsError=true for unknown shell_id")
	}
	msg := res.Content[0].(agent.TextBlock).Text
	if !strings.Contains(msg, "unknown shell_id") {
		t.Fatalf("expected unknown id message, got %q", msg)
	}
}

func TestBashBackgroundNoJobManager(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("posix-only")
	}
	// Without Jobs(), run_in_background=true falls back to foreground (old behavior
	// was to ignore the flag). New behavior: return IsError if no job manager.
	tl := bash.New() // no Jobs()
	res, err := tl.Call(context.Background(), map[string]any{"command": "echo hi", "run_in_background": true})
	if err != nil {
		t.Fatalf("unexpected Go error: %v", err)
	}
	if !res.IsError {
		t.Fatalf("expected IsError=true when run_in_background=true but no JobManager configured")
	}
}
