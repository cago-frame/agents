package agent_test

import (
	"testing"

	"github.com/cago-frame/agents/agent"
	"github.com/cago-frame/agents/agent/blocks"
)

// The agent package re-exports concrete block types from agent/blocks as
// type aliases. This file pins the identity guarantees the rest of the
// codebase relies on:
//
//   - every block type implements the ContentBlock interface;
//   - the alias and the underlying block share Type() and Audience()
//     (alias identity, not a wrapped struct);
//   - audience flags are the canonical filter for the built-in projections.

func TestAliases_ShareTypeIdentityWithBlocks(t *testing.T) {
	// Compile-time: the alias *is* the underlying type. The two-way
	// assignments below would not type-check if the alias relationship ever
	// broke (e.g. if someone reintroduced a wrapper struct).
	var (
		fromBlocks = blocks.TextBlock{Text: "from-blocks"}
		viaAgent   agent.TextBlock
		fromAgent  = agent.DisplayTextBlock{Text: "from-agent"}
		viaBlocks  blocks.DisplayTextBlock
	)
	viaAgent = fromBlocks
	viaBlocks = fromAgent
	if viaAgent.Text != "from-blocks" || viaBlocks.Text != "from-agent" {
		t.Fatal("alias and underlying type diverged")
	}
}

func TestAliases_ImplementContentBlock(t *testing.T) {
	checks := []agent.ContentBlock{
		agent.TextBlock{},
		agent.ImageBlock{},
		agent.ToolUseBlock{},
		agent.ToolResultBlock{},
		agent.ThinkingBlock{},
		agent.DisplayTextBlock{},
		agent.RefBlock{},
		agent.NoticeBlock{},
		agent.SummaryBlock{},
	}
	for _, b := range checks {
		if b.Type() == "" {
			t.Errorf("%T has empty Type()", b)
		}
		if b.Audience() == 0 {
			t.Errorf("%T has zero Audience() — must declare at least one consumer", b)
		}
	}
}

func TestAudienceConstants_MatchBlocksPackage(t *testing.T) {
	if agent.AudienceLLM != blocks.ToLLM {
		t.Fatal("agent.AudienceLLM diverged from blocks.ToLLM")
	}
	if agent.AudienceUI != blocks.ToUI {
		t.Fatal("agent.AudienceUI diverged from blocks.ToUI")
	}
}

func TestDisplayTextBlock_NotForLLM(t *testing.T) {
	if (agent.DisplayTextBlock{}).Audience().Has(agent.AudienceLLM) {
		t.Fatal("DisplayTextBlock must be invisible to the LLM")
	}
}
