package main

import (
	"context"
	"fmt"
	"log"
	"os"

	goai "github.com/sashabaranov/go-openai"

	agent "github.com/cago-frame/agents/agent"
	"github.com/cago-frame/agents/app/coding"
	"github.com/cago-frame/agents/provider"
	"github.com/cago-frame/agents/provider/openai"
	"github.com/cago-frame/agents/provider/providertest"
)

const useMock = true

func main() {
	ctx := context.Background()
	cwd, _ := os.Getwd()

	var prov provider.Provider
	if useMock {
		mock := providertest.New()
		mock.QueueStream(
			provider.StreamChunk{ContentDelta: "app/coding/ contains system.go (System + New), options.go, tools.go, subagents.go, compact.go, context.go, skills.go, slash.go, prompts.go, and doc.go."},
			provider.StreamChunk{FinishReason: provider.FinishStop},
		)
		prov = mock
	} else {
		apiKey := os.Getenv("OPENAI_API_KEY")
		if apiKey == "" {
			log.Fatal("OPENAI_API_KEY not set (or set useMock=true)")
		}
		prov = openai.NewProvider(goai.DefaultConfig(apiKey))
	}

	sys, err := coding.New(ctx, prov, cwd,
		coding.WithModel("gpt-4o-mini"),
		coding.WithCompactionThreshold(80000),
	)
	if err != nil {
		log.Fatal(err)
	}
	defer func() { _ = sys.Close(ctx) }()

	conv := agent.NewConversation()
	r := sys.Agent().Runner(conv)
	defer func() { _ = r.Close() }()

	// 1. /help builtin.
	if reg := sys.SlashRegistry(); reg != nil {
		if res, err := reg.Resolve("/help"); err == nil && res.IsBuiltin {
			if out, err := res.Run(ctx, conv); err == nil {
				fmt.Println("=== /help output ===")
				fmt.Println(out.Notice)
			}
		}
	}

	// 2. Drive a real turn.
	fmt.Println("=== agent response ===")
	events, err := r.Send(ctx, "Summarize the layout of app/coding/ in three bullets.")
	if err != nil {
		log.Fatal(err)
	}
	for ev := range events {
		switch ev.Kind {
		case agent.EventTextDelta:
			fmt.Print(ev.Delta)
		case agent.EventPreToolUse:
			if ev.Tool != nil {
				fmt.Printf("\n[tool start] %s(%v)\n", ev.Tool.Name, ev.Tool.Input)
			}
		case agent.EventPostToolUse:
			if ev.Tool != nil && ev.Tool.Output != nil {
				short := ""
				if len(ev.Tool.Output.Content) > 0 {
					if tb, ok := ev.Tool.Output.Content[0].(agent.TextBlock); ok {
						short = tb.Text
						if len(short) > 200 {
							short = short[:200] + "..."
						}
					}
				}
				fmt.Printf("\n[tool ok] %s → %s\n", ev.Tool.Name, short)
			}
		case agent.EventTurnEnd:
			fmt.Printf("\n[turn end] stop=%d\n", ev.StopReason)
		case agent.EventError:
			fmt.Printf("\n[error] %v\n", ev.Error)
		}
	}
}
