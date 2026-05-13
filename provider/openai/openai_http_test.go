package openai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	openai "github.com/sashabaranov/go-openai"

	"github.com/cago-frame/agents/provider"
)

// newTestProvider points the openai client at the given test server.
func newTestProvider(srvURL string) *Provider {
	cfg := openai.DefaultConfig("test-key")
	cfg.BaseURL = srvURL
	return NewProvider(cfg).(*Provider)
}

func TestChatCompletion_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		if !strings.HasSuffix(r.URL.Path, "/chat/completions") {
			t.Errorf("path = %s, want /chat/completions", r.URL.Path)
		}
		body, _ := io.ReadAll(r.Body)
		var got map[string]any
		_ = json.Unmarshal(body, &got)
		if got["model"] != "gpt-4o" {
			t.Errorf("model = %v, want gpt-4o", got["model"])
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{
			"id": "chatcmpl-1",
			"object": "chat.completion",
			"choices": [{
				"index": 0,
				"message": {
					"role": "assistant",
					"reasoning_content": "think",
					"content": "hello",
					"tool_calls": [{
						"id": "call_1",
						"type": "function",
						"function": {"name": "add", "arguments": "{\"a\":1}"}
					}]
				},
				"finish_reason": "tool_calls"
			}],
			"usage": {
				"prompt_tokens": 10,
				"completion_tokens": 5,
				"total_tokens": 15,
				"prompt_tokens_details": {"cached_tokens": 3},
				"completion_tokens_details": {"reasoning_tokens": 2}
			}
		}`)
	}))
	defer srv.Close()

	p := newTestProvider(srv.URL)
	temp := float32(0.5)
	maxTok := 100
	resp, err := p.ChatCompletion(context.Background(), &provider.CompletionRequest{
		Model:       "gpt-4o",
		Messages:    []provider.Message{{Role: provider.RoleUser, Content: "hi"}},
		Temperature: &temp,
		MaxTokens:   &maxTok,
		Tools: []provider.Tool{{
			Type: provider.ToolTypeFunction,
			Function: &provider.FunctionDefinition{
				Name:       "add",
				Parameters: json.RawMessage(`{"type":"object"}`),
			},
		}},
		ToolChoice: &provider.ToolChoice{Type: "auto"},
		Thinking:   &provider.ThinkingConfig{Effort: provider.ThinkingHigh},
	})
	if err != nil {
		t.Fatalf("ChatCompletion: %v", err)
	}
	if resp.Content != "hello" {
		t.Errorf("Content = %q, want %q", resp.Content, "hello")
	}
	if len(resp.Thinking) != 1 || resp.Thinking[0].Text != "think" {
		t.Errorf("Thinking = %#v, want one reasoning block", resp.Thinking)
	}
	if resp.Role != provider.RoleAssistant {
		t.Errorf("Role = %q, want assistant", resp.Role)
	}
	if len(resp.ToolCalls) != 1 || resp.ToolCalls[0].Function.Name != "add" {
		t.Errorf("ToolCalls = %#v, want one add call", resp.ToolCalls)
	}
	if resp.FinishReason != provider.FinishToolCalls {
		t.Errorf("FinishReason = %q, want tool_calls", resp.FinishReason)
	}
	if resp.Usage.PromptTokens != 10 || resp.Usage.CompletionTokens != 5 || resp.Usage.TotalTokens != 15 {
		t.Errorf("Usage = %+v", resp.Usage)
	}
	if resp.Usage.CachedTokens != 3 || resp.Usage.ReasoningTokens != 2 {
		t.Errorf("Usage cached/reasoning = %+v", resp.Usage)
	}
}

func TestChatCompletion_EmptyChoicesReturnsSentinel(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"x","choices":[]}`)
	}))
	defer srv.Close()

	p := newTestProvider(srv.URL)
	_, err := p.ChatCompletion(context.Background(), &provider.CompletionRequest{
		Model:    "gpt-4o",
		Messages: []provider.Message{{Role: provider.RoleUser, Content: "hi"}},
	})
	if !errors.Is(err, ErrEmptyChoices) {
		t.Fatalf("err = %v, want ErrEmptyChoices", err)
	}
}

