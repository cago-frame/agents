package subagent_test

import (
	"context"
	"sync"
	"testing"

	. "github.com/smartystreets/goconvey/convey"

	"github.com/cago-frame/agents/agent"
	"github.com/cago-frame/agents/provider"
	"github.com/cago-frame/agents/provider/providertest"
	"github.com/cago-frame/agents/tool/subagent"
)

func TestScenario_SubagentIsolation(t *testing.T) {
	Convey("Feature: 父 agent 调度 subagent 时事件流隔离", t, func() {
		// child agent
		childMock := providertest.New().QueueStream(
			provider.StreamChunk{ContentDelta: "[child] "},
			provider.StreamChunk{ContentDelta: "found helloWorld at src/main.go:12"},
			provider.StreamChunk{FinishReason: provider.FinishStop},
		)
		var childEvtsMu sync.Mutex
		var childTextEvts int
		var childTurnEndEvts int

		childAgent := agent.New(childMock,
			agent.System("child"),
			agent.OnEvent(agent.OnlyKinds(agent.EventTextDelta), func(_ context.Context, _ agent.Event) {
				childEvtsMu.Lock()
				defer childEvtsMu.Unlock()
				childTextEvts++
			}),
			agent.OnEvent(agent.OnlyKinds(agent.EventTurnEnd), func(_ context.Context, _ agent.Event) {
				childEvtsMu.Lock()
				defer childEvtsMu.Unlock()
				childTurnEndEvts++
			}),
		)

		dispatchTool := subagent.NewTool(
			"dispatch_subagent",
			"委派给 child",
			[]subagent.Entry{{Type: "explore_repo", Description: "explore", Agent: childAgent}},
		)

		parentMock := providertest.New().
			QueueStream(
				provider.StreamChunk{ToolCallDelta: &provider.ToolCallDelta{Index: 0, ID: "tu-sub-1", Name: "dispatch_subagent"}},
				provider.StreamChunk{ToolCallDelta: &provider.ToolCallDelta{Index: 0, ArgsDelta: `{"type":"explore_repo","prompt":"find helloWorld"}`}},
				provider.StreamChunk{FinishReason: provider.FinishToolCalls},
			).
			QueueStream(
				provider.StreamChunk{ContentDelta: "[parent] done."},
				provider.StreamChunk{FinishReason: provider.FinishStop},
			)
		parent := agent.New(parentMock, agent.System("parent"), agent.Tools(dispatchTool))
		conv := agent.NewConversation()
		r := parent.Runner(conv)
		t.Cleanup(func() { _ = r.Close() })

		events, err := r.Send(context.Background(), "find helloWorld")
		So(err, ShouldBeNil)

		var parentTextEvts int
		var parentSawDispatchPre bool
		for ev := range events {
			switch ev.Kind {
			case agent.EventTextDelta:
				parentTextEvts++
			case agent.EventPreToolUse:
				if ev.Tool != nil && ev.Tool.Name == "dispatch_subagent" {
					parentSawDispatchPre = true
				}
			}
		}

		So(parentSawDispatchPre, ShouldBeTrue)
		So(parentTextEvts, ShouldBeGreaterThan, 0)

		childEvtsMu.Lock()
		defer childEvtsMu.Unlock()
		So(childTextEvts, ShouldBeGreaterThanOrEqualTo, 1)
		So(childTurnEndEvts, ShouldEqual, 1)
	})
}
