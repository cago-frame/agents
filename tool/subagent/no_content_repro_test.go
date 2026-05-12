package subagent_test

import (
	"context"
	"strings"
	"testing"

	"github.com/cago-frame/agents/agent"
	"github.com/cago-frame/agents/provider"
	"github.com/cago-frame/agents/provider/providertest"
	"github.com/cago-frame/agents/tool"
	"github.com/cago-frame/agents/tool/subagent"
)

// 这些用例复现 OpsKat 那边遇到的 "sub-agent returned no content"：
// 子 agent 自然结束了 turn（StopEndTurn），但 lastAssistantText 返回 ""。
// 目的：确认 cago 当前行为到底丢了什么，再决定是 cago 修 / 还是上游兜底。

// noopTool 返回一个固定 text 结果的最小工具，供子 agent 调用。
func noopTool() tool.Tool {
	return &tool.RawTool{
		NameStr:   "noop",
		DescStr:   "no-op tool, returns a fixed string",
		SchemaVal: agent.Schema{Type: "object"},
		Handler: func(_ context.Context, _ map[string]any) (*agent.ToolResultBlock, error) {
			return tool.TextResult("noop-result"), nil
		},
	}
}

// parentDispatchOnce returns a parent mock that calls sub_agent once with
// {type:"explore", prompt:"go"} and then stops with a trailing "done".
func parentDispatchOnce(toolUseID string) *providertest.Mock {
	pm := providertest.New()
	pm.QueueStream(
		provider.StreamChunk{ToolCallDelta: &provider.ToolCallDelta{Index: 0, ID: toolUseID, Name: "sub_agent"}},
		provider.StreamChunk{ToolCallDelta: &provider.ToolCallDelta{Index: 0, ArgsDelta: `{"type":"explore","prompt":"go"}`}},
		provider.StreamChunk{FinishReason: provider.FinishToolCalls},
	)
	pm.QueueStream(
		provider.StreamChunk{ContentDelta: "done"},
		provider.StreamChunk{FinishReason: provider.FinishStop},
	)
	return pm
}

// Scenario A — child 只调工具，**没有任何**文本块、思考块。
// 模拟：模型决定全程走工具调用，最后一步直接 FinishStop 收尾。
//
// 预期（现状）：parent 拿到 "sub-agent returned no content"。
// 问题：子对话里其实有 ToolUse + ToolResult，cago 把这些信息全丢了，
//
//	父 agent 完全不知道 child 做了什么。
func TestRepro_NoContent_OnlyToolUseChain(t *testing.T) {
	childMock := providertest.New()
	// step 1: 只出一个 tool_use，不带任何 ContentDelta
	childMock.QueueStream(
		provider.StreamChunk{ToolCallDelta: &provider.ToolCallDelta{Index: 0, ID: "cu1", Name: "noop"}},
		provider.StreamChunk{ToolCallDelta: &provider.ToolCallDelta{Index: 0, ArgsDelta: `{}`}},
		provider.StreamChunk{FinishReason: provider.FinishToolCalls},
	)
	// step 2: tool 跑完后，模型直接 FinishStop，不再产 text/tool
	childMock.QueueStream(
		provider.StreamChunk{FinishReason: provider.FinishStop},
	)

	child := agent.New(childMock, agent.Tools(noopTool()))
	dispatchTool := subagent.NewTool("sub_agent", "dispatch", []subagent.Entry{
		{Type: "explore", Description: "d", Agent: child},
	})

	results := runAndGetAllToolResults(t, parentDispatchOnce("a1"), dispatchTool)
	rb := results["a1"]
	if rb == nil {
		t.Fatalf("expected a tool result for a1; got nil")
	}
	got := textOf(rb)
	t.Logf("scenario A result (IsError=%v): %q", rb.IsError, got)

	// 当前行为快照：返回写死的 "sub-agent returned no content"。
	// 这条断言写成"必须是这个字符串"——一旦 cago 改了行为，测试会红，
	// 提醒我们 review 新行为是否更合理。
	if got != "sub-agent returned no content" {
		t.Errorf("scenario A: got %q, want %q (current cago behavior)", got, "sub-agent returned no content")
	}
}

