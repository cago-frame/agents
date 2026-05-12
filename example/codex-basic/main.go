// Package main shows how to drive the local Codex app-server through codex.New.
//
// Prerequisite: install and log in to Codex CLI (`codex login`).
//
// Run:
//
//	go run ./example/codex-basic
//
// This example is not intended for CI because it calls the real local CLI.
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

	"github.com/cago-frame/agents/agent"
	"github.com/cago-frame/agents/cliagent/codex"
	"github.com/cago-frame/agents/tool"
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
	r := codex.New(
		codex.Cwd(cwd),
		codex.Sandbox(codex.SandboxWorkspaceWrite),
		codex.Tools(timeTool()),
	)
	defer func() { _ = r.Close(context.Background()) }()

	sess := r.Session()
	defer func() { _ = sess.Close(context.Background()) }()

	stream, err := sess.Stream(ctx, "Call the `now` MCP tool, then run `pwd`, and summarize both results.")
	if err != nil {
		log.Fatal(err)
	}
	printEvents(stream)

	stream, err = sess.Stream(ctx, "What did the previous tool call return?")
	if err != nil {
		log.Fatal(err)
	}
	printEvents(stream)

	res, err := stream.Result()
	if err != nil {
		fmt.Printf("[result err] %v\n", err)
		return
	}
	if res != nil {
		fmt.Printf("\n[result] stop=%s text_len=%d thread_id=%s\n", res.Stop, len(res.Text), res.State.ThreadID)
	}
}

func printEvents(stream *codex.Stream) {
	for stream.Next() {
		ev := stream.Event()
		switch ev.Kind {
		case codex.EventSessionStart:
			fmt.Printf("[meta] thread_id=%s\n", ev.SessionID)
		case codex.EventTextDelta:
			fmt.Print(ev.Text)
		case codex.EventPreToolUse:
			if ev.Tool != nil {
				fmt.Printf("\n[tool-start] %s(%s)\n", ev.Tool.Name, ev.Tool.Input)
			}
		case codex.EventPostToolUse:
			if ev.Tool != nil {
				fmt.Printf("[tool-end] %s err=%v\n", ev.Tool.Name, ev.Tool.Err)
				if result := formatToolResponse(ev.Tool.Response); result != "" {
					fmt.Printf("[tool-result]\n%s\n", result)
				}
			}
		case codex.EventDone:
			fmt.Printf("\n[done] stop=%s\n", ev.Stop)
		case codex.EventError:
			fmt.Printf("\n[error] %v\n", ev.Err)
		}
	}
}

func formatToolResponse(b []byte) string {
	if len(b) == 0 {
		return ""
	}
	var v any
	if err := json.Unmarshal(b, &v); err != nil {
		return string(b)
	}
	return formatAny(v)
}

func formatAny(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return x
	case []any:
		var out strings.Builder
		for _, item := range x {
			text := formatAny(item)
			if text == "" {
				continue
			}
			if out.Len() > 0 {
				out.WriteByte('\n')
			}
			out.WriteString(text)
		}
		return out.String()
	case map[string]any:
		if content, ok := x["content"].([]any); ok {
			return formatAny(content)
		}
		if text, _ := x["text"].(string); text != "" {
			return text
		}
		if output, _ := x["output"].(string); output != "" {
			var out strings.Builder
			if status, _ := x["status"].(string); status != "" {
				out.WriteString("status=")
				out.WriteString(status)
				if exitCode, ok := x["exitCode"].(float64); ok {
					fmt.Fprintf(&out, " exit_code=%.0f", exitCode)
				}
				out.WriteByte('\n')
			}
			out.WriteString(output)
			return strings.TrimRight(out.String(), "\n")
		}
		data, _ := json.Marshal(x)
		return string(data)
	default:
		data, err := json.Marshal(x)
		if err != nil {
			return fmt.Sprintf("%v", x)
		}
		return string(data)
	}
}
