package anthropics

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/cago-frame/agents/provider"
)

func TestBuildRequest_PropagatesTopPAndStopSequences(t *testing.T) {
	p := &Provider{}
	topP := float32(0.5) // 选 float32 可精确表示的值，避免 float32→float64 精度噪声
	req := &provider.CompletionRequest{
		Model:         "claude-sonnet-4-6",
		Messages:      []provider.Message{{Role: provider.RoleUser, Content: "hi"}},
		TopP:          &topP,
		StopSequences: []string{"\n\nHuman:", "<end>"},
	}

	got, err := p.buildRequest(req)
	if err != nil {
		t.Fatalf("buildRequest: %v", err)
	}

	if !got.TopP.Valid() {
		t.Fatal("TopP: want set, got unset")
	}
	if got.TopP.Value != 0.5 {
		t.Errorf("TopP: want 0.5, got %v", got.TopP.Value)
	}
	if len(got.StopSequences) != 2 ||
		got.StopSequences[0] != "\n\nHuman:" ||
		got.StopSequences[1] != "<end>" {
		t.Errorf("StopSequences: want [\\n\\nHuman:, <end>], got %v", got.StopSequences)
	}
}

func TestBuildRequest_IgnoresSeedAndResponseFormat(t *testing.T) {
	// Anthropic 不支持 seed / response_format，buildRequest 应静默忽略而不报错。
	p := &Provider{}
	seed := 42
	req := &provider.CompletionRequest{
		Model:    "claude-sonnet-4-6",
		Messages: []provider.Message{{Role: provider.RoleUser, Content: "hi"}},
		Seed:     &seed,
		ResponseFormat: &provider.ResponseFormat{
			Type: provider.ResponseFormatJSONObject,
		},
	}
	if _, err := p.buildRequest(req); err != nil {
		t.Fatalf("buildRequest: unexpected error %v", err)
	}
}

func TestBuildRequest_ToolChoiceUnknownTypeReturnsError(t *testing.T) {
	p := &Provider{}
	req := &provider.CompletionRequest{
		Model:      "claude-sonnet-4-6",
		Messages:   []provider.Message{{Role: provider.RoleUser, Content: "hi"}},
		ToolChoice: &provider.ToolChoice{Type: "totally_made_up"},
	}
	if _, err := p.buildRequest(req); err == nil {
		t.Error("expected error on unknown tool_choice type, got nil")
	}
}

func TestBuildRequest_ToolChoiceToolWithoutNameReturnsError(t *testing.T) {
	p := &Provider{}
	req := &provider.CompletionRequest{
		Model:      "claude-sonnet-4-6",
		Messages:   []provider.Message{{Role: provider.RoleUser, Content: "hi"}},
		ToolChoice: &provider.ToolChoice{Type: "tool"}, // missing Name
	}
	if _, err := p.buildRequest(req); err == nil {
		t.Error("expected error when tool_choice type=tool has empty Name, got nil")
	}
}

func TestBuildRequest_OmitsUnsetTopP(t *testing.T) {
	p := &Provider{}
	req := &provider.CompletionRequest{
		Model:    "claude-sonnet-4-6",
		Messages: []provider.Message{{Role: provider.RoleUser, Content: "hi"}},
	}
	got, err := p.buildRequest(req)
	if err != nil {
		t.Fatalf("buildRequest: %v", err)
	}
	if got.TopP.Valid() {
		t.Error("TopP: want unset, got set")
	}
	if len(got.StopSequences) != 0 {
		t.Errorf("StopSequences: want empty, got %v", got.StopSequences)
	}
}

