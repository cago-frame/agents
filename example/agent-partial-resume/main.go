// Package main 演示 agent 框架在多种"流式中断 / 干预"场景下的行为：
//
//  1. 用户在输出途中 Cancel，已经流出的文本作为 PartialCancelled 留在 conv，
//     usage 也保留下来；下一次 Send 时框架默认把它带回 LLM，模型可以续写。
//  2. provider 流中途报错，已经流出的文本作为 PartialErrored 留在 conv，
//     错误本身只通过 EventError 上报、不进 conv，下一次 Send 同样把已写
//     文本带回 LLM。
//  3. 用户在 turn 进行中追加消息（Steer）—— 框架把它排队、在下一个 safe
//     point（tool dispatch 之后 / no-tool 收尾时 auto-continue）以 user
//     message 注入；turn 真终结后 Steer 返回 ErrSteerNoActiveTurn，caller
//     用普通 Send 兜底。
//  4. 思考块 + 工具调用混合流：thinking deltas 累计到 ThinkingBlock、
//     ToolCallDelta 实时镜像到 ToolUseBlock.RawArgs；中途 Cancel 时这两
//     种部分态都保留在 conv，UI 可以照常渲染。
//  5. 自动重试：开 agent.Retry(...) option 后，transient 错误（5xx / 429
//     / 网络超时等）触发 EventRetry → backoff sleep → 同一 turn 内开新
//     ChatStream，已经流出的字节作为 errored partial 自动带回 LLM 续写。
//     UI 通过 EventRetry 上的 Attempt / Delay / Cause 渲染"重试中"指示器。
//  6. Token limit：FinishLength 翻译为 StopTokenLimit + PartialTokenLimit；
//     UI 可以挂"continue"按钮，下一轮默认带回 LLM 让它接着写。
//  7. ctx Deadline 与显式 Cancel 区分：deadline 过期落 StopTimeout +
//     PartialTimeout，显式 Cancel 落 StopCancelled + PartialCancelled，
//     UI 显示不同提示文案。
//  8. Hook 错误可观测：UserPromptSubmit / TurnEnd / Pre/PostToolUse hook
//     抛错都通过 EventError 顺出，Error 字段包成 *HookError 带 Stage/Tool。
//
// 全程走 providertest mock；脚本化了 chunk 序列以便把这几种边缘行为压成
// 可重复的 demo。
//
// 运行:
//
//	go run ./example/agent-partial-resume
package main

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/cago-frame/agents/agent"
	"github.com/cago-frame/agents/provider"
	"github.com/cago-frame/agents/provider/providertest"
)

func main() {
	ctx := context.Background()

	fmt.Println("== scenario 1: Cancel mid-stream → next Send 带上已输出内容 ==")
	runCancelResume(ctx)

	fmt.Println("\n== scenario 2: stream error mid-stream → next Send 带上已输出内容 ==")
	runErrorResume(ctx)

	fmt.Println("\n== scenario 3: Steer + ErrSteerNoActiveTurn 兜底 ==")
	runSteer(ctx)

	fmt.Println("\n== scenario 4: thinking + tool 部分态 (Cancel 中断也能渲染) ==")
	runThinkingAndToolPartial(ctx)

	fmt.Println("\n== scenario 5: 自动重试 + EventRetry UI 指示 ==")
	runAutoRetry(ctx)

	fmt.Println("\n== scenario 6: token limit (FinishLength → StopTokenLimit) ==")
	runTokenLimit(ctx)

	fmt.Println("\n== scenario 7: ctx deadline vs Cancel 区分 ==")
	runDeadlineTimeout(ctx)

	fmt.Println("\n== scenario 8: hook 错误通过 EventError 顺出 ==")
	runHookError(ctx)
}

// ---------------- scenario 1: cancel ----------------

