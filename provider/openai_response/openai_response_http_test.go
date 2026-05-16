package openai_response

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/cago-frame/agents/provider"
)

// newTestProvider 把官方 SDK 指到 httptest server，并把重试关掉避免在错误响应上踩慢退避。
func newTestProvider(srvURL string) *Provider {
	zero := 0
	return NewProvider(Config{BaseURL: srvURL, APIKey: "test-key", MaxRetries: &zero}).(*Provider)
}

func TestChatCompletion_Success(t *testing.T) {
	var capturedPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{
			"id":"resp_1",
			"object":"response",
			"status":"completed",
			"created_at":1,
			"error":null,
			"incomplete_details":null,
			"instructions":null,
			"metadata":{},
			"model":"gpt-5.5",
			"output":[
				{"type":"reasoning","id":"rs_1","summary":[{"type":"summary_text","text":"think"}]},
				{"type":"message","id":"msg_1","role":"assistant","status":"completed","content":[{"type":"output_text","text":"hello","annotations":[]}]}
			],
			"parallel_tool_calls":true,
			"temperature":1,
			"tool_choice":"auto",
			"tools":[],
			"top_p":1,
			"usage":{
				"input_tokens":10,
				"input_tokens_details":{"cached_tokens":3},
				"output_tokens":7,
				"output_tokens_details":{"reasoning_tokens":2},
				"total_tokens":17
			}
		}`)
	}))
	defer srv.Close()

	p := newTestProvider(srv.URL)
	resp, err := p.ChatCompletion(context.Background(), &provider.CompletionRequest{
		Model:    "gpt-5.5",
		Messages: []provider.Message{{Role: provider.RoleUser, Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("ChatCompletion: %v", err)
	}
	if !strings.HasSuffix(capturedPath, "/responses") {
		t.Errorf("path = %s, want /responses", capturedPath)
	}
	if resp.Content != "hello" {
		t.Errorf("Content = %q", resp.Content)
	}
	if len(resp.Thinking) != 1 || resp.Thinking[0].Text != "think" {
		t.Errorf("Thinking = %#v", resp.Thinking)
	}
	if resp.FinishReason != provider.FinishStop {
		t.Errorf("FinishReason = %q", resp.FinishReason)
	}
	if resp.Usage.PromptTokens != 10 || resp.Usage.CompletionTokens != 7 || resp.Usage.TotalTokens != 17 {
		t.Errorf("Usage = %+v", resp.Usage)
	}
	if resp.Usage.ReasoningTokens != 2 || resp.Usage.CachedTokens != 3 {
		t.Errorf("Usage cached/reasoning = %+v", resp.Usage)
	}
}

func TestChatCompletion_FunctionCallSetsToolCallsFinish(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{
			"id":"resp_2","object":"response","status":"completed","created_at":1,
			"error":null,"incomplete_details":null,"instructions":null,"metadata":{},"model":"gpt-5.5",
			"output":[
				{"type":"function_call","id":"fc_1","call_id":"call_abc","name":"add","arguments":"{\"a\":1}"}
			],
			"parallel_tool_calls":true,"temperature":1,"tool_choice":"auto","tools":[],"top_p":1,
			"usage":{"input_tokens":3,"input_tokens_details":{"cached_tokens":0},"output_tokens":4,"output_tokens_details":{"reasoning_tokens":0},"total_tokens":7}
		}`)
	}))
	defer srv.Close()

	p := newTestProvider(srv.URL)
	resp, err := p.ChatCompletion(context.Background(), &provider.CompletionRequest{
		Model:    "gpt-5.5",
		Messages: []provider.Message{{Role: provider.RoleUser, Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("ChatCompletion: %v", err)
	}
	if len(resp.ToolCalls) != 1 {
		t.Fatalf("ToolCalls = %#v", resp.ToolCalls)
	}
	tc := resp.ToolCalls[0]
	if tc.ID != "call_abc" || tc.Function.Name != "add" || tc.Function.Arguments != `{"a":1}` {
		t.Errorf("tool call = %#v", tc)
	}
	if resp.FinishReason != provider.FinishToolCalls {
		t.Errorf("FinishReason = %q, want tool_calls", resp.FinishReason)
	}
}

func TestChatCompletion_AuthErrorWrappedAsProviderError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(w, `{"error":{"message":"bad key","type":"invalid_request_error","param":"","code":"invalid_api_key"}}`)
	}))
	defer srv.Close()

	p := newTestProvider(srv.URL)
	_, err := p.ChatCompletion(context.Background(), &provider.CompletionRequest{
		Model:    "gpt-5.5",
		Messages: []provider.Message{{Role: provider.RoleUser, Content: "hi"}},
	})
	if err == nil {
		t.Fatal("expected error")
	}
	var pe *provider.ProviderError
	if !errors.As(err, &pe) {
		t.Fatalf("err = %v (%T), want *provider.ProviderError", err, err)
	}
	if pe.StatusCode != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", pe.StatusCode)
	}
}

