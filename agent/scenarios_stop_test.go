package agent_test

import (
	"context"
	"testing"
	"time"

	. "github.com/smartystreets/goconvey/convey"

	"github.com/cago-frame/agents/agent"
	"github.com/cago-frame/agents/provider"
	"github.com/cago-frame/agents/provider/providertest"
)

// slowStream returns a generator that emits one rune per perChar, then closes WITHOUT a
// FinishReason — caller is expected to Cancel mid-stream.
func slowStream(text string, perChar time.Duration) func(context.Context) <-chan provider.StreamChunk {
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
		}()
		return ch
	}
}

func TestScenario_UserStop(t *testing.T) {
	Convey("Feature: 用户点'停止'按钮", t, func() {

		Convey("Scenario: 流到一半 r.Cancel → conv 保留 PartialCancelled + 文本", func() {
			// Given 慢流 provider（1ms/char），下一轮续写
			prov := providertest.New().
				QueueStreamFunc(slowStream("Once upon a time in a small village", 1*time.Millisecond)).
				QueueStream(
					provider.StreamChunk{ContentDelta: ", a child found a key."},
					provider.StreamChunk{FinishReason: provider.FinishStop},
				)

			_, conv, r := setupAgent(t, prov, agent.System("你是讲故事的人。"))

			// When 收到 >= 16 byte 后 Cancel
			events := mustSend(t, r, "讲个故事")

			var canceled bool
			var sawCancelEvent bool
			var charsRead int
			for ev := range events {
				switch ev.Kind {
				case agent.EventTextDelta:
					charsRead += len(ev.Delta)
					if !canceled && charsRead >= 16 {
						canceled = true
						So(r.Cancel("user clicked stop"), ShouldBeNil)
					}
				case agent.EventCancelled:
					sawCancelEvent = true
				}
			}
			So(sawCancelEvent, ShouldBeTrue)

			// Then conv 末条 = PartialCancelled，已输出文本保留
			last := lastMessage(t, conv)
			So(last.PartialReason, ShouldEqual, agent.PartialCancelled)
			So(textOfBlocks(last.Content), ShouldNotBeBlank)

			// And 下一轮 Send 默认回灌 partial 给 LLM
			drainEvents(mustSend(t, r, "继续"))

			received := prov.Received()
			So(len(received), ShouldEqual, 2)
			// 第二个 request 至少有 user+assistant(partial)+user 三条消息
			So(len(received[1].Messages), ShouldBeGreaterThanOrEqualTo, 3)
		})
	})
}