func runCancelResume(ctx context.Context) {
	prov := providertest.New().
		// turn 1：慢流 26 个字符，等用户在第 16 个字符处 Cancel。
		QueueStreamFunc(slowStreamWithUsage("Once upon a time in a small village", 30*time.Millisecond,
			&provider.Usage{PromptTokens: 12, CompletionTokens: 5, TotalTokens: 17})).
		// turn 2：续写。
		QueueStream(
			provider.StreamChunk{ContentDelta: ", a curious child found a brass key."},
			provider.StreamChunk{FinishReason: provider.FinishStop},
		)

	a := agent.New(prov, agent.System("你是讲故事的人。"))
	conv := agent.NewConversation()
	r := a.Runner(conv)
	defer func() { _ = r.Close() }()

	// turn 1：开流，跑到一半 user 主动 Cancel。
	fmt.Print("user: 给我讲个故事\nassistant: ")
	events, _ := r.Send(ctx, "给我讲个故事")
	canceled := false
	charsRead := 0
	for ev := range events {
		switch ev.Kind {
		case agent.EventTextDelta:
			fmt.Print(ev.Delta)
			charsRead += len(ev.Delta)
			if !canceled && charsRead >= 16 {
				canceled = true
				_ = r.Cancel("user pressed Esc")
			}
		case agent.EventCancelled:
			fmt.Printf("\n[canceled: reason=%q]\n", ev.PartialReason)
		}
	}

	last, _ := conv.MessageAt(conv.Len() - 1)
	fmt.Printf("[partial saved in conv: PartialReason=%q text=%q usage=%s]\n",
		last.PartialReason, textOf(last.Content), formatUsage(last.Usage))

	// turn 2：续写。框架默认把 PartialCancelled 带回 LLM。
	fmt.Print("user: 继续\nassistant: ")
	events, _ = r.Send(ctx, "继续")
	for ev := range events {
		if ev.Kind == agent.EventTextDelta {
			fmt.Print(ev.Delta)
		}
	}
	fmt.Println()

	dumpLLMPrompt("turn-2 prompt sent to provider", prov.Received()[1])
}

// ---------------- scenario 2: provider error ----------------

func runErrorResume(ctx context.Context) {
	boom := errors.New("simulated provider failure")

	prov := providertest.New().
		// turn 1：流前几个字符后 Err。
		QueueStream(
			provider.StreamChunk{ContentDelta: "In a galaxy "},
			provider.StreamChunk{ContentDelta: "far, far"},
			provider.StreamChunk{Err: boom},
		).
		// turn 2：续写。
		QueueStream(
			provider.StreamChunk{ContentDelta: " away, a fleet drifted home."},
			provider.StreamChunk{FinishReason: provider.FinishStop},
		)

	a := agent.New(prov, agent.System("你是讲故事的人。"))
	conv := agent.NewConversation()
	r := a.Runner(conv)
	defer func() { _ = r.Close() }()

	fmt.Print("user: 讲个太空故事\nassistant: ")
	events, _ := r.Send(ctx, "讲个太空故事")
	for ev := range events {
		switch ev.Kind {
		case agent.EventTextDelta:
			fmt.Print(ev.Delta)
		case agent.EventError:
			fmt.Printf("\n[error event (only in stream, NOT in conv): %v]\n", ev.Error)
		}
	}

	last, _ := conv.MessageAt(conv.Len() - 1)
	fmt.Printf("[partial saved in conv: PartialReason=%q text=%q]\n",
		last.PartialReason, textOf(last.Content))

	fmt.Print("user: 接着讲\nassistant: ")
	events, _ = r.Send(ctx, "接着讲")
	for ev := range events {
		if ev.Kind == agent.EventTextDelta {
			fmt.Print(ev.Delta)
		}
	}
	fmt.Println()

	dumpLLMPrompt("turn-2 prompt sent to provider", prov.Received()[1])
}

// ---------------- scenario 3: steer + no-active-turn fallback ----------------

