package agent_test

import (
	"errors"
	"testing"

	agent "github.com/cago-frame/agents/agent"
)

func TestErrors_AreSentinels(t *testing.T) {
	cases := []error{
		agent.ErrConversationBusy,
		agent.ErrRunnerClosed,
		agent.ErrSteerNoActiveTurn,
		agent.ErrIndexOutOfRange,
		agent.ErrCannotResend,
		agent.ErrToolNotFound,
		agent.ErrIncompatibleOption,
	}
	for _, e := range cases {
		if e == nil {
			t.Fatalf("error sentinel is nil")
		}
		if e.Error() == "" {
			t.Fatalf("error has empty message")
		}
	}
}

func TestProviderError_Unwrap(t *testing.T) {
	cause := errors.New("network down")
	pe := &agent.ProviderError{Op: "chat_stream", Status: 502, Cause: cause}
	if !errors.Is(pe, cause) {
		t.Fatalf("ProviderError should unwrap to cause")
	}
}

func TestToolError_Unwrap(t *testing.T) {
	cause := errors.New("invalid json")
	te := &agent.ToolError{ToolName: "Bash", ToolUseID: "tu_1", Cause: cause}
	if !errors.Is(te, cause) {
		t.Fatalf("ToolError should unwrap to cause")
	}
}

func TestProviderError_Error(t *testing.T) {
	cause := errors.New("network timeout")

	e1 := &agent.ProviderError{Op: "chat", Status: 429, Cause: cause}
	if got, want := e1.Error(), "agent: provider chat failed (status=429): network timeout"; got != want {
		t.Errorf("Error() with status = %q, want %q", got, want)
	}

	e2 := &agent.ProviderError{Op: "chat", Status: 0, Cause: cause}
	if got, want := e2.Error(), "agent: provider chat failed: network timeout"; got != want {
		t.Errorf("Error() without status = %q, want %q", got, want)
	}
}

func TestToolError_Error(t *testing.T) {
	cause := errors.New("not found")
	e := &agent.ToolError{ToolName: "ReadFile", ToolUseID: "tu_1", Cause: cause}
	if got, want := e.Error(), "agent: tool \"ReadFile\" (tu_1) failed: not found"; got != want {
		t.Errorf("ToolError.Error() = %q, want %q", got, want)
	}
}