func TestChatCompletion_RateLimitWrappedAndRetryAfter(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Retry-After", "3")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = io.WriteString(w, `{"error":{"message":"too many","type":"rate_limit","param":"","code":"rate_limited"}}`)
	}))
	defer srv.Close()

	p := newTestProvider(srv.URL)
	_, err := p.ChatCompletion(context.Background(), &provider.CompletionRequest{
		Model:    "gpt-5.5",
		Messages: []provider.Message{{Role: provider.RoleUser, Content: "hi"}},
	})
	var pe *provider.ProviderError
	if !errors.As(err, &pe) {
		t.Fatalf("err = %v, want ProviderError", err)
	}
	if pe.StatusCode != http.StatusTooManyRequests {
		t.Errorf("status = %d, want 429", pe.StatusCode)
	}
	if pe.RetryAfter != "3" {
		t.Errorf("RetryAfter = %q, want \"3\"", pe.RetryAfter)
	}
}

// SSE 写入器：每个 event 写成 "event: TYPE\ndata: JSON\n\n" 的标准 SSE 帧。
// openai-go 的 ssestream 解码器只把 "data: " 之后到下一个换行作为 data，
// 多行 JSON 会被截断；先 json.Compact 把 raw 拍平成单行。
func writeSSE(t *testing.T, w http.ResponseWriter, eventType, data string) {
	t.Helper()
	var buf bytes.Buffer
	if err := json.Compact(&buf, []byte(data)); err != nil {
		t.Fatalf("compact sse data: %v", err)
	}
	if _, err := fmt.Fprintf(w, "event: %s\ndata: %s\n\n", eventType, buf.String()); err != nil {
		t.Fatalf("write sse: %v", err)
	}
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}
}

func TestChatStream_TextDeltasAndComplete(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.WriteHeader(http.StatusOK)
		writeSSE(t, w, "response.output_text.delta",
			`{"type":"response.output_text.delta","content_index":0,"item_id":"msg_1","logprobs":[],"output_index":0,"sequence_number":1,"delta":"Hel"}`)
		writeSSE(t, w, "response.output_text.delta",
			`{"type":"response.output_text.delta","content_index":0,"item_id":"msg_1","logprobs":[],"output_index":0,"sequence_number":2,"delta":"lo"}`)
		writeSSE(t, w, "response.completed",
			`{"type":"response.completed","sequence_number":3,"response":{
				"id":"resp_x","object":"response","status":"completed","created_at":1,
				"error":null,"incomplete_details":null,"instructions":null,"metadata":{},"model":"gpt-5.5",
				"output":[{"type":"message","id":"msg_1","role":"assistant","status":"completed","content":[{"type":"output_text","text":"Hello","annotations":[]}]}],
				"parallel_tool_calls":true,"temperature":1,"tool_choice":"auto","tools":[],"top_p":1,
				"usage":{"input_tokens":2,"input_tokens_details":{"cached_tokens":0},"output_tokens":3,"output_tokens_details":{"reasoning_tokens":0},"total_tokens":5}
			}}`)
	}))
	defer srv.Close()

	p := newTestProvider(srv.URL)
	ch, err := p.ChatStream(context.Background(), &provider.CompletionRequest{
		Model:    "gpt-5.5",
		Messages: []provider.Message{{Role: provider.RoleUser, Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("ChatStream: %v", err)
	}

	var got strings.Builder
	var finalUsage *provider.Usage
	var finalFinish provider.FinishReason
	for chunk := range ch {
		if chunk.Err != nil {
			t.Fatalf("stream err: %v", chunk.Err)
		}
		got.WriteString(chunk.ContentDelta)
		if chunk.Usage != nil {
			finalUsage = chunk.Usage
		}
		if chunk.FinishReason != "" {
			finalFinish = chunk.FinishReason
		}
	}
	if got.String() != "Hello" {
		t.Errorf("text = %q, want %q", got.String(), "Hello")
	}
	if finalFinish != provider.FinishStop {
		t.Errorf("finish = %q", finalFinish)
	}
	if finalUsage == nil || finalUsage.PromptTokens != 2 || finalUsage.CompletionTokens != 3 {
		t.Errorf("usage = %+v", finalUsage)
	}
}

func TestChatStream_FailedEventEmitsErr(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		writeSSE(t, w, "response.failed",
			`{"type":"response.failed","sequence_number":1,"response":{
				"id":"resp_x","object":"response","status":"failed","created_at":1,
				"error":{"code":"server_error","message":"boom"},
				"incomplete_details":null,"instructions":null,"metadata":{},"model":"gpt-5.5",
				"output":[],"parallel_tool_calls":true,"temperature":1,"tool_choice":"auto","tools":[],"top_p":1
			}}`)
	}))
	defer srv.Close()

	p := newTestProvider(srv.URL)
	ch, err := p.ChatStream(context.Background(), &provider.CompletionRequest{
		Model:    "gpt-5.5",
		Messages: []provider.Message{{Role: provider.RoleUser, Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("ChatStream: %v", err)
	}
	var sawErr error
	for chunk := range ch {
		if chunk.Err != nil {
			sawErr = chunk.Err
		}
	}
	if sawErr == nil {
		t.Fatal("expected error from stream")
	}
	if !strings.Contains(sawErr.Error(), "boom") {
		t.Errorf("err = %v, want to contain 'boom'", sawErr)
	}
}