func TestChatCompletion_HTTPErrorPropagates(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, `{"error":{"message":"bad request"}}`)
	}))
	defer srv.Close()

	p := newTestProvider(srv.URL)
	_, err := p.ChatCompletion(context.Background(), &provider.CompletionRequest{
		Model:    "gpt-4o",
		Messages: []provider.Message{{Role: provider.RoleUser, Content: "hi"}},
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestChatCompletion_BuildRequestErrorPropagates(t *testing.T) {
	// invalid response_format (json_schema without Schema)
	p := newTestProvider("http://invalid.local")
	_, err := p.ChatCompletion(context.Background(), &provider.CompletionRequest{
		Model:    "gpt-4o",
		Messages: []provider.Message{{Role: provider.RoleUser, Content: "hi"}},
		ResponseFormat: &provider.ResponseFormat{
			Type: provider.ResponseFormatJSONSchema,
			// missing Schema → buildResponseFormat returns error
		},
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestChatStream_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		flusher := w.(http.Flusher)
		frames := []string{
			`{"id":"1","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"role":"assistant","reasoning_content":"think "}}]}`,
			`{"id":"1","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"reasoning_content":"first"}}]}`,
			`{"id":"1","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"content":"Hel"}}]}`,
			`{"id":"1","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"content":"lo"}}]}`,
			`{"id":"1","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"add","arguments":"{\"a\":1}"}}]}}]}`,
			`{"id":"1","object":"chat.completion.chunk","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`,
			`{"id":"1","object":"chat.completion.chunk","choices":[],"usage":{"prompt_tokens":1,"completion_tokens":2,"total_tokens":3}}`,
		}
		for _, f := range frames {
			_, _ = fmt.Fprintf(w, "data: %s\n\n", f)
			flusher.Flush()
		}
		_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
		flusher.Flush()
	}))
	defer srv.Close()

	p := newTestProvider(srv.URL)
	ch, err := p.ChatStream(context.Background(), &provider.CompletionRequest{
		Model:    "gpt-4o",
		Messages: []provider.Message{{Role: provider.RoleUser, Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("ChatStream: %v", err)
	}

	var content strings.Builder
	var thinking strings.Builder
	var finish provider.FinishReason
	var usage *provider.Usage
	var toolCallSeen bool
	for chunk := range ch {
		if chunk.Err != nil {
			t.Fatalf("stream err: %v", chunk.Err)
		}
		if chunk.ContentDelta != "" {
			content.WriteString(chunk.ContentDelta)
		}
		if chunk.ThinkingDelta != nil {
			thinking.WriteString(chunk.ThinkingDelta.Text)
		}
		if chunk.ToolCallDelta != nil && chunk.ToolCallDelta.Name == "add" {
			toolCallSeen = true
		}
		if chunk.FinishReason != "" {
			finish = chunk.FinishReason
		}
		if chunk.Usage != nil {
			usage = chunk.Usage
		}
	}
	if got := content.String(); got != "Hello" {
		t.Errorf("content = %q, want Hello", got)
	}
	if got := thinking.String(); got != "think first" {
		t.Errorf("thinking = %q, want think first", got)
	}
	if !toolCallSeen {
		t.Error("expected a tool_call delta")
	}
	if finish != provider.FinishStop {
		t.Errorf("finish = %q, want stop", finish)
	}
	if usage == nil || usage.TotalTokens != 3 {
		t.Errorf("usage = %+v", usage)
	}
}

func TestChatStream_BuildRequestError(t *testing.T) {
	p := newTestProvider("http://invalid.local")
	_, err := p.ChatStream(context.Background(), &provider.CompletionRequest{
		Model:      "gpt-4o",
		Messages:   []provider.Message{{Role: provider.RoleUser, Content: "hi"}},
		ToolChoice: &provider.ToolChoice{Type: "totally-invalid"},
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestChatStream_HTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = io.WriteString(w, `{"error":{"message":"boom"}}`)
	}))
	defer srv.Close()

	p := newTestProvider(srv.URL)
	_, err := p.ChatStream(context.Background(), &provider.CompletionRequest{
		Model:    "gpt-4o",
		Messages: []provider.Message{{Role: provider.RoleUser, Content: "hi"}},
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	// HTTP 错误必须 wrap 成 *provider.ProviderError 才能让 agent.RetryPolicy
	// 识别 status code 走重试白名单。go-openai SDK 直接返回的 *openai.APIError
	// 在 errors.As(err, &ProviderError) 上拿不到 StatusCode，会被 defaultShouldRetry
	// 漏掉。回归测试覆盖 500/503/429 都能被正确包装。
	var pe *provider.ProviderError
	if !errors.As(err, &pe) {
		t.Fatalf("expected *provider.ProviderError, got %T: %v", err, err)
	}
	if pe.StatusCode != http.StatusInternalServerError {
		t.Fatalf("expected StatusCode=500, got %d", pe.StatusCode)
	}
}

// TestChatStream_503ReturnsProviderError 回归 issue: 503 错误未被 wrap，导致
// agent.RetryPolicy.defaultShouldRetry 无法识别 status code，5xx 完全不重试。
// 修复后 OpenAI provider 在 CreateChatCompletionStream 失败的同步路径也走 wrap。
func TestChatStream_503ReturnsProviderError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = io.WriteString(w, `{"error":{"message":"No available channel"}}`)
	}))
	defer srv.Close()

	p := newTestProvider(srv.URL)
	_, err := p.ChatStream(context.Background(), &provider.CompletionRequest{
		Model:    "gpt-4o",
		Messages: []provider.Message{{Role: provider.RoleUser, Content: "hi"}},
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	var pe *provider.ProviderError
	if !errors.As(err, &pe) {
		t.Fatalf("expected *provider.ProviderError, got %T: %v", err, err)
	}
	if pe.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("expected StatusCode=503, got %d", pe.StatusCode)
	}
}

// TestChatStream_TruncatedBeforeFinishReportsUnexpectedEOF 回归 issue: 服务端
// 在 finish_reason 之前硬断连接，go-openai SDK 返回 io.EOF 与正常完成无法区分。
// 修复后 OpenAI provider 用 finishSeen 标志跟踪 —— 未见 finish_reason 就 EOF
// 视为流被截断，emit io.ErrUnexpectedEOF，agent.RetryPolicy 的字符串 fallback
// 识别 "unexpected eof" 走重试白名单。
func TestChatStream_TruncatedBeforeFinishReportsUnexpectedEOF(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher := w.(http.Flusher)
		// 发一段 content delta 后直接关闭连接（不发 finish_reason，也不发 [DONE]）。
		_, _ = fmt.Fprint(w, "data: {\"id\":\"1\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"Hel\"}}]}\n\n")
		flusher.Flush()
		// 直接 return —— 不发 finish_reason，模拟服务端中途切断。
	}))
	defer srv.Close()

	p := newTestProvider(srv.URL)
	ch, err := p.ChatStream(context.Background(), &provider.CompletionRequest{
		Model:    "gpt-4o",
		Messages: []provider.Message{{Role: provider.RoleUser, Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("ChatStream: %v", err)
	}
	var gotErr error
	for chunk := range ch {
		if chunk.Err != nil {
			gotErr = chunk.Err
		}
	}
	if gotErr == nil {
		t.Fatal("expected error chunk on truncated stream, got nil")
	}
	if !errors.Is(gotErr, io.ErrUnexpectedEOF) {
		t.Fatalf("expected io.ErrUnexpectedEOF, got %v", gotErr)
	}
}

// TestChatCompletion_429ReturnsProviderError 覆盖非 stream 路径同样要 wrap。
func TestChatCompletion_429ReturnsProviderError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = io.WriteString(w, `{"error":{"message":"rate limit"}}`)
	}))
	defer srv.Close()

	p := newTestProvider(srv.URL)
	_, err := p.ChatCompletion(context.Background(), &provider.CompletionRequest{
		Model:    "gpt-4o",
		Messages: []provider.Message{{Role: provider.RoleUser, Content: "hi"}},
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	var pe *provider.ProviderError
	if !errors.As(err, &pe) {
		t.Fatalf("expected *provider.ProviderError, got %T: %v", err, err)
	}
	if pe.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("expected StatusCode=429, got %d", pe.StatusCode)
	}
}

