package agent_test

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	agent "github.com/cago-frame/agents/agent"
	"github.com/cago-frame/agents/provider/providertest"
)

func TestDispatcher_RunnerOnEvent_Fires(t *testing.T) {
	a := agent.New(providertest.New())
	conv := agent.NewConversation()
	r := a.Runner(conv)
	defer func() { _ = r.Close() }()

	var count atomic.Int32
	unsub := r.OnEvent(agent.AnyEvent, func(_ context.Context, _ agent.Event) {
		count.Add(1)
	})
	defer unsub()

	events, err := r.Send(context.Background(), "hi")
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	for range events {
	}

	// At minimum: EventDone fired through dispatcher.
	if count.Load() == 0 {
		t.Fatalf("expected at least 1 OnEvent invocation, got 0")
	}
}

func TestDispatcher_AgentOnEvent_FiresBeforeRunner(t *testing.T) {
	var order []string
	prov := providertest.New()
	a := agent.New(prov, agent.OnEvent(agent.AnyEvent, func(_ context.Context, _ agent.Event) {
		order = append(order, "agent")
	}))
	conv := agent.NewConversation()
	r := a.Runner(conv)
	defer func() { _ = r.Close() }()

	r.OnEvent(agent.AnyEvent, func(_ context.Context, _ agent.Event) {
		order = append(order, "runner")
	})

	events, _ := r.Send(context.Background(), "hi")
	for range events {
	}

	// Agent OnEvent for EventDone must precede Runner OnEvent.
	if len(order) < 2 {
		t.Fatalf("expected both agent + runner observers to fire, got %v", order)
	}
	if order[0] != "agent" {
		t.Fatalf("agent should fire first, order=%v", order)
	}
}

func TestDispatcher_OnEventFilter(t *testing.T) {
	a := agent.New(providertest.New())
	conv := agent.NewConversation()
	r := a.Runner(conv)
	defer func() { _ = r.Close() }()

	var hits atomic.Int32
	r.OnEvent(agent.OnlyKinds(agent.EventTextDelta), func(_ context.Context, _ agent.Event) {
		hits.Add(1)
	})

	events, _ := r.Send(context.Background(), "hi")
	for range events {
	}
	// Stub Send only emits EventDone, which is not in the filter.
	if hits.Load() != 0 {
		t.Fatalf("filtered observer should not fire")
	}

	_ = time.Now() // keep import
}
