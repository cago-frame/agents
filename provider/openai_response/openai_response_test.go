package openai_response

import (
	"encoding/json"
	"testing"

	"github.com/openai/openai-go/responses"
	"github.com/openai/openai-go/shared"

	"github.com/cago-frame/agents/provider"
)

// marshalParams 走 SDK 的 MarshalJSON 拿到真正会发出去的 body，避免直接断言
// param.Opt 等内部封装。然后 json.Unmarshal 回 map 让用例做 shape 断言。
func marshalParams(t *testing.T, params responses.ResponseNewParams) map[string]any {
	t.Helper()
	raw, err := json.Marshal(params)
	if err != nil {
		t.Fatalf("marshal params: %v", err)
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("unmarshal params: %v", err)
	}
	return out
}

func TestBuildRequest_SystemBecomesInstructions(t *testing.T) {
	p := &Provider{}
	got, err := p.buildRequest(&provider.CompletionRequest{
		Model: "gpt-5.5",
		Messages: []provider.Message{
			{Role: provider.RoleSystem, Content: "be helpful"},
			{Role: provider.RoleSystem, Content: "answer in english"},
			{Role: provider.RoleUser, Content: "hi"},
		},
	})
	if err != nil {
		t.Fatalf("buildRequest: %v", err)
	}
	body := marshalParams(t, got)
	if body["instructions"] != "be helpful\n\nanswer in english" {
		t.Errorf("instructions = %v", body["instructions"])
	}
	input, _ := body["input"].([]any)
	if len(input) != 1 {
		t.Fatalf("input len = %d, want 1", len(input))
	}
	first := input[0].(map[string]any)
	if first["role"] != "user" {
		t.Errorf("first input = %#v", first)
	}
}

func TestBuildRequest_ToolsFlatShape(t *testing.T) {
	p := &Provider{}
	req := &provider.CompletionRequest{
		Model:    "gpt-5.5",
		Messages: []provider.Message{{Role: provider.RoleUser, Content: "hi"}},
		Tools: []provider.Tool{{
			Type: provider.ToolTypeFunction,
			Function: &provider.FunctionDefinition{
				Name:        "add",
				Description: "add two numbers",
				Parameters:  json.RawMessage(`{"type":"object","properties":{"a":{"type":"integer"}}}`),
			},
		}},
	}
	got, err := p.buildRequest(req)
	if err != nil {
		t.Fatalf("buildRequest: %v", err)
	}
	body := marshalParams(t, got)
	tools, _ := body["tools"].([]any)
	if len(tools) != 1 {
		t.Fatalf("tools len = %d", len(tools))
	}
	tool := tools[0].(map[string]any)
	// Responses API 的 tool 是扁平 {type, name, description, parameters}，
	// 不是 chat 的 {type, function:{...}}。
	if tool["type"] != "function" || tool["name"] != "add" || tool["description"] != "add two numbers" {
		t.Errorf("tool shape = %#v", tool)
	}
	if _, hasFunctionWrapper := tool["function"]; hasFunctionWrapper {
		t.Errorf("tool should not have nested 'function' wrapper, got %#v", tool)
	}
	params, _ := tool["parameters"].(map[string]any)
	if params["type"] != "object" {
		t.Errorf("parameters = %#v", params)
	}
}

func TestBuildRequest_ToolsBackfillProperties(t *testing.T) {
	p := &Provider{}
	req := &provider.CompletionRequest{
		Model:    "gpt-5.5",
		Messages: []provider.Message{{Role: provider.RoleUser, Content: "hi"}},
		Tools: []provider.Tool{{
			Type: provider.ToolTypeFunction,
			Function: &provider.FunctionDefinition{
				Name:       "noop",
				Parameters: json.RawMessage(`{"type":"object"}`),
			},
		}},
	}
	got, err := p.buildRequest(req)
	if err != nil {
		t.Fatalf("buildRequest: %v", err)
	}
	body := marshalParams(t, got)
	tools, _ := body["tools"].([]any)
	tool := tools[0].(map[string]any)
	params, _ := tool["parameters"].(map[string]any)
	if _, ok := params["properties"]; !ok {
		t.Errorf("properties should be backfilled, got %#v", params)
	}
}

