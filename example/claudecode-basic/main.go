// Package main 展示如何用 claudecode.New 驱动本地 Claude Code，并演示两件事：
//
//  1. 把 subagent.NewTool 注册到 claudecode.Tools —— Claude CLI 通过 MCP bridge
//     调到这个分派工具，委派给 Go 侧子 agent（这里子 agent 用 providertest.Mock
//     替身，不需要真 provider）。
//  2. 用 Claude Code 原生 hook（PostToolUse）在 CLI 内置工具（Bash）执行完后，
//     通过 claudecode.PostToolUse 的 Go 回调注入 additionalContext —— 下一轮
//     LLM 会看到这条补充内容。inbox 模拟"外部排队的用户补充"。
//
// 前置：机器上装好 claude-code CLI 并已登录 (`claude login`)。
//
// 跑法：
//
//	go run ./example/claudecode-basic
//
// 不进 CI。
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"sync/atomic"
	"syscall"

	"github.com/cago-frame/agents/agent"
	"github.com/cago-frame/agents/cliagent/claudecode"
	"github.com/cago-frame/agents/provider"
	"github.com/cago-frame/agents/provider/providertest"
	"github.com/cago-frame/agents/tool"
	"github.com/cago-frame/agents/tool/subagent"
)

// timeTool 是一个最小 agent.Tool 实现：返回固定的 unix 时间戳。
func timeTool() tool.Tool {
	return &tool.RawTool{
		NameStr:   "now",
		DescStr:   "current unix time",
		SchemaVal: agent.Schema{Type: "object"},
		Handler: func(_ context.Context, _ map[string]any) (*agent.ToolResultBlock, error) {
			b, _ := json.Marshal(map[string]any{"unix": 1700000000})
			return tool.TextResult(string(b)), nil
		},
	}
}

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	cwd, _ := os.Getwd()

	// ── sub-agent：父 CLI 通过 MCP bridge 调到 sub_agent 工具 → Go 侧子 agent 处理 ──
	child := buildChildAgent()
	subTool := subagent.NewTool(
		"sub_agent",
		"把任务委派给一个专长子 agent",
		[]subagent.Entry{
			{Type: "explore", Description: "在仓库里搜代码 / 梳理结构", Agent: child},
		},
	)

	// ── inbox：外部向 Claude CLI 注入 user 补充的队列 ──
	// PostToolUse hook 在 CLI 执行完 Bash 后，把 inbox 里的文本以 additionalContext
	// 注入到下一轮 LLM 上下文。
	inbox := make(chan string, 16)
	var bashSeen atomic.Int32

	r := claudecode.New(
		claudecode.Cwd(cwd),
		claudecode.Model("claude-sonnet-4-6"),
		claudecode.Tools(timeTool(), subTool),
		claudecode.PostToolUse("Bash", func(_ context.Context, _ claudecode.HookInput) (*claudecode.HookOutput, error) {
			bashSeen.Add(1)
			var parts []string
			for {
				select {
				case s := <-inbox:
					parts = append(parts, s)
				default:
					if len(parts) == 0 {
						return nil, nil
					}
					text := "用户补充：\n- " + joinLines(parts)
					fmt.Printf("\n[hook PostToolUse/Bash] 注入 additionalContext: %q\n", text)
					return &claudecode.HookOutput{AdditionalContext: text}, nil
				}
			}
		}),
	)
	defer func() { _ = r.Close(context.Background()) }()

	sess := r.Session()
	defer func() { _ = sess.Close(context.Background()) }()

	// 起一个 goroutine，在进入 Run 前先排一条补充，模拟"外部在干别的事突然想到要补一句"
	inbox <- "顺便把刚才的时间戳也写成人类可读格式"

	// Round 1
	stream, err := sess.Stream(ctx, "先跑 `date +%s` 确认当前时间，然后调用 `now` 工具做对比，"+
		"最后用 sub_agent(explore) 找仓库里叫 helloWorld 的函数。")
	if err != nil {
		log.Fatal(err)
	}
	printEvents(stream)

	// Round 2 (uses --resume via session metadata)
	stream, err = sess.Stream(ctx, "刚才的时间是多少？有没有收到我补充的要求？")
	if err != nil {
		log.Fatal(err)
	}
	printEvents(stream)

	res, err := stream.Result()
	if err != nil {
		fmt.Printf("[result err] %v\n", err)
	} else if res != nil {
		fmt.Printf("\n[result] stop=%s text_len=%d\n", res.Stop, len(res.Text))
	}
	fmt.Printf("PostToolUse/Bash hook 触发 %d 次\n", bashSeen.Load())
}

// buildChildAgent 构造一个用 mock provider 的子 agent（agent），只为演示装配结构。
func buildChildAgent() *agent.Agent {
	mock := providertest.New()
	mock.QueueStream(
		provider.StreamChunk{ContentDelta: "找到了 helloWorld 函数，位于 example/simple/main.go:15（mock 回复）。"},
		provider.StreamChunk{FinishReason: provider.FinishStop},
	)
	return agent.New(mock,
		agent.Model("mock-child"),
		agent.System("你是代码探索专家。"),
	)
}

func joinLines(xs []string) string { return strings.Join(xs, "\n- ") }

func formatToolResponse(raw json.RawMessage) string {
	if len(raw) == 0 {
		return "<empty>"
	}
	const maxResponseLen = 200
	s := string(raw)
	s = strings.ReplaceAll(s, "\n", "\\n")
	if len(s) > maxResponseLen {
		s = s[:maxResponseLen] + "...(truncated)"
	}
	return s
}

func printEvents(stream *claudecode.Stream) {
	for stream.Next() {
		ev := stream.Event()
		switch ev.Kind {
		case claudecode.EventSessionStart:
			fmt.Printf("[meta] session_id=%s\n", ev.SessionID)
		case claudecode.EventTextDelta:
			fmt.Print(ev.Text)
		case claudecode.EventPreToolUse:
			if ev.Tool != nil {
				fmt.Printf("\n[tool-start] %s(%s)\n", ev.Tool.Name, ev.Tool.Input)
			}
		case claudecode.EventPostToolUse:
			if ev.Tool != nil {
				fmt.Printf("[tool-end] %s err=%v result=%s\n", ev.Tool.Name, ev.Tool.Err, formatToolResponse(ev.Tool.Response))
			}
		case claudecode.EventDone:
			fmt.Printf("\n[done] stop=%s\n", ev.Stop)
		case claudecode.EventError:
			fmt.Printf("\n[error] %v\n", ev.Err)
		}
	}
}