func TestChatStream_CancellingCtx(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher := w.(http.Flusher)
		_, _ = fmt.Fprint(w, "data: {\"id\":\"1\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"x\"}}]}\n\n")
		flusher.Flush()
		<-r.Context().Done()
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	p := newTestProvider(srv.URL)
	ch, err := p.ChatStream(ctx, &provider.CompletionRequest{
		Model:    "gpt-4o",
		Messages: []provider.Message{{Role: provider.RoleUser, Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("ChatStream: %v", err)
	}
	got := false
	for chunk := range ch {
		if chunk.ContentDelta != "" && !got {
			got = true
			cancel()
		}
	}
	if !got {
		t.Error("expected at least one content chunk before cancel")
	}
}

// --- helpers / smaller branches ---

func TestBuildResponseFormat_Text(t *testing.T) {
	got, err := buildResponseFormat(&provider.ResponseFormat{Type: provider.ResponseFormatText})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if got.Type != openai.ChatCompletionResponseFormatTypeText {
		t.Errorf("Type = %v, want text", got.Type)
	}
}

func TestBuildResponseFormat_Unknown(t *testing.T) {
	if _, err := buildResponseFormat(&provider.ResponseFormat{Type: "made_up"}); err == nil {
		t.Error("expected error on unknown type")
	}
}

func TestBuildToolChoice_Variants(t *testing.T) {
	cases := []struct {
		in      *provider.ToolChoice
		wantErr bool
	}{
		{&provider.ToolChoice{Type: "auto"}, false},
		{&provider.ToolChoice{Type: "required"}, false},
		{&provider.ToolChoice{Type: "none"}, false},
		{&provider.ToolChoice{Type: "tool", Name: "add"}, false},
	}
	for _, tc := range cases {
		out, err := buildToolChoice(tc.in)
		if tc.wantErr && err == nil {
			t.Errorf("type=%q: expected error", tc.in.Type)
		}
		if !tc.wantErr && err != nil {
			t.Errorf("type=%q: unexpected error: %v", tc.in.Type, err)
		}
		if tc.in.Type == "tool" && err == nil {
			m, ok := out.(map[string]any)
			if !ok || m["type"] != "function" {
				t.Errorf("type=tool: out = %#v", out)
			}
		}
	}
}

func TestFromOAIToolCalls_NilEmpty(t *testing.T) {
	if got := fromOAIToolCalls(nil); got != nil {
		t.Errorf("got %v, want nil", got)
	}
	if got := fromOAIToolCalls([]openai.ToolCall{}); got != nil {
		t.Errorf("got %v, want nil", got)
	}
}

func TestFromOAIFinishReason(t *testing.T) {
	cases := map[string]provider.FinishReason{
		"stop":           provider.FinishStop,
		"tool_calls":     provider.FinishToolCalls,
		"length":         provider.FinishLength,
		"content_filter": provider.FinishContentFilter,
		"":               "",
		"null":           "",
		"weird":          provider.FinishReason("weird"),
	}
	for in, want := range cases {
		if got := fromOAIFinishReason(in); got != want {
			t.Errorf("fromOAIFinishReason(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestProvider_Name(t *testing.T) {
	p := newTestProvider("http://example.invalid")
	if p.Name() != "openai" {
		t.Errorf("Name = %q", p.Name())
	}
}

func TestBuildRequest_ToolCallsAndToolMessages(t *testing.T) {
	p := &Provider{}
	req := &provider.CompletionRequest{
		Model: "gpt-4o",
		Messages: []provider.Message{
			{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{
				ID: "call_1", Type: provider.ToolTypeFunction,
				Function: provider.ToolCallFunction{Name: "add", Arguments: `{"a":1}`},
			}}},
			{Role: provider.RoleTool, ToolCallID: "call_1", Name: "add", Content: "2"},
		},
	}
	got, err := p.buildRequest(req, false)
	if err != nil {
		t.Fatalf("buildRequest: %v", err)
	}
	if len(got.Messages) != 2 {
		t.Fatalf("messages len = %d, want 2", len(got.Messages))
	}
	if len(got.Messages[0].ToolCalls) != 1 || got.Messages[0].ToolCalls[0].Function.Name != "add" {
		t.Errorf("first message ToolCalls = %#v", got.Messages[0].ToolCalls)
	}
	if got.Messages[1].ToolCallID != "call_1" {
		t.Errorf("ToolCallID = %q", got.Messages[1].ToolCallID)
	}
}

func TestBuildRequest_AssistantThinkingAsReasoningContent(t *testing.T) {
	p := &Provider{}
	got, err := p.buildRequest(&provider.CompletionRequest{
		Model: "deepseek-v4-pro",
		Messages: []provider.Message{{
			Role:    provider.RoleAssistant,
			Content: "final",
			Thinking: []provider.ThinkingBlock{
				{Text: "first "},
				{Text: "second"},
			},
		}},
	}, false)
	if err != nil {
		t.Fatalf("buildRequest: %v", err)
	}
	if len(got.Messages) != 1 {
		t.Fatalf("messages len = %d, want 1", len(got.Messages))
	}
	if got.Messages[0].ReasoningContent != "first second" {
		t.Errorf("ReasoningContent = %q, want first second", got.Messages[0].ReasoningContent)
	}
}

func TestBuildRequest_ToolFunctionParametersInvalidJSON(t *testing.T) {
	p := &Provider{}
	_, err := p.buildRequest(&provider.CompletionRequest{
		Model:    "gpt-4o",
		Messages: []provider.Message{{Role: provider.RoleUser, Content: "hi"}},
		Tools: []provider.Tool{{
			Type: provider.ToolTypeFunction,
			Function: &provider.FunctionDefinition{
				Name:       "broken",
				Parameters: json.RawMessage(`not-json`),
			},
		}},
	}, false)
	if err == nil {
		t.Error("expected error on invalid JSON parameters")
	}
}

// 同 anthropic 的 cctranai 修复：OpenAI 兼容代理（含 OneAPI/new-api 衍生）若工具
// parameters 没有 properties 字段，也可能回 400 "function parameters is empty"。
// 这里保证 type=object 但 properties 缺失时，序列化出去一定带 "properties":{}。
func TestBuildRequest_EmptyPropertiesObjectSchemaGetsDefaultedProperties(t *testing.T) {
	p := &Provider{}
	for _, raw := range []json.RawMessage{
		json.RawMessage(`{"type":"object"}`),
		nil,
		json.RawMessage(``),
	} {
		got, err := p.buildRequest(&provider.CompletionRequest{
			Model:    "gpt-4o",
			Messages: []provider.Message{{Role: provider.RoleUser, Content: "hi"}},
			Tools: []provider.Tool{{
				Type: provider.ToolTypeFunction,
				Function: &provider.FunctionDefinition{
					Name:       "no_args",
					Parameters: raw,
				},
			}},
		}, false)
		if err != nil {
			t.Fatalf("buildRequest(%q): %v", string(raw), err)
		}
		if len(got.Tools) != 1 {
			t.Fatalf("buildRequest(%q): want 1 tool, got %d", string(raw), len(got.Tools))
		}
		body, err := json.Marshal(got.Tools[0].Function.Parameters)
		if err != nil {
			t.Fatalf("marshal(%q): %v", string(raw), err)
		}
		if !strings.Contains(string(body), `"properties"`) {
			t.Errorf("buildRequest(%q): marshaled parameters missing properties: %s", string(raw), body)
		}
	}
}

func TestBuildRequest_NilToolFunctionSkipped(t *testing.T) {
	p := &Provider{}
	got, err := p.buildRequest(&provider.CompletionRequest{
		Model:    "gpt-4o",
		Messages: []provider.Message{{Role: provider.RoleUser, Content: "hi"}},
		Tools:    []provider.Tool{{Type: provider.ToolTypeFunction, Function: nil}},
	}, false)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(got.Tools) != 0 {
		t.Errorf("Tools = %d, want 0 (nil func skipped)", len(got.Tools))
	}
}

func TestOpenAI_ChatCompletion_ImageMultiContent(t *testing.T) {
	var capturedBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedBody, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{
			"id": "x", "object":"chat.completion", "created":0, "model":"gpt-x",
			"choices":[{"index":0,"message":{"role":"assistant","content":"a cat"},"finish_reason":"stop"}],
			"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}
		}`)
	}))
	defer srv.Close()

	p := newTestProvider(srv.URL)
	req := &provider.CompletionRequest{
		Model: "gpt-x",
		Messages: []provider.Message{{
			Role: provider.RoleUser,
			MultiContent: []provider.MessagePart{
				{Type: provider.MessagePartText, Text: "what is this?"},
				{Type: provider.MessagePartImage, Image: &provider.MessageImage{
					URL: "https://example.com/cat.png",
				}},
			},
		}},
	}
	_, err := p.ChatCompletion(context.Background(), req)
	if err != nil {
		t.Fatalf("chat: %v", err)
	}

	bodyStr := string(capturedBody)
	if !strings.Contains(bodyStr, `"type":"image_url"`) {
		t.Fatalf("body missing image_url part:\n%s", bodyStr)
	}
	if !strings.Contains(bodyStr, `"url":"https://example.com/cat.png"`) {
		t.Fatalf("body missing image url:\n%s", bodyStr)
	}
	if !strings.Contains(bodyStr, `"text":"what is this?"`) {
		t.Fatalf("body missing text part:\n%s", bodyStr)
	}
}

func TestOpenAI_ChatCompletion_ImageMultiContent_InlineBase64(t *testing.T) {
	var capturedBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedBody, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{
			"id": "x", "object":"chat.completion", "created":0, "model":"gpt-x",
			"choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],
			"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}
		}`)
	}))
	defer srv.Close()

	p := newTestProvider(srv.URL)
	req := &provider.CompletionRequest{
		Model: "gpt-x",
		Messages: []provider.Message{{
			Role: provider.RoleUser,
			MultiContent: []provider.MessagePart{
				{Type: provider.MessagePartImage, Image: &provider.MessageImage{
					MediaType: "image/png",
					Inline:    []byte{0x89, 0x50, 0x4e, 0x47},
				}},
			},
		}},
	}
	if _, err := p.ChatCompletion(context.Background(), req); err != nil {
		t.Fatalf("chat: %v", err)
	}
	bodyStr := string(capturedBody)
	if !strings.Contains(bodyStr, `data:image/png;base64,`) {
		t.Fatalf("body missing base64 data URI:\n%s", bodyStr)
	}
}
