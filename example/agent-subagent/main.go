// Package main demonstrates subagent.NewTool — wrapping a child *agent.Agent as
// a normal tool callable by a parent agent.
//
// Key points illustrated:
//   - Child agent gets its own OnEvent observer; its internal events do NOT
//     bubble to the parent's event stream.
//   - Parent sees only EventPreToolUse / EventPostToolUse around the dispatch
//     call, then EventTextDelta / EventTurnEnd for its own final reply.
//   - Both parent and child use providertest.New() mocks — no network required.
//
// Run:
//
//	go run ./example/agent-subagent
package main

import (
	"context"
	"fmt"
	"iter"
	"log"

	"github.com/cago-frame/agents/agent"
	"github.com/cago-frame/agents/provider"
	"github.com/cago-frame/agents/provider/providertest"
	"github.com/cago-frame/agents/tool/subagent"
)

func main() {
	ctx := context.Background()

	// ── child mock ──────────────────────────────────────────────────────────
	childMock := providertest.New()
	// Child's single turn: stream a short text reply.
	childMock.QueueStream(
		provider.StreamChunk{ContentDelta: "[child] "},
		provider.StreamChunk{ContentDelta: "找到 helloWorld 在 src/greetings.go:12."},
		provider.StreamChunk{FinishReason: provider.FinishStop},
	)

	// ── child agent ─────────────────────────────────────────────────────────
	childAgent := agent.New(childMock,
		agent.System("你是一个只读代码探索助手。"),
		agent.Model("mock-child"),
		// Child's own observer — prints [child-stream] lines.
		// These events are private to the child; they never reach the parent.
		agent.OnEvent(agent.OnlyKinds(agent.EventTextDelta), func(_ context.Context, ev agent.Event) {
			fmt.Printf("[child-stream] %s\n", ev.Delta)
		}),
		agent.OnEvent(agent.OnlyKinds(agent.EventTurnEnd), func(_ context.Context, ev agent.Event) {
			fmt.Printf("[child-stream] turn ended stop=%v\n", ev.StopReason)
		}),
	)

	// ── dispatch tool ────────────────────────────────────────────────────────
	dispatchTool := subagent.NewTool(
		"subagent",
		"将任务委派给子 agent。",
		[]subagent.Entry{
			{
				Type:        "explore_repo",
				Description: "探索代码仓库、查找符号或文件位置。",
				Agent:       childAgent,
			},
		},
	)

	// ── parent mock ──────────────────────────────────────────────────────────
	parentMock := providertest.New()
	// Turn 1a: parent decides to call subagent.
	parentMock.QueueStream(
		provider.StreamChunk{ToolCallDelta: &provider.ToolCallDelta{Index: 0, ID: "tc-sub-1", Name: "subagent"}},
		provider.StreamChunk{ToolCallDelta: &provider.ToolCallDelta{Index: 0, ArgsDelta: `{"type":"explore_repo","prompt":"在项目里找 helloWorld 函数在哪里定义"}`}},
		provider.StreamChunk{FinishReason: provider.FinishToolCalls},
	)
	// Turn 1b: parent summarizes the child's output.
	parentMock.QueueStream(
		provider.StreamChunk{ContentDelta: "[parent] 子 agent 已找到 helloWorld，位于 src/greetings.go."},
		provider.StreamChunk{FinishReason: provider.FinishStop},
	)

	// ── parent agent ─────────────────────────────────────────────────────────
	parentAgent := agent.New(parentMock,
		agent.System("你是一个编码助手，可以通过 subagent 工具委派任务。"),
		agent.Model("mock-parent"),
		agent.Tools(dispatchTool),
	)

	conv := agent.NewConversation()
	runner := parentAgent.Runner(conv)
	defer func() { _ = runner.Close() }()

	fmt.Println("=== agent-subagent demo ===")
	consume(runner.Send(ctx, "请帮我找到 helloWorld 函数的位置。"))
	fmt.Printf("\nconv.Len() = %d\n", conv.Len())
}

func consume(events iter.Seq[agent.Event], err error) {
	if err != nil {
		log.Printf("send error: %v", err)
		return
	}
	for ev := range events {
		switch ev.Kind {
		case agent.EventTextDelta:
			fmt.Printf("[parent-text] %s\n", ev.Delta)
		case agent.EventPreToolUse:
			if ev.Tool != nil {
				fmt.Printf("[parent-pre]  tool=%s id=%s\n", ev.Tool.Name, ev.Tool.ToolUseID)
			}
		case agent.EventPostToolUse:
			if ev.Tool != nil {
				n := 0
				if ev.Tool.Output != nil {
					n = len(ev.Tool.Output.Content)
				}
				fmt.Printf("[parent-post] tool=%s output_blocks=%d\n", ev.Tool.Name, n)
			}
		case agent.EventTurnEnd:
			fmt.Printf("[parent-end]  stop=%v\n", ev.StopReason)
		case agent.EventError:
			fmt.Printf("[parent-err]  %v\n", ev.Error)
		}
	}
}
