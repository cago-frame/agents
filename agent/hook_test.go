package agent_test

import (
	"context"
	"testing"

	agent "github.com/cago-frame/agents/agent"
)

func TestHook_DecisionConstants(t *testing.T) {
	// Decision constants are still used by UserPromptOutput. Tool dispatch
	// has moved to ToolContext methods (Next / AbortWithDeny / AbortWithError).
	if agent.DecisionPass == agent.DecisionApprove {
		t.Fatalf("Decision values must be distinct")
	}
	if agent.DecisionPass == agent.DecisionDeny {
		t.Fatalf("Decision values must be distinct")
	}
	if agent.DecisionApprove == agent.DecisionDeny {
		t.Fatalf("Decision values must be distinct")
	}
}

func TestHook_ToolMiddleware_TypeCheck(t *testing.T) {
	// Compile-time check that ToolMiddleware has the expected shape, and
	// that AbortWithDeny / Next produce the documented behavior.
	var fn agent.ToolMiddleware = func(c *agent.ToolContext) {
		if c.ToolName != "Bash" {
			c.Next()
			return
		}
		c.AbortWithDeny("no")
	}

	td := &agent.ToolDispatcher{
		Tools:      []agent.Tool{noopTool{name: "Bash"}, noopTool{name: "Other"}},
		Middleware: []agent.ToolHookEntry[agent.ToolMiddleware]{{Matcher: ".*", Fn: fn}},
	}

	bash := td.Run(context.Background(), agent.DispatchInput{ToolName: "Bash", ToolUseID: "tu_1"})
	if bash.Output == nil || !bash.Output.IsError {
		t.Fatalf("Bash should be denied, got %+v", bash.Output)
	}
	other := td.Run(context.Background(), agent.DispatchInput{ToolName: "Other", ToolUseID: "tu_2"})
	if other.Output == nil || other.Output.IsError {
		t.Fatalf("Other should pass through, got %+v", other.Output)
	}
}

func TestHook_LifecycleHooks_Compile(t *testing.T) {
	var _ agent.ToolMiddleware = func(c *agent.ToolContext) {}
	var _ agent.UserPromptHook = func(ctx context.Context, in *agent.UserPromptInput) (*agent.UserPromptOutput, error) { return nil, nil }
	var _ agent.TurnEndHook = func(ctx context.Context, in *agent.TurnEndInput) (*agent.TurnEndOutput, error) { return nil, nil }
}

func TestHook_ToolContext_ConvAccessor(t *testing.T) {
	// Compile-time assertion: ToolContext.Conv is ConversationReader.
	var c agent.ToolContext
	_ = (agent.ConversationReader)(c.Conv)
}

type noopTool struct{ name string }

func (n noopTool) Name() string         { return n.name }
func (n noopTool) Description() string  { return "noop" }
func (n noopTool) Schema() agent.Schema { return agent.Schema{Type: "object"} }
func (n noopTool) Call(_ context.Context, _ map[string]any) (*agent.ToolResultBlock, error) {
	return &agent.ToolResultBlock{Content: []agent.ContentBlock{agent.TextBlock{Text: "ok"}}}, nil
}
