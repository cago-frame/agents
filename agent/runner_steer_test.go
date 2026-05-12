package agent_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	agent "github.com/cago-frame/agents/agent"
	"github.com/cago-frame/agents/provider"
	"github.com/cago-frame/agents/provider/providertest"
)

func TestSteer_InjectsAtSafePoint(t *testing.T) {
	args, _ := json.Marshal(map[string]any{"text": "go"})

	// The second stream starts blocked (no chunks queued initially).
	// We release it after steer is injected.
	release := make(chan struct{})

	prov := providertest.New().
		QueueStream(
			provider.StreamChunk{ToolCallDelta: &provider.ToolCallDelta{Index: 0, ID: "t1", Name: "Echo", ArgsDelta: string(args)}},
			provider.StreamChunk{FinishReason: provider.FinishToolCalls},
		).
		QueueStreamFunc(func(ctx context.Context) <-chan provider.StreamChunk {
			ch := make(chan provider.StreamChunk, 2)
			go func() {
				select {
				case <-release:
				case <-ctx.Done():
					close(ch)
					return
				}
				ch <- provider.StreamChunk{ContentDelta: "after-steer"}
				ch <- provider.StreamChunk{FinishReason: provider.FinishStop}
				close(ch)
			}()
			return ch
		})

	a := agent.New(prov, agent.Tools(echoTool{}))
	conv := agent.NewConversation()
	r := a.Runner(conv)
	defer func() { _ = r.Close() }()

	events, _ := r.Send(context.Background(), "first")

	done := make(chan struct{})
	var deltas []string
	go func() {
		defer close(done)
		for ev := range events {
			if ev.Kind == agent.EventPostToolUse {
				// Safe point: tool dispatch just finished; inject steer before next LLM call.
				if err := r.Steer(context.Background(), "be brief"); err != nil {
					t.Errorf("steer: %v", err)
				}
				// Unblock the second LLM stream.
				close(release)
			}
			if ev.Kind == agent.EventTextDelta {
				deltas = append(deltas, ev.Delta)
			}
		}
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatalf("timed out waiting for events")
	}

	if !strings.Contains(strings.Join(deltas, ""), "after-steer") {
		t.Fatalf("expected post-steer text, got %v", deltas)
	}

	// Steer must have appended a user message into history (between the tool
	// result and the second LLM call).
	found := false
	for i := 0; i < conv.Len(); i++ {
		m, _ := conv.MessageAt(i)
		if m.Role == agent.RoleUser && textOfBlocks(m.Content) == "be brief" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("steered text not found in conv")
	}
}

func TestSteer_PreservesDisplayMetadata(t *testing.T) {
	// turn 1: 慢流，等 user Steer 注入后让 turn 自然结束
	prov := providertest.New().
		QueueStreamFunc(func(ctx context.Context) <-chan provider.StreamChunk {
			ch := make(chan provider.StreamChunk, 8)
			go func() {
				defer close(ch)
				for _, r := range "hello world" {
					select {
					case <-ctx.Done():
						return
					case ch <- provider.StreamChunk{ContentDelta: string(r)}:
					}
					time.Sleep(5 * time.Millisecond)
				}
				ch <- provider.StreamChunk{FinishReason: provider.FinishStop}
			}()
			return ch
		}).
		QueueStream(
			provider.StreamChunk{ContentDelta: "ack"},
			provider.StreamChunk{FinishReason: provider.FinishStop},
		)

	a := agent.New(prov)
	conv := agent.NewConversation()
	r := a.Runner(conv)
	defer func() { _ = r.Close() }()

	// Wait deterministically for the first text delta before steering, so this
	// test is race-free and immune to scheduler skew under -race / slow CI.
	fired := make(chan struct{})
	var once sync.Once
	unsubscribe := r.OnEvent(agent.OnlyKinds(agent.EventTextDelta), func(ctx context.Context, ev agent.Event) {
		once.Do(func() { close(fired) })
	})
	defer unsubscribe()

	events, err := r.Send(context.Background(), "hi")
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	go func() {
		<-fired
		_ = r.Steer(context.Background(), "expanded body", agent.WithSteerDisplay("@raw display"))
	}()
	for range events {
	}

	// 找到被 steer 注入的 user msg：role=user，应当含 DisplayTextBlock 旁路
	// 块（Audience 排除 LLM，但 UI 看得到）。
	var found bool
	for i := 0; i < conv.Len(); i++ {
		m, _ := conv.MessageAt(i)
		if m.Role != agent.RoleUser {
			continue
		}
		for _, b := range m.Content {
			if dt, ok := b.(agent.DisplayTextBlock); ok && dt.Text == "@raw display" {
				found = true
			}
		}
	}
	if !found {
		t.Fatalf("steer'd user msg missing DisplayTextBlock; conv=%+v", conv.Messages())
	}
}

