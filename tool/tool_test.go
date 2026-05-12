package tool

import (
	"context"
	"testing"

	agent "github.com/cago-frame/agents/agent"
)

func TestRawTool_ImplementsAgentTool(t *testing.T) {
	var _ agent.Tool = (*RawTool)(nil)
	var _ agent.SerialTool = (*RawTool)(nil)
}

func TestRawTool_CallReturnsToolResultBlock(t *testing.T) {
	rt := &RawTool{
		NameStr:   "echo",
		DescStr:   "echo input",
		SchemaVal: agent.Schema{Type: "object"},
		IsSerial:  true,
		Handler: func(ctx context.Context, in map[string]any) (*agent.ToolResultBlock, error) {
			return TextResult(in["msg"].(string)), nil
		},
	}
	if rt.Name() != "echo" || !rt.Serial() {
		t.Fatalf("metadata: name=%q serial=%v", rt.Name(), rt.Serial())
	}
	out, err := rt.Call(context.Background(), map[string]any{"msg": "hi"})
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	if out.Content[0].(agent.TextBlock).Text != "hi" {
		t.Fatalf("got %#v", out.Content[0])
	}
}
