package agent_test

import (
	"context"
	"testing"

	agent "github.com/cago-frame/agents/agent"
)

type fakeTool struct{ called bool }

func (t *fakeTool) Name() string        { return "Echo" }
func (t *fakeTool) Description() string { return "echo input back" }
func (t *fakeTool) Schema() agent.Schema {
	return agent.Schema{Type: "object", Properties: map[string]*agent.Property{"text": {Type: "string"}}}
}
func (t *fakeTool) Call(ctx context.Context, in map[string]any) (*agent.ToolResultBlock, error) {
	t.called = true
	return &agent.ToolResultBlock{Content: []agent.ContentBlock{agent.TextBlock{Text: in["text"].(string)}}}, nil
}

func TestTool_InterfaceShape(t *testing.T) {
	var tool agent.Tool = &fakeTool{}
	if tool.Name() != "Echo" {
		t.Fatalf("name")
	}
	out, err := tool.Call(context.Background(), map[string]any{"text": "hi"})
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	if got := out.Content[0].(agent.TextBlock).Text; got != "hi" {
		t.Fatalf("got %q", got)
	}
}
