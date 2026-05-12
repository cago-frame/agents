package agent_test

import (
	"context"
	"encoding/json"
	"testing"

	agent "github.com/cago-frame/agents/agent"
	"github.com/cago-frame/agents/provider"
	"github.com/cago-frame/agents/provider/providertest"
)

type captureTool struct{ got map[string]any }

func (t *captureTool) Name() string         { return "Capture" }
func (t *captureTool) Description() string  { return "" }
func (t *captureTool) Schema() agent.Schema { return agent.Schema{Type: "object"} }
func (t *captureTool) Call(_ context.Context, in map[string]any) (*agent.ToolResultBlock, error) {
	t.got = in
	return &agent.ToolResultBlock{Content: []agent.ContentBlock{agent.TextBlock{Text: "ok"}}}, nil
}

func TestHook_PreToolUse_ModifiedInput(t *testing.T) {
	args, _ := json.Marshal(map[string]any{"text": "original"})
	prov := providertest.New().
		QueueStream(
			provider.StreamChunk{ToolCallDelta: &provider.ToolCallDelta{Index: 0, ID: "t1", Name: "Capture", ArgsDelta: string(args)}},
			provider.StreamChunk{FinishReason: provider.FinishToolCalls},
		).
		QueueStream(
			provider.StreamChunk{ContentDelta: "fin"},
			provider.StreamChunk{FinishReason: provider.FinishStop},
		)

	captured := &captureTool{}
	a := agent.New(prov,
		agent.Tools(captured),
		agent.Use("Capture", func(c *agent.ToolContext) {
			c.Input = map[string]any{"text": "MODIFIED"}
			c.Next()
		}),
	)
	conv := agent.NewConversation()
	r := a.Runner(conv)
	defer func() { _ = r.Close() }()

	events, _ := r.Send(context.Background(), "go")
	for range events {
	}
	if captured.got["text"] != "MODIFIED" {
		t.Fatalf("expected modified input, got %v", captured.got)
	}
}

func TestHook_PostToolUse_ModifiedOutput(t *testing.T) {
	args, _ := json.Marshal(map[string]any{"text": "x"})
	prov := providertest.New().
		QueueStream(
			provider.StreamChunk{ToolCallDelta: &provider.ToolCallDelta{Index: 0, ID: "t1", Name: "Capture", ArgsDelta: string(args)}},
			provider.StreamChunk{FinishReason: provider.FinishToolCalls},
		).
		QueueStream(
			provider.StreamChunk{ContentDelta: "fin"},
			provider.StreamChunk{FinishReason: provider.FinishStop},
		)

	a := agent.New(prov,
		agent.Tools(&captureTool{}),
		agent.Use("Capture", func(c *agent.ToolContext) {
			c.Next()
			c.Output = &agent.ToolResultBlock{
				ToolUseID: c.ToolUseID,
				Content:   []agent.ContentBlock{agent.TextBlock{Text: "REWRITTEN"}},
			}
		}),
	)
	conv := agent.NewConversation()
	r := a.Runner(conv)
	defer func() { _ = r.Close() }()

	events, _ := r.Send(context.Background(), "go")
	for range events {
	}
	// tool result message is conv[2]
	last := conv.Messages()[2]
	tr := last.Content[0].(agent.ToolResultBlock)
	if textOfBlocks(tr.Content) != "REWRITTEN" {
		t.Fatalf("expected rewritten output, got %q", textOfBlocks(tr.Content))
	}
}