func TestExtractUsage_SplitsCacheCreationFromPrompt(t *testing.T) {
	u := anthropic.MessageDeltaUsage{
		InputTokens:              50,
		CacheCreationInputTokens: 100,
		CacheReadInputTokens:     200,
		OutputTokens:             10,
	}
	got := extractUsageFromDelta(u)
	if got.PromptTokens != 50 {
		t.Errorf("PromptTokens = %d, want 50 (raw input only)", got.PromptTokens)
	}
	if got.CacheCreationTokens != 100 {
		t.Errorf("CacheCreationTokens = %d, want 100", got.CacheCreationTokens)
	}
	if got.CachedTokens != 200 {
		t.Errorf("CachedTokens = %d, want 200", got.CachedTokens)
	}
	if got.CompletionTokens != 10 {
		t.Errorf("CompletionTokens = %d, want 10", got.CompletionTokens)
	}
	if got.TotalTokens != 360 {
		t.Errorf("TotalTokens = %d, want 360 (50+100+200+10)", got.TotalTokens)
	}
}

func TestBuildRequest_AnnotatesLastSystemAndLastToolWithCacheControl(t *testing.T) {
	p := &Provider{}
	tool1 := provider.Tool{
		Type: provider.ToolTypeFunction,
		Function: &provider.FunctionDefinition{
			Name:        "first",
			Description: "first tool",
			Parameters:  json.RawMessage(`{"type":"object"}`),
		},
	}
	tool2 := provider.Tool{
		Type: provider.ToolTypeFunction,
		Function: &provider.FunctionDefinition{
			Name:        "second",
			Description: "second tool",
			Parameters:  json.RawMessage(`{"type":"object"}`),
		},
	}
	req := &provider.CompletionRequest{
		Model: "claude-3-5-sonnet-20241022",
		Messages: []provider.Message{
			{Role: provider.RoleSystem, Content: "you are helpful"},
			{Role: provider.RoleUser, Content: "hello"},
			{Role: provider.RoleAssistant, Content: "hi"},
			{Role: provider.RoleUser, Content: "what is 2+2"},
		},
		Tools: []provider.Tool{tool1, tool2},
	}
	params, err := p.buildRequest(req)
	if err != nil {
		t.Fatalf("buildRequest: %v", err)
	}

	if len(params.System) == 0 {
		t.Fatal("expected system block, got none")
	}
	if got := params.System[len(params.System)-1].CacheControl.Type; got != "ephemeral" {
		t.Errorf("last system block CacheControl.Type = %q, want ephemeral", got)
	}

	if len(params.Tools) == 0 {
		t.Fatal("expected tools, got none")
	}
	last := params.Tools[len(params.Tools)-1]
	if last.OfTool == nil {
		t.Fatalf("last tool union has no OfTool: %+v", last)
	}
	if got := last.OfTool.CacheControl.Type; got != "ephemeral" {
		t.Errorf("last tool CacheControl.Type = %q, want ephemeral", got)
	}

	// Earlier tool should NOT be tagged.
	if len(params.Tools) >= 2 {
		first := params.Tools[0]
		if first.OfTool != nil && first.OfTool.CacheControl.Type == "ephemeral" {
			t.Errorf("first tool unexpectedly tagged ephemeral")
		}
	}
}

func TestBuildRequest_NoPanicWithNoSystemOrTools(t *testing.T) {
	p := &Provider{}
	req := &provider.CompletionRequest{
		Model: "claude-3-5-sonnet-20241022",
		Messages: []provider.Message{
			{Role: provider.RoleUser, Content: "hi"},
		},
	}
	params, err := p.buildRequest(req)
	if err != nil {
		t.Fatalf("buildRequest with empty system/tools: %v", err)
	}
	if len(params.System) != 0 {
		t.Errorf("expected empty System, got %d entries", len(params.System))
	}
	if len(params.Tools) != 0 {
		t.Errorf("expected empty Tools, got %d entries", len(params.Tools))
	}
}