func runSteer(ctx context.Context) {
	// turn 1：先慢慢吐第一段，等 user Steer 后再吐第二段。
	steerInjected := make(chan struct{}, 1)

	prov := providertest.New().
		QueueStreamFunc(func(ctx context.Context) <-chan provider.StreamChunk {
			ch := make(chan provider.StreamChunk, 8)
			go func() {
				defer close(ch)
				for _, r := range "Hello, here is a long answer" {
					select {
					case <-ctx.Done():
						return
					case ch <- provider.StreamChunk{ContentDelta: string(r)}:
					}
					time.Sleep(20 * time.Millisecond)
				}
				ch <- provider.StreamChunk{FinishReason: provider.FinishStop}
			}()
			return ch
		}).
		// turn 2：因为 turn 1 是 no-tool 结束 + 有 pending steer，runner 会
		// auto-continue 一轮，把队列里的 "be brief" 当 user 注入再调一次 LLM。
		QueueStream(
			provider.StreamChunk{ContentDelta: "Brief: hi."},
			provider.StreamChunk{FinishReason: provider.FinishStop},
		).
		// turn 3：演示完 ErrSteerNoActiveTurn 之后的 Send("another nudge")。
		QueueStream(
			provider.StreamChunk{ContentDelta: "noted."},
			provider.StreamChunk{FinishReason: provider.FinishStop},
		)

	a := agent.New(prov, agent.System("回答用户。"))
	conv := agent.NewConversation()
	r := a.Runner(conv)
	defer func() { _ = r.Close() }()

	fmt.Print("user: 帮我答一个问题\nassistant: ")
	events, _ := r.Send(ctx, "帮我答一个问题")
	steered := false
	chars := 0
	for ev := range events {
		if ev.Kind == agent.EventTextDelta {
			fmt.Print(ev.Delta)
			chars += len(ev.Delta)
			if !steered && chars >= 10 {
				steered = true
				if err := r.Steer(ctx, "be brief"); err != nil {
					if errors.Is(err, agent.ErrSteerNoActiveTurn) {
						fmt.Printf("\n[steer skipped: turn ended → fall back to Send]\n")
					} else {
						fmt.Printf("\n[steer error: %v]\n", err)
					}
				} else {
					steerInjected <- struct{}{}
				}
			}
		}
	}
	fmt.Println()

	// 关键：steer 在 turn 1 流式中被接受。即便 turn 1 的 LLM 流以 FinishStop
	// 收尾（没有 tool 调用），runner 也会 auto-continue —— drainSteer 拿到
	// "be brief" 当 user 注入，再起一轮 LLM call（turn 2 的 mock）。所以你
	// 看到的输出是两段 assistant 文本紧挨着出现："Hello, here is a long
	// answer" 后面直接是 turn 2 的 "Brief: hi."。
	if !steered {
		fmt.Println("[steer never injected — would only happen if stream is too short]")
	}

	// 现在 turn 已经彻底退出（auto-continue 也走完）。再调 Steer 演示
	// ErrSteerNoActiveTurn —— 这次 turn 真的结束了，框架拒绝。
	if err := r.Steer(ctx, "another nudge"); err != nil {
		if errors.Is(err, agent.ErrSteerNoActiveTurn) {
			fmt.Println("[expected: Steer after turn ended → ErrSteerNoActiveTurn]")
			// 兜底：直接当普通 Send 起新一轮。
			fmt.Print("user: another nudge\nassistant: ")
			events2, _ := r.Send(ctx, "another nudge")
			for ev := range events2 {
				if ev.Kind == agent.EventTextDelta {
					fmt.Print(ev.Delta)
				}
			}
			fmt.Println()
		} else {
			fmt.Printf("[unexpected steer err: %v]\n", err)
		}
	}

	dumpLLMPrompt("turn-2 prompt sent to provider", prov.Received()[1])
	_ = steerInjected
}

// ---------------- scenario 4: thinking + tool partial ----------------

