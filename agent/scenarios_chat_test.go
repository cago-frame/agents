package agent_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	. "github.com/smartystreets/goconvey/convey"

	"github.com/cago-frame/agents/agent"
	"github.com/cago-frame/agents/provider"
	"github.com/cago-frame/agents/provider/providertest"
)

// chatAddTool: 算术 add 工具
type chatAddTool struct{}

func (chatAddTool) Name() string        { return "add" }
func (chatAddTool) Description() string { return "把两个整数相加。" }
func (chatAddTool) Schema() agent.Schema {
	return agent.Schema{
		Type: "object",
		Properties: map[string]*agent.Property{
			"a": {Type: "integer"},
			"b": {Type: "integer"},
		},
		Required: []string{"a", "b"},
	}
}
func (chatAddTool) Call(_ context.Context, in map[string]any) (*agent.ToolResultBlock, error) {
	a, _ := in["a"].(float64)
	b, _ := in["b"].(float64)
	return &agent.ToolResultBlock{
		Content: []agent.ContentBlock{agent.TextBlock{Text: fmt.Sprintf("%d", int(a)+int(b))}},
	}, nil
}

// chatWriteNoteTool: write_note 工具
type chatWriteNoteTool struct{}

func (chatWriteNoteTool) Name() string        { return "write_note" }
func (chatWriteNoteTool) Description() string { return "写一条 note。" }
func (chatWriteNoteTool) Schema() agent.Schema {
	return agent.Schema{
		Type:       "object",
		Properties: map[string]*agent.Property{"text": {Type: "string"}},
		Required:   []string{"text"},
	}
}
func (chatWriteNoteTool) Call(_ context.Context, _ map[string]any) (*agent.ToolResultBlock, error) {
	return &agent.ToolResultBlock{
		Content: []agent.ContentBlock{agent.TextBlock{Text: "ok"}},
	}, nil
}

func TestScenario_Chat(t *testing.T) {
	Convey("Feature: 基础多轮对话 + tool + audit hook", t, func() {

		Convey("Scenario: 模型用 add 工具算 2+3", func() {
			prov := providertest.New().
				QueueStream(
					provider.StreamChunk{ToolCallDelta: &provider.ToolCallDelta{Index: 0, ID: "tc1", Name: "add"}},
					provider.StreamChunk{ToolCallDelta: &provider.ToolCallDelta{Index: 0, ArgsDelta: `{"a":2,"b":3}`}},
					provider.StreamChunk{FinishReason: provider.FinishToolCalls},
				).
				QueueStream(
					provider.StreamChunk{ContentDelta: "答案是 5。"},
					provider.StreamChunk{FinishReason: provider.FinishStop},
				)
			_, conv, r := setupAgent(t, prov, agent.Tools(chatAddTool{}))

			evts := drainEvents(mustSend(t, r, "请用 add 工具计算 2+3"))

			preEv, ok := findEvent(evts, agent.EventPreToolUse)
			So(ok, ShouldBeTrue)
			So(preEv.Tool, ShouldNotBeNil)
			So(preEv.Tool.Name, ShouldEqual, "add")

			postEv, ok := findEvent(evts, agent.EventPostToolUse)
			So(ok, ShouldBeTrue)
			So(postEv.Tool, ShouldNotBeNil)
			So(textOfBlocks(postEv.Tool.Output.Content), ShouldEqual, "5")

			final := lastMessage(t, conv)
			So(textOfBlocks(final.Content), ShouldEqual, "答案是 5。")
		})

		Convey("Scenario: 中间件拒含 secret 的 note，模型重试成功", func() {
			auditMW := func(c *agent.ToolContext) {
				text, _ := c.Input["text"].(string)
				if strings.Contains(strings.ToLower(text), "secret") {
					c.AbortWithDeny("note 含敏感字样")
					return
				}
				c.Next()
			}

			prov := providertest.New().
				QueueStream(
					provider.StreamChunk{ToolCallDelta: &provider.ToolCallDelta{Index: 0, ID: "tc2", Name: "write_note"}},
					provider.StreamChunk{ToolCallDelta: &provider.ToolCallDelta{Index: 0, ArgsDelta: `{"text":"secret payload"}`}},
					provider.StreamChunk{FinishReason: provider.FinishToolCalls},
				).
				QueueStream(
					provider.StreamChunk{ToolCallDelta: &provider.ToolCallDelta{Index: 0, ID: "tc3", Name: "write_note"}},
					provider.StreamChunk{ToolCallDelta: &provider.ToolCallDelta{Index: 0, ArgsDelta: `{"text":"hello world"}`}},
					provider.StreamChunk{FinishReason: provider.FinishToolCalls},
				).
				QueueStream(
					provider.StreamChunk{ContentDelta: "已写入。"},
					provider.StreamChunk{FinishReason: provider.FinishStop},
				)

			_, conv, r := setupAgent(t, prov,
				agent.Tools(chatWriteNoteTool{}),
				agent.Use("write_note", auditMW),
			)

			evts := drainEvents(mustSend(t, r, "请写一条 note"))

			var preCount, postCount int
			for _, ev := range evts {
				if ev.Kind == agent.EventPreToolUse {
					preCount++
				}
				if ev.Kind == agent.EventPostToolUse {
					postCount++
				}
			}
			So(preCount, ShouldBeGreaterThanOrEqualTo, 2)
			So(postCount, ShouldBeGreaterThanOrEqualTo, 2)

			final := lastMessage(t, conv)
			So(textOfBlocks(final.Content), ShouldEqual, "已写入。")
		})

		Convey("Scenario: 多轮 Conv 复用 — 第二轮 prompt 带上第一轮 history", func() {
			prov := providertest.New().
				QueueStream(
					provider.StreamChunk{ContentDelta: "hi"},
					provider.StreamChunk{FinishReason: provider.FinishStop},
				).
				QueueStream(
					provider.StreamChunk{ContentDelta: "again"},
					provider.StreamChunk{FinishReason: provider.FinishStop},
				)
			_, conv, r := setupAgent(t, prov)

			drainEvents(mustSend(t, r, "first user msg"))
			drainEvents(mustSend(t, r, "second user msg"))

			So(conv.Len(), ShouldEqual, 4)
			received := prov.Received()
			So(len(received), ShouldEqual, 2)
			So(len(received[1].Messages), ShouldBeGreaterThanOrEqualTo, 3)
		})
	})
}
