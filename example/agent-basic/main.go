// Package main 是 agent 框架的入门 example：
//
//   - agent.New + Tools + System
//   - agent.NewConversation + a.Runner(conv)
//   - 两个自定义 agent.Tool（add 算术 + write_note 副作用）
//   - 一个 agent.Use 中间件拒绝危险副作用（AbortWithDeny）
//   - runner.Send 拿 iter.Seq[Event] 订阅 EventTextDelta / EventPreToolUse / EventTurnEnd
//   - 第二轮 runner.Send 复用同一 Conversation
//   - runner.Resend 演示流中 error 回退（conv.Truncate 砍掉 errored partial）
//
// 默认走 providertest.New() mock；useMock=false + OPENAI_API_KEY 时切真实 OpenAI。
//
// 运行:
//
//	go run ./example/agent-basic
package main

import (
	"context"
	"fmt"
	"iter"
	"log"
	"os"
	"strings"

	goai "github.com/sashabaranov/go-openai"

	"github.com/cago-frame/agents/agent"
	"github.com/cago-frame/agents/provider"
	"github.com/cago-frame/agents/provider/openai"
	"github.com/cago-frame/agents/provider/providertest"
)

const useMock = true // OPENAI_API_KEY 非空且本变量改 false 时切真实 OpenAI。

// addTool: 纯算术工具，无副作用。
type addTool struct{}

func (addTool) Name() string        { return "add" }
func (addTool) Description() string { return "把两个整数相加。" }
func (addTool) Schema() agent.Schema {
	return agent.Schema{
		Type: "object",
		Properties: map[string]*agent.Property{
			"a": {Type: "integer"},
			"b": {Type: "integer"},
		},
		Required: []string{"a", "b"},
	}
}
func (addTool) Call(_ context.Context, in map[string]any) (*agent.ToolResultBlock, error) {
	a, _ := in["a"].(float64)
	b, _ := in["b"].(float64)
	return &agent.ToolResultBlock{
		Content: []agent.ContentBlock{agent.TextBlock{Text: fmt.Sprintf("%d", int(a)+int(b))}},
	}, nil
}

// writeNoteTool: 演示副作用工具（只把内容打印到 stdout，不真的写盘，避免污染示例环境）。
type writeNoteTool struct{}

func (writeNoteTool) Name() string { return "write_note" }
func (writeNoteTool) Description() string {
	return "把一段 note 写到本地（demo 中只 print）。"
}
func (writeNoteTool) Schema() agent.Schema {
	return agent.Schema{
		Type: "object",
		Properties: map[string]*agent.Property{
			"text": {Type: "string"},
		},
		Required: []string{"text"},
	}
}
func (writeNoteTool) Call(_ context.Context, in map[string]any) (*agent.ToolResultBlock, error) {
	text, _ := in["text"].(string)
	fmt.Printf("[write_note] %s\n", text)
	return &agent.ToolResultBlock{
		Content: []agent.ContentBlock{agent.TextBlock{Text: "ok"}},
	}, nil
}

// auditWriteNote: 一个最小 ToolMiddleware，演示 AbortWithDeny。
// 任何 write_note 的 text 包含 "secret" 则被拒。
func auditWriteNote(c *agent.ToolContext) {
	text, _ := c.Input["text"].(string)
	if containsSecret(text) {
		c.AbortWithDeny("note 包含敏感字样，被 audit hook 拒绝")
		return
	}
	c.Next()
}

func containsSecret(s string) bool {
	return strings.Contains(strings.ToLower(s), "secret")
}

func main() {
	ctx := context.Background()

	prov := buildProvider()

	a := agent.New(prov,
		agent.System("你是一个简明助手，遇到算术题用 add 工具；写笔记用 write_note 工具。"),
		agent.Model("mock-basic"),
		agent.Tools(addTool{}, writeNoteTool{}),
		agent.Use("write_note", auditWriteNote),
	)

	conv := agent.NewConversation()
	runner := a.Runner(conv)
	defer func() { _ = runner.Close() }()

	// 第一轮：用 add 工具算术。
	fmt.Println("=== Turn 1: 2+3 ===")
	consume(runner.Send(ctx, "请用 add 工具计算 2+3。"))

	// 第二轮：复用同一 Conversation，触发 write_note + 演示 hook 拒绝。
	fmt.Println("\n=== Turn 2: write a note ===")
	consume(runner.Send(ctx, "请把 'hello world' 写成一条 note。"))

	// 第三轮：演示 Resend 在流中 error 后的回退（mock 这里直接喂 EventError）。
	if useMock {
		fmt.Println("\n=== Turn 3: error then resend ===")
		demoErrorResend(ctx, runner, conv)
	}

	fmt.Printf("\nfinal conv.Len() = %d\n", conv.Len())
}

