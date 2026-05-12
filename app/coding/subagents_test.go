package coding_test

import (
	"strings"
	"testing"

	"github.com/cago-frame/agents/app/coding"
	"github.com/cago-frame/agents/provider/providertest"
)

func TestExplore_Defaults(t *testing.T) {
	mock := providertest.New()
	entry := coding.Explore(mock, ".")
	// Phase 3: agent has no Close method; no cleanup needed.

	if entry.Type != "explore" {
		t.Errorf("Type = %q, want %q", entry.Type, "explore")
	}
	if entry.Description == "" {
		t.Error("Description must not be empty")
	}
	if strings.Contains(entry.Description, "\n") {
		t.Errorf("Description must be single-line, got: %q", entry.Description)
	}
	if entry.Agent == nil {
		t.Fatal("Agent must not be nil")
	}
}

func TestPlan_Defaults(t *testing.T) {
	mock := providertest.New()
	entry := coding.Plan(mock, ".")
	// Phase 3: agent has no Close method; no cleanup needed.

	if entry.Type != "plan" {
		t.Errorf("Type = %q, want %q", entry.Type, "plan")
	}
	if entry.Agent == nil {
		t.Fatal("Agent must not be nil")
	}
}

func TestExplore_OptionsOverride(t *testing.T) {
	mock := providertest.New()
	entry := coding.Explore(mock, ".",
		coding.SubagentWithType("explore-custom"),
		coding.SubagentWithDescription("custom desc"),
		coding.SubagentWithSystem("custom system"),
		coding.SubagentWithModel("model-x"),
	)
	// Phase 3: agent has no Close method; no cleanup needed.

	if entry.Type != "explore-custom" {
		t.Errorf("Type = %q, want explore-custom", entry.Type)
	}
	if entry.Description != "custom desc" {
		t.Errorf("Description = %q", entry.Description)
	}
}

// SubagentWithSystem("") 必须真正清空默认 system（区分"未设"和"显式空"）。
func TestExplore_SystemEmptyClearsDefault(t *testing.T) {
	mock := providertest.New()
	entry := coding.Explore(mock, ".", coding.SubagentWithSystem(""))
	// Phase 3: agent has no Close method; no cleanup needed.
	if entry.Agent == nil {
		t.Fatal("Agent nil")
	}
}

func TestGeneralPurpose_Defaults(t *testing.T) {
	mock := providertest.New()
	entry := coding.GeneralPurpose(mock, ".")
	// Phase 3: agent has no Close method; no cleanup needed.

	if entry.Type != "general-purpose" {
		t.Errorf("Type = %q, want general-purpose", entry.Type)
	}
	if entry.Description == "" {
		t.Error("Description must not be empty")
	}
	if entry.Agent == nil {
		t.Fatal("Agent must not be nil")
	}
}