func TestBuildRequest_ToolChoiceVariants(t *testing.T) {
	cases := []struct {
		name string
		in   *provider.ToolChoice
		want any
	}{
		{"auto", &provider.ToolChoice{Type: "auto"}, "auto"},
		{"required", &provider.ToolChoice{Type: "required"}, "required"},
		{"none", &provider.ToolChoice{Type: "none"}, "none"},
		{
			"specific tool", &provider.ToolChoice{Type: "tool", Name: "lookup"},
			map[string]any{"type": "function", "name": "lookup"},
		},
	}
	p := &Provider{}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := p.buildRequest(&provider.CompletionRequest{
				Model:      "gpt-5.5",
				Messages:   []provider.Message{{Role: provider.RoleUser, Content: "hi"}},
				ToolChoice: c.in,
			})
			if err != nil {
				t.Fatalf("buildRequest: %v", err)
			}
			body := marshalParams(t, got)
			tc := body["tool_choice"]
			switch want := c.want.(type) {
			case string:
				if tc != want {
					t.Errorf("tool_choice = %#v, want %q", tc, want)
				}
			case map[string]any:
				m, _ := tc.(map[string]any)
				if m["type"] != want["type"] || m["name"] != want["name"] {
					t.Errorf("tool_choice = %#v, want %#v", m, want)
				}
			}
		})
	}
}

func TestBuildRequest_ToolChoiceErrors(t *testing.T) {
	p := &Provider{}
	if _, err := p.buildRequest(&provider.CompletionRequest{
		Model:      "gpt-5.5",
		Messages:   []provider.Message{{Role: provider.RoleUser, Content: "hi"}},
		ToolChoice: &provider.ToolChoice{Type: "totally_made_up"},
	}); err == nil {
		t.Error("expected error on unknown tool_choice type")
	}
	if _, err := p.buildRequest(&provider.CompletionRequest{
		Model:      "gpt-5.5",
		Messages:   []provider.Message{{Role: provider.RoleUser, Content: "hi"}},
		ToolChoice: &provider.ToolChoice{Type: "tool"},
	}); err == nil {
		t.Error("expected error when tool_choice type=tool has empty Name")
	}
}

func TestBuildRequest_ReasoningEffortMapping(t *testing.T) {
	cases := map[provider.ThinkingEffort]shared.ReasoningEffort{
		provider.ThinkingLow:    shared.ReasoningEffortLow,
		provider.ThinkingMedium: shared.ReasoningEffortMedium,
		provider.ThinkingHigh:   shared.ReasoningEffortHigh,
		provider.ThinkingXHigh:  shared.ReasoningEffortHigh,
		provider.ThinkingMax:    shared.ReasoningEffortHigh,
	}
	p := &Provider{}
	for in, want := range cases {
		got, err := p.buildRequest(&provider.CompletionRequest{
			Model:    "gpt-5.5",
			Messages: []provider.Message{{Role: provider.RoleUser, Content: "hi"}},
			Thinking: &provider.ThinkingConfig{Effort: in},
		})
		if err != nil {
			t.Fatalf("buildRequest(%s): %v", in, err)
		}
		if got.Reasoning.Effort != want {
			t.Errorf("effort %s -> %s, want %s", in, got.Reasoning.Effort, want)
		}
	}
}

func TestBuildRequest_ResponseFormatJSONSchema(t *testing.T) {
	p := &Provider{}
	got, err := p.buildRequest(&provider.CompletionRequest{
		Model:    "gpt-5.5",
		Messages: []provider.Message{{Role: provider.RoleUser, Content: "hi"}},
		ResponseFormat: &provider.ResponseFormat{
			Type: provider.ResponseFormatJSONSchema,
			Schema: &provider.ResponseSchema{
				Name:        "answer",
				Description: "structured answer",
				Schema:      json.RawMessage(`{"type":"object","properties":{"x":{"type":"integer"}}}`),
				Strict:      true,
			},
		},
	})
	if err != nil {
		t.Fatalf("buildRequest: %v", err)
	}
	body := marshalParams(t, got)
	text, _ := body["text"].(map[string]any)
	format, _ := text["format"].(map[string]any)
	if format["type"] != "json_schema" {
		t.Errorf("format type = %v", format["type"])
	}
	if format["name"] != "answer" {
		t.Errorf("name = %v", format["name"])
	}
	if format["strict"] != true {
		t.Errorf("strict = %v", format["strict"])
	}
}

