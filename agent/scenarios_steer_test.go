package agent_test

import (
	"context"
	"errors"
	"testing"
	"time"

	. "github.com/smartystreets/goconvey/convey"

	"github.com/cago-frame/agents/agent"
	"github.com/cago-frame/agents/provider"
	"github.com/cago-frame/agents/provider/providertest"
)

// slowStreamWithStop 与 slowStream 类似，但在所有 char 发送完后追加 FinishStop。
// 这样 turn 能自然终止 → 触发 auto-continue 跑下一轮。
func slowStreamWithStop(text string, perChar time.Duration) func(context.Context) <-chan provider.StreamChunk {
	return func(ctx context.Context) <-chan provider.StreamChunk {
		ch := make(chan provider.StreamChunk, 4)
		go func() {
			defer close(ch)
			for _, r := range text {
				select {
				case <-ctx.Done():
					return
				case ch <- provider.StreamChunk{ContentDelta: string(r)}:
				}
				time.Sleep(perChar)
			}
			select {
			case <-ctx.Done():
			case ch <- provider.StreamChunk{FinishReason: provider.FinishStop}:
			}
		}()
		return ch
	}
}

func TestScenario_Steer(t *testing.T) {
	Convey("Feature: 输出过程中用户继续输入", t, func() {

		Convey("Scenario: 流中 Steer → safe point 注入 → auto-continue 一轮", func() {
			// Given turn 1 慢流（1ms/char + FinishStop）+ turn 2 准备好接 steered 输入
			prov := providertest.New().
				QueueStreamFunc(slowStreamWithStop("Hello, here is a long answer", 1*time.Millisecond)).
				QueueStream(
					provider.StreamChunk{ContentDelta: "Brief: hi."},
					provider.StreamChunk{FinishReason: provider.FinishStop},
				)
			_, conv, r := setupAgent(t, prov, agent.System("回答问题。"))

			events := mustSend(t, r, "帮我答个问题")

			// When 读到 10 char 时调 Steer
			var steered bool
			var chars int
			for ev := range events {
				if ev.Kind == agent.EventTextDelta {
					chars += len(ev.Delta)
					if !steered && chars >= 10 {
						steered = true
						err := r.Steer(context.Background(), "be brief")
						So(err, ShouldBeNil)
					}
				}
			}
			So(steered, ShouldBeTrue)

			// Then auto-continue 一轮跑完
			received := prov.Received()
			So(len(received), ShouldBeGreaterThanOrEqualTo, 2)

			// 末条 assistant message 应包含 "Brief" (auto-continue 续了一轮)
			lastAsst := lastMessage(t, conv)
			So(lastAsst.Role, ShouldEqual, agent.RoleAssistant)
			So(textOfBlocks(lastAsst.Content), ShouldContainSubstring, "Brief")
		})

		Convey("Scenario: turn 已结束后 Steer → ErrSteerNoActiveTurn → 兜底 Send", func() {
			prov := providertest.New().
				QueueStream(
					provider.StreamChunk{ContentDelta: "done"},
					provider.StreamChunk{FinishReason: provider.FinishStop},
				).
				QueueStream(
					provider.StreamChunk{ContentDelta: "noted."},
					provider.StreamChunk{FinishReason: provider.FinishStop},
				)
			_, conv, r := setupAgent(t, prov)

			// 跑完一轮
			drainEvents(mustSend(t, r, "question"))
			// 等 active turn 清理
			time.Sleep(10 * time.Millisecond)

			// When turn 已结束时 Steer
			err := r.Steer(context.Background(), "another nudge")

			// Then 拿到 ErrSteerNoActiveTurn
			So(errors.Is(err, agent.ErrSteerNoActiveTurn), ShouldBeTrue)

			// 兜底用普通 Send
			drainEvents(mustSend(t, r, "another nudge"))

			// conv 应有 4 条：q, asst, nudge, noted
			So(conv.Len(), ShouldEqual, 4)
		})
	})
}
