package approve_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	agent "github.com/cago-frame/agents/agent"
	"github.com/cago-frame/agents/agent/approve"
	"github.com/cago-frame/agents/provider"
	"github.com/cago-frame/agents/provider/providertest"
)

type denyCheckTool struct{}

func (denyCheckTool) Name() string         { return "Bash" }
func (denyCheckTool) Description() string  { return "would run a command if not denied" }
func (denyCheckTool) Schema() agent.Schema { return agent.Schema{Type: "object"} }
func (denyCheckTool) Call(ctx context.Context, input map[string]any) (*agent.ToolResultBlock, error) {
	return &agent.ToolResultBlock{Content: []agent.ContentBlock{agent.TextBlock{Text: "executed"}}}, nil
}

func TestApprove_Integration_Deny(t *testing.T) {
	t.Parallel()
	args, _ := json.Marshal(map[string]any{"cmd": "rm -rf /"})
	prov := providertest.New().
		QueueStream(
			provider.StreamChunk{ToolCallDelta: &provider.ToolCallDelta{
				Index: 0, ID: "tu_1", Name: "Bash", ArgsDelta: string(args),
			}},
			provider.StreamChunk{FinishReason: provider.FinishToolCalls},
		).
		QueueStream(
			provider.StreamChunk{ContentDelta: "skipped"},
			provider.StreamChunk{FinishReason: provider.FinishStop},
		)

	approver := approve.New()
	defer func() { _ = approver.Close() }()
	go func() {
		for p := range approver.Pending() {
			_ = approver.Deny(p.ID, "policy")
		}
	}()

	a := agent.New(prov,
		agent.Tools(denyCheckTool{}),
		agent.Use("Bash", approve.Hook(approver)),
	)
	conv := agent.NewConversation()
	r := a.Runner(conv)
	defer func() { _ = r.Close() }()

	events, _ := r.Send(context.Background(), "go")
	for range events {
	}

	msgs := conv.Messages()
	var sawExecuted, sawDenied bool
	for _, m := range msgs {
		for _, blk := range m.Content {
			if tr, ok := blk.(agent.ToolResultBlock); ok {
				for _, sub := range tr.Content {
					if tb, ok := sub.(agent.TextBlock); ok {
						if tb.Text == "executed" {
							sawExecuted = true
						}
						if contains(tb.Text, "policy") {
							sawDenied = true
						}
					}
				}
			}
		}
	}
	if sawExecuted {
		t.Fatal("tool ran despite Deny")
	}
	if !sawDenied {
		t.Fatal("Deny reason 'policy' not surfaced in tool result")
	}
}

func contains(s, sub string) bool {
	if len(sub) == 0 {
		return true
	}
	if len(s) < len(sub) {
		return false
	}
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

func TestApprove_Integration_ContextCancel(t *testing.T) {
	t.Parallel()
	args, _ := json.Marshal(map[string]any{"cmd": "ls"})
	prov := providertest.New().
		QueueStream(
			provider.StreamChunk{ToolCallDelta: &provider.ToolCallDelta{
				Index: 0, ID: "tu_1", Name: "Bash", ArgsDelta: string(args),
			}},
			provider.StreamChunk{FinishReason: provider.FinishToolCalls},
		)

	approver := approve.New()
	defer func() { _ = approver.Close() }()

	a := agent.New(prov,
		agent.Tools(denyCheckTool{}),
		agent.Use("Bash", approve.Hook(approver)),
	)
	conv := agent.NewConversation()
	r := a.Runner(conv)
	defer func() { _ = r.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	events, _ := r.Send(ctx, "go")
	for range events {
	}
}
