// example/claudecode-multiturn 跑真 Claude Code 两轮，观察跨轮复用同一个 claude
// 进程时第二轮延迟下降（省掉冷启动 + MCP 重连 + prompt cache 重建）。
// 进程在 sess.Close() 时才退出。需要本机已装 claude CLI 且已登录。非 CI。
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/cago-frame/agents/cliagent/claudecode"
)

func main() {
	cwd, _ := os.Getwd()
	r := claudecode.New(
		claudecode.Cwd(cwd),
		claudecode.Model("claude-sonnet-4-6"),
		claudecode.InitTimeout(10*time.Second),
	)
	defer func() { _ = r.Close(context.Background()) }()

	sess := r.Session()
	defer func() { _ = sess.Close(context.Background()) }()

	for i, prompt := range []string{
		"say hello in one word",
		"now say goodbye in one word",
	} {
		t0 := time.Now()
		stream, err := sess.Stream(context.Background(), prompt)
		if err != nil {
			log.Fatalf("turn %d: %v", i+1, err)
		}
		for stream.Next() {
			ev := stream.Event()
			if ev.Kind == claudecode.EventTextDelta {
				fmt.Print(ev.Text)
			}
		}
		fmt.Printf("\n  turn %d elapsed: %v\n\n", i+1, time.Since(t0))
	}
}
