package openai

import (
	"encoding/json"
	"testing"

	openai "github.com/sashabaranov/go-openai"

	"github.com/cago-frame/agents/provider"
)

func TestBuildRequest_PropagatesNewFields(t *testing.T) {
	p := &Provider{}
	topP := float32(0.9)
	seed := 42
	req := &provider.CompletionRequest{
		Model:         "gpt-5.5",
		Messages:      []provider.Message{{Role: provider.RoleUser, Content: "hi"}},
		TopP:          &topP,
		StopSequences: []string{"\n\nUser:", "<end>"},
		Seed:          &seed,
		ResponseFormat: &provider.ResponseFormat{
			Type: provider.ResponseFormatJSONObject,
		},
	}

	got, err := p.buildRequest(req, false)
	if err != nil {
		t.Fatalf("buildRequest: %v", err)
	}

	if got.TopP != 0.9 {
		t.Errorf("TopP: want 0.9, got %v", got.TopP)
	}
	if len(got.Stop) != 2 || got.Stop[0] != "\n\nUser:" || got.Stop[1] != "<end>" {
		t.Errorf("Stop: want [\\n\\nUser:, <end>], got %v", got.Stop)
	}
	if got.Seed == nil || *got.Seed != 42 {
		t.Errorf("Seed: want 42, got %v", got.Seed)
	}
	if got.ResponseFormat == nil || got.ResponseFormat.Type != openai.ChatCompletionResponseFormatTypeJSONObject {
		t.Errorf("ResponseFormat type: want json_object, got %#v", got.ResponseFormat)
	}
}

func TestBuildRequest_ResponseFormatJSONSchema(t *testing.T) {
	p := &Provider{}
	schema := json.RawMessage(`{"type":"object","properties":{"x":{"type":"integer"}}}`)
	req := &provider.CompletionRequest{
		Model:    "gpt-5.5",
		Messages: []provider.Message{{Role: provider.RoleUser, Content: "hi"}},
		ResponseFormat: &provider.ResponseFormat{
			Type: provider.ResponseFormatJSONSchema,
			Schema: &provider.ResponseSchema{
				Name:   "answer",
				Schema: schema,
				Strict: true,
			},
		},
	}

	got, err := p.buildRequest(req, false)
	if err != nil {
		t.Fatalf("buildRequest: %v", err)
	}
	if got.ResponseFormat == nil {
		t.Fatal("ResponseFormat: nil")
	}
	if got.ResponseFormat.Type != openai.ChatCompletionResponseFormatTypeJSONSchema {
		t.Errorf("Type: want json_schema, got %v", got.ResponseFormat.Type)
	}
	if got.ResponseFormat.JSONSchema == nil {
		t.Fatal("JSONSchema: nil")
	}
	if got.ResponseFormat.JSONSchema.Name != "answer" {
		t.Errorf("Name: want answer, got %q", got.ResponseFormat.JSONSchema.Name)
	}
	if !got.ResponseFormat.JSONSchema.Strict {
		t.Error("Strict: want true")
	}
}

func TestBuildRequest_ToolChoiceUnknownTypeReturnsError(t *testing.T) {
	p := &Provider{}
	req := &provider.CompletionRequest{
		Model:      "gpt-5.5",
		Messages:   []provider.Message{{Role: provider.RoleUser, Content: "hi"}},
		ToolChoice: &provider.ToolChoice{Type: "totally_made_up"},
	}
	if _, err := p.buildRequest(req, false); err == nil {
		t.Error("expected error on unknown tool_choice type, got nil")
	}
}

func TestBuildRequest_ToolChoiceToolWithoutNameReturnsError(t *testing.T) {
	p := &Provider{}
	req := &provider.CompletionRequest{
		Model:      "gpt-5.5",
		Messages:   []provider.Message{{Role: provider.RoleUser, Content: "hi"}},
		ToolChoice: &provider.ToolChoice{Type: "tool"}, // missing Name
	}
	if _, err := p.buildRequest(req, false); err == nil {
		t.Error("expected error when tool_choice type=tool has empty Name, got nil")
	}
}

func TestBuildRequest_OmitsUnsetFields(t *testing.T) {
	p := &Provider{}
	req := &provider.CompletionRequest{
		Model:    "gpt-5.5",
		Messages: []provider.Message{{Role: provider.RoleUser, Content: "hi"}},
	}
	got, err := p.buildRequest(req, false)
	if err != nil {
		t.Fatalf("buildRequest: %v", err)
	}
	if got.TopP != 0 {
		t.Errorf("TopP: want zero, got %v", got.TopP)
	}
	if got.Stop != nil {
		t.Errorf("Stop: want nil, got %v", got.Stop)
	}
	if got.Seed != nil {
		t.Errorf("Seed: want nil, got %v", got.Seed)
	}
	if got.ResponseFormat != nil {
		t.Errorf("ResponseFormat: want nil, got %v", got.ResponseFormat)
	}
}
