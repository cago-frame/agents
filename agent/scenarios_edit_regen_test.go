package agent_test

import (
	"context"
	"testing"

	. "github.com/smartystreets/goconvey/convey"

	"github.com/cago-frame/agents/provider"
	"github.com/cago-frame/agents/provider/providertest"
)

func TestScenario_EditAndRegenerate(t *testing.T) {
	Convey("Feature: UI 编辑某条消息重发 / 点击重新生成", t, func() {

		Convey("Scenario A: 编辑 user 消息重发 → 替换该 user 之后的所有消息", func() {
			// Given 两轮 + 第三轮（edited 重发）的回复
			prov := providertest.New().
				QueueStream(
					provider.StreamChunk{ContentDelta: "first answer"},
					provider.StreamChunk{FinishReason: provider.FinishStop},
				).
				QueueStream(
					provider.StreamChunk{ContentDelta: "second answer"},
					provider.StreamChunk{FinishReason: provider.FinishStop},
				).
				QueueStream(
					provider.StreamChunk{ContentDelta: "edited answer"},
					provider.StreamChunk{FinishReason: provider.FinishStop},
				)
			_, conv, r := setupAgent(t, prov)

			// 跑两轮
			drainEvents(mustSend(t, r, "q1"))
			drainEvents(mustSend(t, r, "q2"))
			So(conv.Len(), ShouldEqual, 4)

			// When 用户编辑 q2 → Truncate(2) + Send(edited)
			So(conv.Truncate(2), ShouldBeNil)
			So(conv.Len(), ShouldEqual, 2)

			drainEvents(mustSend(t, r, "q2-edited"))

			// Then 新增 q2-edited 和 "edited answer"，旧 q2/second answer 已丢
			So(conv.Len(), ShouldEqual, 4)
			final := lastMessage(t, conv)
			So(textOfBlocks(final.Content), ShouldEqual, "edited answer")

			// 倒数第二条应是新的 user msg
			user2, _ := conv.MessageAt(2)
			So(textOfBlocks(user2.Content), ShouldEqual, "q2-edited")
		})

		Convey("Scenario B: 点'重新生成' → Truncate 末条 + Resend 复发同一 user", func() {
			prov := providertest.New().
				QueueStream(
					provider.StreamChunk{ContentDelta: "original answer"},
					provider.StreamChunk{FinishReason: provider.FinishStop},
				).
				QueueStream(
					provider.StreamChunk{ContentDelta: "regenerated answer"},
					provider.StreamChunk{FinishReason: provider.FinishStop},
				)
			_, conv, r := setupAgent(t, prov)

			drainEvents(mustSend(t, r, "question"))
			So(conv.Len(), ShouldEqual, 2)
			original := lastMessage(t, conv)
			So(textOfBlocks(original.Content), ShouldEqual, "original answer")

			// When 用户点重新生成：Truncate(1) + Resend
			So(conv.Truncate(1), ShouldBeNil)
			events, err := r.Resend(context.Background())
			So(err, ShouldBeNil)
			drainEvents(events)

			// Then conv 长度回到 2，末条是 regenerated
			So(conv.Len(), ShouldEqual, 2)
			regen := lastMessage(t, conv)
			So(textOfBlocks(regen.Content), ShouldEqual, "regenerated answer")

			// And 第二个 LLM request 收到的还是同一 user "question"
			received := prov.Received()
			So(len(received), ShouldEqual, 2)
			lastMsg := received[1].Messages[len(received[1].Messages)-1]
			So(lastMsg.Content, ShouldContainSubstring, "question")
		})
	})
}
