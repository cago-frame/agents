package codex

import (
	"testing"

	"github.com/cago-frame/agents/cliagent/internal/runtime"
)

func TestToolSourceForItem(t *testing.T) {
	cases := []struct {
		name string
		item *appThreadItem
		want runtime.ToolSource
	}{
		{"nil", nil, runtime.ToolSourceUnknown},
		{"commandExecution", &appThreadItem{Type: "commandExecution"}, runtime.ToolSourceBuiltin},
		{"fileChange", &appThreadItem{Type: "fileChange"}, runtime.ToolSourceBuiltin},
		{"mcpToolCall", &appThreadItem{Type: "mcpToolCall", Server: "s", Tool: "t"}, runtime.ToolSourceMCP},
		{"dynamicToolCall", &appThreadItem{Type: "dynamicToolCall", Tool: "t"}, runtime.ToolSourceBuiltin},
		{"collabAgentToolCall", &appThreadItem{Type: "collabAgentToolCall", Tool: "spawnAgent"}, runtime.ToolSourceBuiltin},
		{"agentMessage", &appThreadItem{Type: "agentMessage"}, runtime.ToolSourceUnknown},
		{"unknown", &appThreadItem{Type: "weird"}, runtime.ToolSourceUnknown},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := toolSourceForItem(tc.item)
			if got != tc.want {
				t.Errorf("toolSourceForItem(%+v) = %q, want %q", tc.item, got, tc.want)
			}
		})
	}
}