func TestSteer_EmitsSteerConsumedEvent(t *testing.T) {
	// turn 1: tool call → tool result → 排队的 Steer 注入 → turn 2 LLM 出文本。
	// 期望：在 EventPostToolUse 后、turn 2 EventTextDelta 前 emit 一次
	// EventSteerConsumed，Delta 是显示用文本。
	args, _ := json.Marshal(map[string]any{"text": "go"})

	release := make(chan struct{})
	prov := providertest.New().
		QueueStream(
			provider.StreamChunk{ToolCallDelta: &provider.ToolCallDelta{Index: 0, ID: "t1", Name: "Echo", ArgsDelta: string(args)}},
			provider.StreamChunk{FinishReason: provider.FinishToolCalls},
		).
		QueueStreamFunc(func(ctx context.Context) <-chan provider.StreamChunk {
			ch := make(chan provider.StreamChunk, 2)
			go func() {
				select {
				case <-release:
				case <-ctx.Done():
					close(ch)
					return
				}
				ch <- provider.StreamChunk{ContentDelta: "after"}
				ch <- provider.StreamChunk{FinishReason: provider.FinishStop}
				close(ch)
			}()
			return ch
		})

	a := agent.New(prov, agent.Tools(echoTool{}))
	conv := agent.NewConversation()
	r := a.Runner(conv)
	defer func() { _ = r.Close() }()

	events, _ := r.Send(context.Background(), "first")

	var kinds []agent.EventKind
	var consumedDeltas []string
	for ev := range events {
		kinds = append(kinds, ev.Kind)
		if ev.Kind == agent.EventPostToolUse {
			if err := r.Steer(context.Background(), "llm-text", agent.WithSteerDisplay("display-text")); err != nil {
				t.Fatalf("steer: %v", err)
			}
			close(release)
		}
		if ev.Kind == agent.EventSteerConsumed {
			consumedDeltas = append(consumedDeltas, ev.Delta)
		}
	}

	if len(consumedDeltas) != 1 || consumedDeltas[0] != "display-text" {
		t.Fatalf("want one EventSteerConsumed carrying displayText, got %v", consumedDeltas)
	}

	// 顺序：PostToolUse → SteerConsumed → TextDelta（turn 2 的回复）
	post, consumed, text := -1, -1, -1
	for i, k := range kinds {
		switch {
		case k == agent.EventPostToolUse && post == -1:
			post = i
		case k == agent.EventSteerConsumed && consumed == -1:
			consumed = i
		case k == agent.EventTextDelta && text == -1:
			text = i
		}
	}
	if !(post >= 0 && consumed > post && text > consumed) {
		t.Fatalf("event order wrong: post=%d consumed=%d text=%d, kinds=%v", post, consumed, text, kinds)
	}
}

func TestSteer_NoOptsBackwardCompat(t *testing.T) {
	// 确保不传 opts 时旧行为不变
	prov := providertest.New().QueueStream(
		provider.StreamChunk{ContentDelta: "x"},
		provider.StreamChunk{FinishReason: provider.FinishStop},
	)
	a := agent.New(prov)
	conv := agent.NewConversation()
	r := a.Runner(conv)
	defer func() { _ = r.Close() }()
	events, _ := r.Send(context.Background(), "hi")
	for range events {
	}
	// turn 已结束，Steer 应返 ErrSteerNoActiveTurn（不传 opts 也要兼容）
	if err := r.Steer(context.Background(), "late"); !errors.Is(err, agent.ErrSteerNoActiveTurn) {
		t.Fatalf("want ErrSteerNoActiveTurn, got %v", err)
	}
}