func consume(events iter.Seq[agent.Event], err error) {
	if err != nil {
		log.Printf("send error: %v", err)
		return
	}
	for ev := range events {
		switch ev.Kind {
		case agent.EventTextDelta:
			fmt.Print(ev.Delta)
		case agent.EventPreToolUse:
			if ev.Tool != nil {
				fmt.Printf("\n[pre_tool_use] %s\n", ev.Tool.Name)
			}
		case agent.EventPostToolUse:
			if ev.Tool != nil {
				fmt.Printf("[post_tool_use] %s -> %v\n", ev.Tool.Name, ev.Tool.Output)
			}
		case agent.EventTurnEnd:
			usageTotal := 0
			if ev.Usage != nil {
				usageTotal = ev.Usage.TotalTokens
			}
			fmt.Printf("\n[turn_end] stop=%v usage=%d\n", ev.StopReason, usageTotal)
		case agent.EventError:
			fmt.Printf("\n[error] %v\n", ev.Error)
		}
	}
}

// demoErrorResend: 触发一次错误 → 砍 partial → Resend。
func demoErrorResend(ctx context.Context, runner *agent.Runner, conv *agent.Conversation) {
	// 先 Send 一次（mock 已经在 buildProvider 里 queue 了 error 与 retry 两个 stream）。
	consume(runner.Send(ctx, "再来一轮（mock 里这轮会先报错再恢复）。"))

	// errored partial 会留在 conv 里：砍掉它。
	if conv.Len() > 0 {
		_ = conv.Truncate(conv.Len() - 1)
	}

	// Resend 复发同一条 user message。
	events, err := runner.Resend(ctx)
	if err != nil {
		log.Printf("resend error: %v", err)
		return
	}
	for ev := range events {
		switch ev.Kind {
		case agent.EventTextDelta:
			fmt.Print(ev.Delta)
		case agent.EventTurnEnd:
			fmt.Printf("\n[turn_end after resend] stop=%v\n", ev.StopReason)
		}
	}
	fmt.Println()
}

func buildProvider() provider.Provider {
	if !useMock {
		key := os.Getenv("OPENAI_API_KEY")
		if key == "" {
			log.Fatal("OPENAI_API_KEY not set (or set useMock=true)")
		}
		return openai.NewProvider(goai.DefaultConfig(key))
	}

	mock := providertest.New()

	// Turn 1: 模型选 add(2,3) → 工具结果 5 → 模型回 "答案是 5。"
	mock.QueueStream(
		provider.StreamChunk{ToolCallDelta: &provider.ToolCallDelta{Index: 0, ID: "tc1", Name: "add"}},
		provider.StreamChunk{ToolCallDelta: &provider.ToolCallDelta{Index: 0, ArgsDelta: `{"a":2,"b":3}`}},
		provider.StreamChunk{FinishReason: provider.FinishToolCalls},
	)
	mock.QueueStream(
		provider.StreamChunk{ContentDelta: "答案是 5。"},
		provider.StreamChunk{FinishReason: provider.FinishStop},
	)

	// Turn 2: 模型先尝试 write_note("secret payload") 被 hook 拒绝，再 write_note("hello world")。
	mock.QueueStream(
		provider.StreamChunk{ToolCallDelta: &provider.ToolCallDelta{Index: 0, ID: "tc2", Name: "write_note"}},
		provider.StreamChunk{ToolCallDelta: &provider.ToolCallDelta{Index: 0, ArgsDelta: `{"text":"secret payload"}`}},
		provider.StreamChunk{FinishReason: provider.FinishToolCalls},
	)
	mock.QueueStream(
		provider.StreamChunk{ToolCallDelta: &provider.ToolCallDelta{Index: 0, ID: "tc3", Name: "write_note"}},
		provider.StreamChunk{ToolCallDelta: &provider.ToolCallDelta{Index: 0, ArgsDelta: `{"text":"hello world"}`}},
		provider.StreamChunk{FinishReason: provider.FinishToolCalls},
	)
	mock.QueueStream(
		provider.StreamChunk{ContentDelta: "已写入。"},
		provider.StreamChunk{FinishReason: provider.FinishStop},
	)

	// Turn 3: 第一次喂 Err 模拟 transient 错误；Resend 第二次正常。
	mock.QueueError(fmt.Errorf("transient upstream blip"))
	mock.QueueStream(
		provider.StreamChunk{ContentDelta: "重试成功，回到正轨。"},
		provider.StreamChunk{FinishReason: provider.FinishStop},
	)

	return mock
}