func TestBuildRequest_ResponseFormatJSONObject(t *testing.T) {
	p := &Provider{}
	got, err := p.buildRequest(&provider.CompletionRequest{
		Model:          "gpt-5.5",
		Messages:       []provider.Message{{Role: provider.RoleUser, Content: "hi"}},
		ResponseFormat: &provider.ResponseFormat{Type: provider.ResponseFormatJSONObject},
	})
	if err != nil {
		t.Fatalf("buildRequest: %v", err)
	}
	body := marshalParams(t, got)
	text, _ := body["text"].(map[string]any)
	format, _ := text["format"].(map[string]any)
	if format["type"] != "json_object" {
		t.Errorf("format type = %v", format["type"])
	}
}

func TestBuildRequest_AssistantWithToolCallAndToolResult(t *testing.T) {
	p := &Provider{}
	got, err := p.buildRequest(&provider.CompletionRequest{
		Model: "gpt-5.5",
		Messages: []provider.Message{
			{Role: provider.RoleUser, Content: "what is 1+2"},
			{
				Role:    provider.RoleAssistant,
				Content: "let me check",
				ToolCalls: []provider.ToolCall{{
					ID:   "call_1",
					Type: provider.ToolTypeFunction,
					Function: provider.ToolCallFunction{
						Name:      "add",
						Arguments: `{"a":1,"b":2}`,
					},
				}},
			},
			{Role: provider.RoleTool, ToolCallID: "call_1", Content: "3"},
		},
	})
	if err != nil {
		t.Fatalf("buildRequest: %v", err)
	}
	body := marshalParams(t, got)
	input, _ := body["input"].([]any)
	// user, assistant message, function_call, function_call_output → 4
	if len(input) != 4 {
		t.Fatalf("input len = %d, want 4 (got %#v)", len(input), input)
	}
	fc := input[2].(map[string]any)
	if fc["type"] != "function_call" || fc["name"] != "add" || fc["call_id"] != "call_1" {
		t.Errorf("function_call item = %#v", fc)
	}
	out := input[3].(map[string]any)
	if out["type"] != "function_call_output" || out["call_id"] != "call_1" || out["output"] != "3" {
		t.Errorf("function_call_output item = %#v", out)
	}
}

func TestBuildRequest_OmitsUnsetFields(t *testing.T) {
	p := &Provider{}
	got, err := p.buildRequest(&provider.CompletionRequest{
		Model:    "gpt-5.5",
		Messages: []provider.Message{{Role: provider.RoleUser, Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("buildRequest: %v", err)
	}
	body := marshalParams(t, got)
	for _, k := range []string{"temperature", "top_p", "max_output_tokens", "tools", "tool_choice", "reasoning", "text", "instructions"} {
		if _, ok := body[k]; ok {
			t.Errorf("expected %q to be omitted, got %v", k, body[k])
		}
	}
}

func TestBuildRequest_MultiModalImage(t *testing.T) {
	p := &Provider{}
	got, err := p.buildRequest(&provider.CompletionRequest{
		Model: "gpt-5.5",
		Messages: []provider.Message{{
			Role: provider.RoleUser,
			MultiContent: []provider.MessagePart{
				{Type: provider.MessagePartText, Text: "describe"},
				{Type: provider.MessagePartImage, Image: &provider.MessageImage{URL: "https://example.com/cat.png"}},
				{Type: provider.MessagePartImage, Image: &provider.MessageImage{Inline: []byte{0x1, 0x2}, MediaType: "image/jpeg"}},
			},
		}},
	})
	if err != nil {
		t.Fatalf("buildRequest: %v", err)
	}
	body := marshalParams(t, got)
	input, _ := body["input"].([]any)
	first := input[0].(map[string]any)
	content, _ := first["content"].([]any)
	if len(content) != 3 {
		t.Fatalf("content parts = %d, want 3 (got %#v)", len(content), content)
	}
	if content[0].(map[string]any)["type"] != "input_text" {
		t.Errorf("part0 = %#v", content[0])
	}
	img1 := content[1].(map[string]any)
	if img1["type"] != "input_image" || img1["image_url"] != "https://example.com/cat.png" {
		t.Errorf("img1 = %#v", img1)
	}
	img2 := content[2].(map[string]any)
	if img2["type"] != "input_image" {
		t.Errorf("img2 = %#v", img2)
	}
	url, _ := img2["image_url"].(string)
	if url == "" || url[:23] != "data:image/jpeg;base64," {
		t.Errorf("inline image url not base64-data-uri: %q", url)
	}
}
