package clihook_test

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/cago-frame/agents/cliagent/claudecode/internal/clihook"
)

func TestServerStartShutdown(t *testing.T) {
	s := clihook.NewServer()
	if err := s.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() { _ = s.Shutdown(context.Background()) }()

	if s.SocketPath() == "" {
		t.Fatalf("expected non-empty socket path after Start")
	}
	if _, err := os.Stat(s.SocketPath()); err != nil {
		t.Fatalf("socket file missing: %v", err)
	}
}

func TestServerInvokeHook(t *testing.T) {
	called := make(chan clihook.Input, 1)
	s := clihook.NewServer()
	id := s.AddHook(clihook.Entry{
		Stage: clihook.PreToolUse,
		Fn: func(_ context.Context, in clihook.Input) (*clihook.Output, error) {
			called <- in
			return clihook.DenyTool("nope"), nil
		},
	})
	if err := s.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() { _ = s.Shutdown(context.Background()) }()

	cmd := s.HookCommand(id)
	if !strings.Contains(cmd, "--unix-socket") {
		t.Fatalf("expected curl unix-socket command, got %q", cmd)
	}

	out, err := postUnix(t, s.SocketPath(), id, []byte(`{"tool_name":"Bash"}`))
	if err != nil {
		t.Fatalf("postUnix: %v", err)
	}
	if !strings.Contains(string(out), `"decision":"block"`) {
		t.Fatalf("expected block decision, got %s", out)
	}

	select {
	case in := <-called:
		if in.ToolName != "Bash" {
			t.Fatalf("unexpected ToolName: %q", in.ToolName)
		}
	case <-time.After(time.Second):
		t.Fatalf("hook never invoked")
	}
}