func runThinkingAndToolPartial(ctx context.Context) {
	// turn 1：先来一段 thinking deltas，然后开始一个 tool call 的 args
	// 流式（JSON 拆成两半），第二半到一半时 user 主动 Cancel —— 验证
	// thinking + 半成品 ToolUseBlock 都被保留在 partial 里。
	prov := providertest.New().QueueStreamFunc(func(ctx context.Context) <-chan provider.StreamChunk {
		ch := make(chan provider.StreamChunk, 16)
		go func() {
			defer close(ch)
			send := func(c provider.StreamChunk) bool {
				select {
				case <-ctx.Done():
					return false
				case ch <- c:
				}
				time.Sleep(20 * time.Millisecond)
				return true
			}
			// thinking 拆 3 份。
			for _, t := range []string{"先看一下用户问题…", "需要调用 weather 工具，", "city=Beijing。"} {
				if !send(provider.StreamChunk{ThinkingDelta: &provider.ThinkingDelta{Text: t}}) {
					return
				}
			}
			// thinking 末尾带 signature。
			if !send(provider.StreamChunk{ThinkingDelta: &provider.ThinkingDelta{Signature: "sig-xyz"}}) {
				return
			}
			// 然后开始 tool args 流（注意第一段不是合法 JSON，第二段才能拼合）。
			if !send(provider.StreamChunk{ToolCallDelta: &provider.ToolCallDelta{
				Index: 0, ID: "tu_1", Name: "weather", ArgsDelta: `{"city":"Beij`,
			}}) {
				return
			}
			if !send(provider.StreamChunk{ToolCallDelta: &provider.ToolCallDelta{
				Index: 0, ArgsDelta: `ing"}`,
			}}) {
				return
			}
			// 永远不会发 FinishReason —— 等 user Cancel 把流截掉。
		}()
		return ch
	})

	a := agent.New(prov, agent.System("先思考再调工具。"))
	conv := agent.NewConversation()
	r := a.Runner(conv)
	defer func() { _ = r.Close() }()

	fmt.Print("user: 北京今天天气如何\n")
	events, _ := r.Send(ctx, "北京今天天气如何")

	// 我们要在第二段 args delta 抵达后立刻 Cancel——这样 RawArgs 已经拼到完整
	// JSON 但 Input 还来不及 parse（partial）。
	argChunks := 0
	for ev := range events {
		switch ev.Kind {
		case agent.EventThinkingDelta:
			fmt.Printf("[thinking_delta] %s\n", ev.Delta)
		case agent.EventToolDelta:
			argChunks++
			fmt.Printf("[tool_delta(args)] tool=%s id=%s delta=%s\n",
				ev.Tool.Name, ev.Tool.ToolUseID, ev.Delta)
			if argChunks == 2 {
				_ = r.Cancel("UI cut off, demo done")
			}
		case agent.EventCancelled:
			fmt.Printf("[canceled: %s]\n", ev.PartialReason)
		}
	}

	// 检查 partial：thinking + tool_use(RawArgs) 都在 conv 里，UI 可渲染。
	last, _ := conv.MessageAt(conv.Len() - 1)
	fmt.Printf("[partial PartialReason=%q]\n", last.PartialReason)
	for i, b := range last.Content {
		switch v := b.(type) {
		case agent.ThinkingBlock:
			fmt.Printf("  Content[%d] thinking text=%q signature=%q\n", i, v.Text, v.Signature)
		case agent.TextBlock:
			fmt.Printf("  Content[%d] text     %q\n", i, v.Text)
		case agent.ToolUseBlock:
			parsed := "<nil — args never finalized>"
			if v.Input != nil {
				parsed = fmt.Sprintf("%v", v.Input)
			}
			fmt.Printf("  Content[%d] tool_use id=%s name=%s RawArgs=%s Input(parsed)=%s\n",
				i, v.ID, v.Name, v.RawArgs, parsed)
		default:
			fmt.Printf("  Content[%d] %T\n", i, b)
		}
	}

	// 最后给一个例证：因为 Input 仍然 nil，下一轮 BuildRequest 默认会 *跳过*
	// 这条 tool_use（避免向 LLM 发 malformed tool_call without paired result），
	// 但 thinking + 任何 finalized text 仍然会带上。
}

// ---------------- scenario 5: auto-retry ----------------

func runAutoRetry(ctx context.Context) {
	transient := &provider.ProviderError{StatusCode: 503, Err: errors.New("upstream busy")}
	prov := providertest.New().
		// turn 1：流前几个字符就 503，触发框架自动重试。
		QueueStream(
			provider.StreamChunk{ContentDelta: "Once "},
			provider.StreamChunk{ContentDelta: "upon a"},
			provider.StreamChunk{Err: transient},
		).
		// retry 1：服务恢复，续写。
		QueueStream(
			provider.StreamChunk{ContentDelta: " time, in a kingdom..."},
			provider.StreamChunk{FinishReason: provider.FinishStop},
		)

	a := agent.New(prov,
		agent.System("讲故事。"),
		agent.Retry(agent.RetryPolicy{
			MaxAttempts:  3,
			InitialDelay: 50 * time.Millisecond,
			MaxDelay:     2 * time.Second,
		}),
	)
	conv := agent.NewConversation()
	r := a.Runner(conv)
	defer func() { _ = r.Close() }()

	fmt.Print("user: 讲个故事\nassistant: ")
	events, _ := r.Send(ctx, "讲个故事")
	for ev := range events {
		switch ev.Kind {
		case agent.EventTextDelta:
			fmt.Print(ev.Delta)
		case agent.EventRetry:
			// UI 上这里就是渲染"Retrying… (attempt N, ~Xms)"指示器的地方。
			fmt.Printf("\n[retry] attempt=%d delay=%s cause=%v\n  resuming: ",
				ev.Retry.Attempt, ev.Retry.Delay, ev.Retry.Cause)
		case agent.EventError:
			fmt.Printf("\n[fatal error] %v\n", ev.Error)
		}
	}
	fmt.Println()

	// 看一下 turn 上下文：第一次 attempt 留下的 errored partial 已经在 turn-2
	// prompt 里被回灌给 LLM 当 historical context，模型从中续写。
	turn2 := prov.Received()[1]
	dumpLLMPrompt("retry attempt prompt sent to provider", turn2)
}

