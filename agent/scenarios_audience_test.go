package agent_test

import (
	"context"
	"strings"
	"testing"
	"time"

	. "github.com/smartystreets/goconvey/convey"

	"github.com/cago-frame/agents/agent"
	"github.com/cago-frame/agents/agent/blocks"
	"github.com/cago-frame/agents/provider"
	"github.com/cago-frame/agents/provider/providertest"
)

// displayTextFromMessage 把 Message 投影成 DisplayMessage，再从 Segments 里
// 提取所有 DisplayText segment 拼成单串，方便子串断言。
func displayTextFromMessage(m agent.Message) string {
	dm := agent.RenderForDisplay(m)
	var sb strings.Builder
	for _, seg := range dm.Segments {
		if dt, ok := seg.(agent.DisplayText); ok {
			if sb.Len() > 0 {
				sb.WriteString("\n")
			}
			sb.WriteString(dt.Text)
		}
	}
	return sb.String()
}

func TestScenario_AudienceProjection(t *testing.T) {
	Convey("Feature: WithSendDisplay / WithSteerDisplay 双向投影（UI vs LLM）", t, func() {

		Convey("Scenario: send 注入 DisplayTextBlock → UI 看原文，LLM 看 expanded", func() {
			prov := providertest.New().QueueStream(
				provider.StreamChunk{ContentDelta: "ack"},
				provider.StreamChunk{FinishReason: provider.FinishStop},
			)
			_, conv, r := setupAgent(t, prov)

			expanded := "PLEASE_EXECUTE_LONG_EXPANDED_BODY"
			display := "@srv1 status"
			drainEvents(mustSend(t, r, expanded, agent.WithSendDisplay(display)))

			// UI 投影：第一条 user message 的 DisplayText 应为 display，而非 expanded
			userMsg, err := conv.MessageAt(0)
			So(err, ShouldBeNil)
			uiText := displayTextFromMessage(userMsg)
			So(uiText, ShouldContainSubstring, display)
			So(uiText, ShouldNotContainSubstring, expanded)

			// LLM 投影：BuildRequest 应只发 expanded，不发 display
			received := prov.Received()
			So(len(received), ShouldEqual, 1)
			var sawExpanded, sawDisplay bool
			for _, m := range received[0].Messages {
				if m.Role != provider.RoleUser {
					continue
				}
				combined := m.Content
				for _, p := range m.MultiContent {
					combined += "\n" + p.Text
				}
				if strings.Contains(combined, expanded) {
					sawExpanded = true
				}
				if strings.Contains(combined, display) {
					sawDisplay = true
				}
			}
			So(sawExpanded, ShouldBeTrue)
			So(sawDisplay, ShouldBeFalse)
		})

		Convey("Scenario: DisplayTextBlock 经 blocks.EncodeAll/DecodeAll 完整还原", func() {
			original := []agent.ContentBlock{
				agent.DisplayTextBlock{Text: "@srv1 status"},
				agent.TextBlock{Text: "expanded body"},
			}

			encoded, err := blocks.EncodeAll(original)
			So(err, ShouldBeNil)

			decoded, err := blocks.DecodeAll(encoded)
			So(err, ShouldBeNil)
			So(len(decoded), ShouldEqual, 2)

			// 不依赖具体顺序，按类型查找。
			var (
				gotDisplay agent.DisplayTextBlock
				gotText    agent.TextBlock
				okDisplay  bool
				okText     bool
			)
			for _, b := range decoded {
				switch v := b.(type) {
				case agent.DisplayTextBlock:
					gotDisplay = v
					okDisplay = true
				case agent.TextBlock:
					gotText = v
					okText = true
				}
			}
			So(okDisplay, ShouldBeTrue)
			So(gotDisplay.Text, ShouldEqual, "@srv1 status")
			So(okText, ShouldBeTrue)
			So(gotText.Text, ShouldEqual, "expanded body")
		})

		Convey("Scenario: Steer with WithSteerDisplay 同理", func() {
			// 长慢流给 steer 时间介入；steered turn 之后跟一段 finalize。
			prov := providertest.New().
				QueueStreamFunc(slowStreamWithStop("waiting for nudge", 1*time.Millisecond)).
				QueueStream(
					provider.StreamChunk{ContentDelta: "ok done"},
					provider.StreamChunk{FinishReason: provider.FinishStop},
				)
			_, conv, r := setupAgent(t, prov)

			events, err := r.Send(context.Background(), "start")
			So(err, ShouldBeNil)

			var steered bool
			for ev := range events {
				if ev.Kind == agent.EventTextDelta && !steered {
					steered = true
					_ = r.Steer(context.Background(),
						"EXPANDED_STEER_BODY",
						agent.WithSteerDisplay("@srv1 nudge"))
				}
			}
			So(steered, ShouldBeTrue)

			// 找 conv 里 inject 的 user message（不是最初的 "start"）—
			// 它的 UI 投影应包含 "@srv1 nudge"。
			var found bool
			for i := 0; i < conv.Len(); i++ {
				m, err := conv.MessageAt(i)
				So(err, ShouldBeNil)
				if m.Role != agent.RoleUser {
					continue
				}
				uiText := displayTextFromMessage(m)
				if strings.Contains(uiText, "@srv1 nudge") {
					found = true
					break
				}
			}
			So(found, ShouldBeTrue)
		})
	})
}
