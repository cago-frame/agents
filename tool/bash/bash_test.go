package bash_test

import (
	"context"
	"runtime"
	"strings"
	"testing"
	"time"

	agent "github.com/cago-frame/agents/agent"
	"github.com/cago-frame/agents/tool"
	"github.com/cago-frame/agents/tool/bash"
)

// callBash calls the bash tool with a command (and optional timeout in seconds).
// On success it returns (text, false, nil). On IsError it returns (text, true, nil).
// On genuine Go error it returns ("", false, err).
func callBash(t *testing.T, tl tool.Tool, command string, timeout float64) (string, bool, error) {
	t.Helper()
	in := map[string]any{"command": command}
	if timeout > 0 {
		in["timeout"] = timeout
	}
	res, err := tl.Call(context.Background(), in)
	if err != nil {
		return "", false, err
	}
	text := res.Content[0].(agent.TextBlock).Text
	return text, res.IsError, nil
}

func TestBashEcho(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("posix-only test")
	}
	out, isErr, err := callBash(t, bash.New(), "echo hello", 0)
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	if isErr {
		t.Fatalf("unexpected IsError, got %q", out)
	}
	if !strings.Contains(out, "hello") {
		t.Fatalf("got %q", out)
	}
}

func TestBashNoOutput(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("posix-only")
	}
	out, isErr, err := callBash(t, bash.New(), "true", 0)
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	if isErr {
		t.Fatalf("unexpected IsError, got %q", out)
	}
	if out != "(no output)" {
		t.Fatalf("got %q", out)
	}
}

func TestBashNonZeroExit(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("posix-only")
	}
	// Non-zero exit must return IsError=true, nil Go error.
	out, isErr, err := callBash(t, bash.New(), "echo bad >&2; exit 7", 0)
	if err != nil {
		t.Fatalf("expected nil Go error, got: %v", err)
	}
	if !isErr {
		t.Fatalf("expected IsError=true, got false; out=%q", out)
	}
	if !strings.Contains(out, "bad") || !strings.Contains(out, "Command exited with code 7") {
		t.Fatalf("missing context in: %q", out)
	}
}

func TestBashTimeout(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("posix-only")
	}
	start := time.Now()
	// Timeout is a genuine Go error (context cancellation).
	_, _, err := callBash(t, bash.New(), "sleep 5", 0.2)
	dur := time.Since(start)
	if err == nil {
		t.Fatalf("expected timeout Go error")
	}
	if !strings.Contains(err.Error(), "Command timed out") {
		t.Fatalf("wrong err: %v", err)
	}
	if dur > 2*time.Second {
		t.Fatalf("did not actually timeout: %v", dur)
	}
}

func TestBashCwd(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("posix-only")
	}
	dir := t.TempDir()
	tl := bash.New(bash.Cwd(dir))
	out, isErr, err := callBash(t, tl, "pwd", 0)
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	if isErr {
		t.Fatalf("unexpected IsError, got %q", out)
	}
	got := strings.TrimSpace(out)
	// pwd may resolve to /private/tmp/... vs /tmp/... on macOS. Compare last segment.
	if !strings.HasSuffix(got, dir) && !strings.HasSuffix(got, dir[len("/private"):]) {
		t.Fatalf("expected pwd in %q, got %q", dir, got)
	}
}

func TestBashTruncatedAddsNotice(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("posix-only")
	}
	tl := bash.New(bash.MaxBytes(50))
	out, isErr, err := callBash(t, tl, "for i in $(seq 1 200); do echo line-$i; done", 0)
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	if isErr {
		t.Fatalf("unexpected IsError, got %q", out)
	}
	if !strings.Contains(out, "Full output: ") {
		t.Fatalf("expected truncation notice, got %q", out)
	}
}