// ---------------- scenario 6: token limit ----------------

func runTokenLimit(ctx context.Context) {
	prov := providertest.New().
		// turn 1：模型流式输出后以 FinishLength 收尾（output cap 触顶）。
		QueueStream(
			provider.StreamChunk{ContentDelta: "The first three points are: (1) "},
			provider.StreamChunk{ContentDelta: "use TLS, (2) rotate keys, (3) ..."},
			provider.StreamChunk{FinishReason: provider.FinishLength,
				Usage: &provider.Usage{PromptTokens: 20, CompletionTokens: 256, TotalTokens: 276}},
		).
		// turn 2：用户发"continue"，模型接着写。
		QueueStream(
			provider.StreamChunk{ContentDelta: "audit logs daily."},
			provider.StreamChunk{FinishReason: provider.FinishStop},
		)

	a := agent.New(prov, agent.System("回答问题。"))
	conv := agent.NewConversation()
	r := a.Runner(conv)
	defer func() { _ = r.Close() }()

	fmt.Print("user: 列出网络安全要点\nassistant: ")
	events, _ := r.Send(ctx, "列出网络安全要点")
	var stop agent.StopReason
	for ev := range events {
		switch ev.Kind {
		case agent.EventTextDelta:
			fmt.Print(ev.Delta)
		case agent.EventTurnEnd:
			stop = ev.StopReason
		}
	}
	fmt.Println()

	last, _ := conv.MessageAt(conv.Len() - 1)
	// UI 看到 StopTokenLimit + PartialTokenLimit → 显示"Continue"按钮。
	fmt.Printf("[turn-end stop=%s] partial PartialReason=%q usage=%s\n",
		stop, last.PartialReason, formatUsage(last.Usage))

	// 用户点 Continue 触发普通 Send：默认 strip 不剥 PartialTokenLimit，
	// 上一轮的 truncated 文本作为 historical context 回灌给 LLM。
	fmt.Print("user: continue\nassistant: ")
	events, _ = r.Send(ctx, "continue")
	for ev := range events {
		if ev.Kind == agent.EventTextDelta {
			fmt.Print(ev.Delta)
		}
	}
	fmt.Println()

	dumpLLMPrompt("turn-2 prompt sent to provider", prov.Received()[1])
}

// ---------------- scenario 7: deadline vs cancel ----------------

func runDeadlineTimeout(ctx context.Context) {
	// 一个永远不收尾的 stream，等 ctx 截止。
	prov := providertest.New().QueueStreamFunc(func(ctx context.Context) <-chan provider.StreamChunk {
		ch := make(chan provider.StreamChunk)
		go func() {
			defer close(ch)
			<-ctx.Done()
		}()
		return ch
	})

	a := agent.New(prov, agent.System("回答。"))
	conv := agent.NewConversation()
	r := a.Runner(conv)
	defer func() { _ = r.Close() }()

	deadline, cancel := context.WithTimeout(ctx, 80*time.Millisecond)
	defer cancel()

	fmt.Print("user: 慢慢来\nassistant: ")
	events, _ := r.Send(deadline, "慢慢来")
	var stop agent.StopReason
	for ev := range events {
		switch ev.Kind {
		case agent.EventCancelled:
			fmt.Printf("[canceled event StopReason=%s]\n", ev.StopReason)
		case agent.EventTurnEnd:
			stop = ev.StopReason
		}
	}

	last, _ := conv.MessageAt(conv.Len() - 1)
	// UI 看到 StopTimeout / PartialTimeout，可显示"已超时，重试"按钮；与显式
	// Cancel（StopCancelled / PartialCancelled）的提示文案不同。
	fmt.Printf("[turn-end stop=%s] partial PartialReason=%q\n", stop, last.PartialReason)
}

