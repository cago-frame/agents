package agent_test

import (
	"context"
	"testing"

	agent "github.com/cago-frame/agents/agent"
	"github.com/cago-frame/agents/provider/providertest"
)

func TestAgent_NewWithOptions(t *testing.T) {
	prov := providertest.New()
	a := agent.New(prov,
		agent.System("you are helpful"),
		agent.Tools(),
		agent.Model("gpt-4o-mini"),
		agent.MaxSteps(10),
	)
	if a == nil {
		t.Fatalf("New returned nil")
	}
	if a.Provider() != prov {
		t.Fatalf("provider not stored")
	}
	if len(a.Tools()) != 0 {
		t.Fatalf("tools should be empty, got %d", len(a.Tools()))
	}
}

func TestAgent_Compose(t *testing.T) {
	prov := providertest.New()
	bundle := agent.Compose(
		agent.System("a"),
		agent.AppendSystem("b"),
	)
	a := agent.New(prov, bundle)
	if a == nil {
		t.Fatalf("New returned nil")
	}
}

func TestAgent_OnRunnerStart_Registered(t *testing.T) {
	prov := providertest.New()
	var seen bool
	a := agent.New(prov, agent.OnRunnerStart(func(ctx context.Context, r *agent.Runner) error {
		seen = true
		return nil
	}))
	// Runner not yet implemented; this test only verifies Option compiles & does not panic.
	_ = a
	_ = seen
}

func TestAgent_OnRunnerEnd_Registered(t *testing.T) {
	var called bool
	a := agent.New(providertest.New(),
		agent.OnRunnerEnd(func(ctx context.Context, r *agent.Runner) {
			called = true
		}),
	)
	if a == nil {
		t.Fatalf("New returned nil")
	}
	// Option registered successfully; exact invocation tested in runner tests.
	_ = called
}
