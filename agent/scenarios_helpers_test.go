package agent_test

import (
	"context"
	"iter"
	"testing"

	"github.com/cago-frame/agents/agent"
	"github.com/cago-frame/agents/provider"
)

// mustSend wraps r.Send and t.Fatalf's on error. Use this in scenario tests
// where Send is not expected to fail at the API boundary (the failure modes
// you actually want to test surface as Events, not as Send error).
func mustSend(t *testing.T, r *agent.Runner, text string, opts ...agent.SendOption) iter.Seq[agent.Event] {
	t.Helper()
	events, err := r.Send(context.Background(), text, opts...)
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	return events
}

// setupAgent 创建 agent + conv + runner 的常用三件套，注册自动 cleanup。
func setupAgent(t *testing.T, prov provider.Provider, opts ...agent.Option) (*agent.Agent, *agent.Conversation, *agent.Runner) {
	t.Helper()
	a := agent.New(prov, opts...)
	conv := agent.NewConversation()
	r := a.Runner(conv)
	t.Cleanup(func() { _ = r.Close() })
	return a, conv, r
}

// drainEvents 把 iter.Seq[Event] 同步耗成 slice，方便顺序断言。
func drainEvents(events iter.Seq[agent.Event]) []agent.Event {
	var out []agent.Event
	for ev := range events {
		out = append(out, ev)
	}
	return out
}

// findEvent 返回第一条 Kind 命中的 Event；未找到返回 false。
func findEvent(events []agent.Event, kind agent.EventKind) (agent.Event, bool) {
	for _, ev := range events {
		if ev.Kind == kind {
			return ev, true
		}
	}
	return agent.Event{}, false
}

// lastMessage 返回 conv 末条快照；conv 为空时 t.Fatalf。
func lastMessage(t *testing.T, conv *agent.Conversation) agent.Message {
	t.Helper()
	if conv.Len() == 0 {
		t.Fatalf("conv is empty")
	}
	m, _ := conv.MessageAt(conv.Len() - 1)
	return m
}