// TestBuildRequest_MergesConsecutiveAssistantThinkingAndToolUse 回归测试：
// 当模型一次回合返回 thinking + tool_use 时，agent 层会把这条 assistant 拆成
// 两条 provider.Message（一条携带 Thinking，一条携带 ToolCalls）。Anthropic
// Messages API 要求 thinking 块必须与同回合的 tool_use 处于同一条 assistant
// 消息内（缺失则 400 "The content[].thinking in the thinking mode must be
// passed back to the API"）。buildRequest 必须把连续的 assistant
// provider.Message 合并成一条 anthropic.MessageParam，使得 thinking + tool_use
// 处于同一 turn。
func TestBuildRequest_MergesConsecutiveAssistantThinkingAndToolUse(t *testing.T) {
	p := &Provider{}
	req := &provider.CompletionRequest{
		Model: "claude-opus-4-7",
		Messages: []provider.Message{
			{Role: provider.RoleUser, Content: "list assets"},
			// agent 拆出来的"text+thinking"（Content 可空，仅承载 Thinking）
			{
				Role:    provider.RoleAssistant,
				Content: "",
				Thinking: []provider.ThinkingBlock{
					{Text: "I should call list_assets", Signature: "sig-xyz"},
				},
			},
			// agent 拆出来的 tool_use（同回合）
			{
				Role: provider.RoleAssistant,
				ToolCalls: []provider.ToolCall{{
					ID:   "call_1",
					Type: provider.ToolTypeFunction,
					Function: provider.ToolCallFunction{
						Name:      "list_assets",
						Arguments: `{}`,
					},
				}},
			},
			{
				Role:       provider.RoleTool,
				ToolCallID: "call_1",
				Content:    `{"result":[]}`,
			},
		},
	}

	params, err := p.buildRequest(req)
	if err != nil {
		t.Fatalf("buildRequest: %v", err)
	}

	// 期望: user → assistant(thinking+tool_use) → user(tool_result)
	if len(params.Messages) != 3 {
		roles := make([]string, 0, len(params.Messages))
		for _, m := range params.Messages {
			roles = append(roles, string(m.Role))
		}
		t.Fatalf("params.Messages length = %d, want 3 (got roles=%v)", len(params.Messages), roles)
	}

	if params.Messages[0].Role != anthropic.MessageParamRoleUser {
		t.Errorf("msg[0].Role = %q, want user", params.Messages[0].Role)
	}

	asst := params.Messages[1]
	if asst.Role != anthropic.MessageParamRoleAssistant {
		t.Fatalf("msg[1].Role = %q, want assistant", asst.Role)
	}
	// 同一 assistant turn 必须同时包含 thinking 与 tool_use 块。
	var hasThinking, hasToolUse bool
	for _, b := range asst.Content {
		if b.OfThinking != nil {
			hasThinking = true
			if b.OfThinking.Signature != "sig-xyz" {
				t.Errorf("thinking signature = %q, want sig-xyz", b.OfThinking.Signature)
			}
		}
		if b.OfToolUse != nil {
			hasToolUse = true
			if b.OfToolUse.ID != "call_1" {
				t.Errorf("tool_use ID = %q, want call_1", b.OfToolUse.ID)
			}
		}
	}
	if !hasThinking {
		t.Error("merged assistant message missing thinking block — Anthropic API will reject the next round with 400")
	}
	if !hasToolUse {
		t.Error("merged assistant message missing tool_use block")
	}

	// thinking 必须排在 tool_use 之前（API 要求 signature 验证顺序）。
	thinkingIdx, toolUseIdx := -1, -1
	for i, b := range asst.Content {
		if b.OfThinking != nil && thinkingIdx == -1 {
			thinkingIdx = i
		}
		if b.OfToolUse != nil && toolUseIdx == -1 {
			toolUseIdx = i
		}
	}
	if thinkingIdx >= 0 && toolUseIdx >= 0 && thinkingIdx > toolUseIdx {
		t.Errorf("thinking block at idx %d must precede tool_use at idx %d", thinkingIdx, toolUseIdx)
	}

	if params.Messages[2].Role != anthropic.MessageParamRoleUser {
		t.Errorf("msg[2].Role = %q, want user (tool_result)", params.Messages[2].Role)
	}
}