// ---------------- scenario 8: hook 错误 EventError ----------------

func runHookError(ctx context.Context) {
	boom := errors.New("policy gate down")
	prov := providertest.New().QueueStream(
		provider.StreamChunk{ContentDelta: "irrelevant"},
		provider.StreamChunk{FinishReason: provider.FinishStop},
	)
	a := agent.New(prov,
		agent.UserPromptSubmit(func(_ context.Context, _ *agent.UserPromptInput) (*agent.UserPromptOutput, error) {
			// 模拟 policy hook 抛错——常见于"远端策略服务挂了"的场景。
			return nil, boom
		}),
	)
	conv := agent.NewConversation()
	r := a.Runner(conv)
	defer func() { _ = r.Close() }()

	fmt.Println("user: hi (UserPromptSubmit hook 会先抛错)")
	events, _ := r.Send(ctx, "hi")
	for ev := range events {
		switch ev.Kind {
		case agent.EventError:
			// UI 上可以专门处理 *agent.HookError 区分 hook 失败 vs provider 失败。
			var he *agent.HookError
			if errors.As(ev.Error, &he) {
				fmt.Printf("[event_error] hook stage=%s tool=%q cause=%v\n",
					he.Stage, he.Tool, he.Cause)
			} else {
				fmt.Printf("[event_error] %v\n", ev.Error)
			}
		case agent.EventTurnEnd:
			fmt.Printf("[turn_end stop=%s reason=%q]\n", ev.StopReason, ev.PartialReason)
		}
	}
}

// ---------------- helpers ----------------

func slowStreamWithUsage(text string, perChar time.Duration, finalUsage *provider.Usage) func(context.Context) <-chan provider.StreamChunk {
	return func(ctx context.Context) <-chan provider.StreamChunk {
		ch := make(chan provider.StreamChunk, 16)
		go func() {
			defer close(ch)
			for i, r := range text {
				select {
				case <-ctx.Done():
					return
				case ch <- provider.StreamChunk{
					ContentDelta: string(r),
					// Provider docs say Usage is a cumulative snapshot; emit
					// every chunk so a mid-stream cancel still preserves the
					// latest counts on the partial.
					Usage: cumulativeUsage(finalUsage, i+1, len([]rune(text))),
				}:
				}
				time.Sleep(perChar)
			}
			ch <- provider.StreamChunk{FinishReason: provider.FinishStop, Usage: finalUsage}
		}()
		return ch
	}
}

func cumulativeUsage(final *provider.Usage, n, total int) *provider.Usage {
	if final == nil || total <= 0 {
		return nil
	}
	scale := func(v int) int { return v * n / total }
	return &provider.Usage{
		PromptTokens:     final.PromptTokens, // prompt tokens fixed once known
		CompletionTokens: scale(final.CompletionTokens),
		TotalTokens:      final.PromptTokens + scale(final.CompletionTokens),
	}
}

func formatUsage(u *provider.Usage) string {
	if u == nil {
		return "<nil>"
	}
	return fmt.Sprintf("prompt=%d completion=%d total=%d",
		u.PromptTokens, u.CompletionTokens, u.TotalTokens)
}

func textOf(blocks []agent.ContentBlock) string {
	var b strings.Builder
	for _, blk := range blocks {
		switch v := blk.(type) {
		case agent.TextBlock:
			b.WriteString(v.Text)
		case agent.SummaryBlock:
			b.WriteString(v.Text)
		}
	}
	return b.String()
}

func dumpLLMPrompt(label string, req *provider.CompletionRequest) {
	fmt.Printf("[%s]\n", label)
	for i, m := range req.Messages {
		role := string(m.Role)
		content := m.Content
		if len(content) > 80 {
			content = content[:77] + "..."
		}
		fmt.Printf("  [%d] %-9s %q\n", i, role, content)
	}
}
