// Package main 演示消费 Claude Code *原生* Task / Agent 子 agent 的流式输出。
//
// 架构：
//   - 父 runner 用 claudecode.New 驱动本地 `claude -p`，不写 Go 侧 subagent.NewTool。
//   - 父 LLM 调用 Claude Code 内置的 `Task` / `Agent` 工具把任务派给一个子 agent。
//   - 子 agent 的最终结果出现在 Task/Agent 工具的 tool_result（EventPostToolUse）里，
//     确保内容一定可见。若 CLI 版本把中间 assistant 消息冒泡，也会通过
//     EventTextDelta / EventThinkingDelta 实时打印。
//
// 前置：机器上装好 claude-code CLI 并 `claude login`。
//
// 跑法：
//
//	go run ./example/claudecode-substream
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/cago-frame/agents/cliagent/claudecode"
)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	cwd, _ := os.Getwd()
	r := claudecode.New(
		claudecode.Cwd(cwd),
		claudecode.Model("claude-sonnet-4-6"),
		// -p 非交互模式下必须 bypass，否则内置工具（含 Task/Agent）会阻塞询权。
		claudecode.Permission(claudecode.PermissionModeBypassPermissions),
		// 不同 CLI 版本下派发工具可能叫 Task 或 Agent；都放进允许名单。
		// 子 agent 内部常用的 Read/Grep/Glob 也一并允许，避免它自己起动时被卡。
		claudecode.AllowedTools([]string{"Task", "Agent", "Read", "Grep", "Glob"}),
		// 让 CLI 把 Anthropic SSE 的 text_delta / thinking_delta 原样吐出
		// （stream_event）。不开此 flag CLI 只会在整段写完后送一个 aggregated
		// assistant 消息，看起来就是"一次性整段"。
		claudecode.IncludePartialMessages(),
		claudecode.SystemPrompt(
			"任何代码探索类任务都必须通过 Task / Agent 工具派发给一个通用子 agent 去做，"+
				"你自己不要直接读文件、不要直接 Grep。最终把子 agent 的原文转述给用户。"),
	)
	defer func() { _ = r.Close(context.Background()) }()

	sess := r.Session()
	defer func() { _ = sess.Close(context.Background()) }()

	stream, err := sess.Stream(ctx,
		"派发一个通用子 agent：读 example/simple/main.go，"+
			"用 2-3 句话解释它的作用，并列出所有导入的包。")
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
}

// printEvents 打印 stream 中的各类事件。子 agent 的最终结果会在 Task/Agent 工具的
// tool_result（[sub-result]）上完整出现；spec §11.1 起框架不再把子 agent 内部事件
// 冒泡到父 stream，所以这里不区分父/子 agent 颜色。
func printEvents(stream *claudecode.Stream) {
	// 记录当前 Task/Agent 工具的 call_id，用于在它的 tool_result 上打子 agent 最终文本。
	taskCallIDs := map[string]struct{}{}

	for stream.Next() {
		ev := stream.Event()
		switch ev.Kind {
		case claudecode.EventSessionStart:
			fmt.Printf("[meta] session_id=%s\n", ev.SessionID)

		case claudecode.EventTextDelta:
			// 为了把"流式感"展示出来：每次 TextDelta 都另起一行，
			// 用 <delta> 标签包起来。一条 delta 到达 = 一行输出。
			fmt.Printf("<delta>%s</delta>\n", visible(ev.Text))
			_ = os.Stdout.Sync()

		case claudecode.EventThinkingDelta:
			fmt.Printf("<thinking>%s</thinking>\n", visible(ev.Text))
			_ = os.Stdout.Sync()

		case claudecode.EventPreToolUse:
			if ev.Tool != nil {
				if isTaskTool(ev.Tool.Name) {
					taskCallIDs[ev.Tool.ID] = struct{}{}
				}
				fmt.Printf("\n[tool-start] %s %s\n", ev.Tool.Name, ev.Tool.Input)
			}

		case claudecode.EventPostToolUse:
			if ev.Tool != nil {
				isTask := false
				if _, ok := taskCallIDs[ev.Tool.ID]; ok {
					isTask = true
					delete(taskCallIDs, ev.Tool.ID)
				}
				fmt.Printf("\n[tool-end] %s err=%v\n", ev.Tool.Name, ev.Tool.Err)
				result := formatResult(ev.Tool.Response)
				if isTask {
					fmt.Printf("\x1b[36m[sub-result]\n%s\x1b[0m\n", result)
				} else {
					fmt.Printf("[result]\n%s\n", result)
				}
			}

		case claudecode.EventDone:
			fmt.Printf("\n[done] stop=%s\n", ev.Stop)

		case claudecode.EventError:
			fmt.Printf("\n[error] %v\n", ev.Err)
		}
	}
}

func isTaskTool(name string) bool {
	return name == "Task" || name == "Agent"
}

// formatResult 把 tool_result.Content 扁平化成字符串。
// claude 的 tool_result.content 形状有三种常见：string / []byte / JSON。
func formatResult(b []byte) string {
	if len(b) == 0 {
		return ""
	}
	// Try to decode as JSON for pretty display.
	var v any
	if err := json.Unmarshal(b, &v); err == nil {
		return formatAny(v)
	}
	return string(b)
}

func formatAny(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return x
	case map[string]any:
		if t, _ := x["text"].(string); t != "" {
			return t
		}
		j, _ := json.Marshal(x)
		return string(j)
	case []any:
		var b strings.Builder
		for _, part := range x {
			if m, ok := part.(map[string]any); ok {
				if t, _ := m["text"].(string); t != "" {
					b.WriteString(t)
					continue
				}
				j, _ := json.Marshal(m)
				b.Write(j)
				continue
			}
			fmt.Fprintf(&b, "%v", part)
		}
		return b.String()
	default:
		j, err := json.Marshal(v)
		if err != nil {
			return fmt.Sprintf("%+v", v)
		}
		return string(j)
	}
}

// visible 把 delta 里的换行 / 制表等不可见字符可视化，
// 这样即便是一次只到一个字符（甚至一个空白），每行也能看到它。
func visible(s string) string {
	r := strings.NewReplacer("\n", "\\n", "\r", "\\r", "\t", "\\t")
	return r.Replace(s)
}