// TestBuildThinking_EffortToBudget 覆盖四档 effort → token budget 的映射，
// 同时验证 budget 必须严格 < maxTokens 的截断 + < 1024 关闭 thinking 的兜底。
func TestBuildThinking_EffortToBudget(t *testing.T) {
	const bigMaxTokens int64 = 64000

	tests := []struct {
		name       string
		effort     provider.ThinkingEffort
		maxTokens  int64
		wantBudget int64 // 0 表示期望 disabled
	}{
		{"low", provider.ThinkingLow, bigMaxTokens, 1024},
		{"medium", provider.ThinkingMedium, bigMaxTokens, 4096},
		{"high", provider.ThinkingHigh, bigMaxTokens, 16000},
		{"xhigh", provider.ThinkingXHigh, bigMaxTokens, 32000},
		{"max", provider.ThinkingMax, 80000, 64000},
		{"unknown effort falls back to low", provider.ThinkingEffort("totally-bogus"), bigMaxTokens, 1024},
		// budget 截断到 maxTokens-1
		{"xhigh clipped by smaller maxTokens", provider.ThinkingXHigh, 5000, 4999},
		// budget < 1024 时关闭 thinking（设 maxTokens=512：xhigh 被截到 511，触发 disabled）
		{"clipped below 1024 disables thinking", provider.ThinkingXHigh, 512, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &provider.ThinkingConfig{Effort: tt.effort}
			got := buildThinking(cfg, tt.maxTokens)

			if tt.wantBudget == 0 {
				if got.OfDisabled == nil {
					t.Fatalf("expected OfDisabled set, got %+v", got)
				}
				return
			}
			if got.OfEnabled == nil {
				t.Fatalf("expected OfEnabled set, got %+v", got)
			}
			if got.OfEnabled.BudgetTokens != tt.wantBudget {
				t.Errorf("BudgetTokens = %d, want %d", got.OfEnabled.BudgetTokens, tt.wantBudget)
			}
		})
	}
}

func TestChatCompletion_429ReturnsProviderError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Retry-After", "7")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"type":"error","error":{"type":"rate_limit_error","message":"slow down"}}`))
	}))
	defer srv.Close()

	noRetry := 0
	p := NewProvider(Config{APIKey: "test", BaseURL: srv.URL, MaxRetries: &noRetry})
	req := &provider.CompletionRequest{
		Model:    "claude-3-5-sonnet-20241022",
		Messages: []provider.Message{{Role: provider.RoleUser, Content: "hi"}},
	}
	_, err := p.ChatCompletion(context.Background(), req)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	var pe *provider.ProviderError
	if !errors.As(err, &pe) {
		t.Fatalf("expected *provider.ProviderError, got %T: %v", err, err)
	}
	if pe.StatusCode != http.StatusTooManyRequests {
		t.Errorf("StatusCode = %d, want 429", pe.StatusCode)
	}
	if pe.RetryAfter != "7" {
		t.Errorf("RetryAfter = %q, want \"7\"", pe.RetryAfter)
	}
}

func TestChatStream_429ReturnsProviderError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Retry-After", "11")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"type":"error","error":{"type":"rate_limit_error","message":"slow down"}}`))
	}))
	defer srv.Close()

	noRetry := 0
	p := NewProvider(Config{APIKey: "test", BaseURL: srv.URL, MaxRetries: &noRetry})
	req := &provider.CompletionRequest{
		Model:    "claude-3-5-sonnet-20241022",
		Messages: []provider.Message{{Role: provider.RoleUser, Content: "hi"}},
	}
	ch, err := p.ChatStream(context.Background(), req)
	if err != nil {
		t.Fatalf("ChatStream returned non-nil err on setup: %v", err)
	}
	var lastErr error
	for chunk := range ch {
		if chunk.Err != nil {
			lastErr = chunk.Err
		}
	}
	if lastErr == nil {
		t.Fatal("expected an error chunk, got none")
	}
	var pe *provider.ProviderError
	if !errors.As(lastErr, &pe) {
		t.Fatalf("expected *provider.ProviderError, got %T: %v", lastErr, lastErr)
	}
	if pe.StatusCode != http.StatusTooManyRequests {
		t.Errorf("StatusCode = %d, want 429", pe.StatusCode)
	}
	if pe.RetryAfter != "11" {
		t.Errorf("RetryAfter = %q, want \"11\"", pe.RetryAfter)
	}
}