// Scenario B — child 第一步出了文本 + 工具调用，最后一步只 FinishStop 没文本。
// 这是验证 lastAssistantText 的"向后搜索"是否真的能找到更早的文本块。
//
// 预期（按 lastAssistantText 的实现逻辑）：应当返回第一步的 "looking at the repo"。
// 如果实际返回 "sub-agent returned no content"，说明实现/spec 有 bug。
func TestRepro_NoContent_EarlierTextSurvives(t *testing.T) {
	childMock := providertest.New()
	// step 1: 含 ContentDelta + tool_use
	childMock.QueueStream(
		provider.StreamChunk{ContentDelta: "looking at the repo"},
		provider.StreamChunk{ToolCallDelta: &provider.ToolCallDelta{Index: 0, ID: "cu2", Name: "noop"}},
		provider.StreamChunk{ToolCallDelta: &provider.ToolCallDelta{Index: 0, ArgsDelta: `{}`}},
		provider.StreamChunk{FinishReason: provider.FinishToolCalls},
	)
	// step 2: 啥也不产，直接 FinishStop
	childMock.QueueStream(
		provider.StreamChunk{FinishReason: provider.FinishStop},
	)

	child := agent.New(childMock, agent.Tools(noopTool()))
	dispatchTool := subagent.NewTool("sub_agent", "dispatch", []subagent.Entry{
		{Type: "explore", Description: "d", Agent: child},
	})

	results := runAndGetAllToolResults(t, parentDispatchOnce("b1"), dispatchTool)
	rb := results["b1"]
	if rb == nil {
		t.Fatalf("expected a tool result for b1; got nil")
	}
	got := textOf(rb)
	t.Logf("scenario B result (IsError=%v): %q", rb.IsError, got)

	if !strings.Contains(got, "looking at the repo") {
		t.Errorf("scenario B: expected earlier text 'looking at the repo' to survive, got %q", got)
	}
}

// Scenario C — child 全程只产 thinking（reasoning），从不产 ContentDelta。
// reasoning provider（DeepSeek-R1 / Anthropic extended thinking）允许模型
// 把最终结论直接写在 thinking 流里。
//
// 行为：lastAssistantText 找不到任何 TextBlock 时回退到 ThinkingBlock，
// 父 agent 拿到的是 thinking 文本而不是 "no content"。
func TestRepro_NoContent_ThinkingOnly(t *testing.T) {
	childMock := providertest.New()
	childMock.QueueStream(
		provider.StreamChunk{ThinkingDelta: &provider.ThinkingDelta{Text: "after analyzing the directory I concluded X"}},
		provider.StreamChunk{FinishReason: provider.FinishStop},
	)

	child := agent.New(childMock)
	dispatchTool := subagent.NewTool("sub_agent", "dispatch", []subagent.Entry{
		{Type: "explore", Description: "d", Agent: child},
	})

	results := runAndGetAllToolResults(t, parentDispatchOnce("c1"), dispatchTool)
	rb := results["c1"]
	if rb == nil {
		t.Fatalf("expected a tool result for c1; got nil")
	}
	got := textOf(rb)
	t.Logf("scenario C result (IsError=%v): %q", rb.IsError, got)

	if !strings.Contains(got, "after analyzing the directory I concluded X") {
		t.Errorf("scenario C: expected thinking text to surface as fallback, got %q", got)
	}
}

// 回归：thinking 仅作为 fallback；同一条消息里 TextBlock 存在时优先 TextBlock，
// 不能因为有 ThinkingBlock 就把它和 text 拼起来回给父。
func TestRepro_TextWinsOverThinking(t *testing.T) {
	childMock := providertest.New()
	childMock.QueueStream(
		provider.StreamChunk{ThinkingDelta: &provider.ThinkingDelta{Text: "internal reasoning"}},
		provider.StreamChunk{ContentDelta: "final answer"},
		provider.StreamChunk{FinishReason: provider.FinishStop},
	)

	child := agent.New(childMock)
	dispatchTool := subagent.NewTool("sub_agent", "dispatch", []subagent.Entry{
		{Type: "explore", Description: "d", Agent: child},
	})

	results := runAndGetAllToolResults(t, parentDispatchOnce("d1"), dispatchTool)
	rb := results["d1"]
	if rb == nil {
		t.Fatalf("expected a tool result for d1; got nil")
	}
	got := textOf(rb)
	if got != "final answer" {
		t.Errorf("text-wins regression: got %q, want 'final answer' (thinking must not leak when text exists)", got)
	}
}
