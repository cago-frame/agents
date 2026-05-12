package anthropics

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/anthropics/anthropic-sdk-go"

	"github.com/cago-frame/agents/provider"
)

func TestProvider_Name(t *testing.T) {
	p := NewProvider(Config{APIKey: "test"})
	if p.Name() != "anthropic" {
		t.Errorf("Name = %q", p.Name())
	}
}

func TestChatCompletion_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{
			"id": "msg_1",
			"type": "message",
			"role": "assistant",
			"model": "claude-sonnet-4-6",
			"stop_reason": "tool_use",
			"content": [
				{"type": "thinking", "thinking": "let me think", "signature": "sig"},
				{"type": "text", "text": "hello"},
				{"type": "tool_use", "id": "toolu_1", "name": "add", "input": {"a": 1}}
			],
			"usage": {
				"input_tokens": 10,
				"output_tokens": 5,
				"cache_creation_input_tokens": 1,
				"cache_read_input_tokens": 2
			}
		}`)
	}))
	defer srv.Close()
	noRetry := 0
	p := NewProvider(Config{APIKey: "test", BaseURL: srv.URL, MaxRetries: &noRetry})

	resp, err := p.ChatCompletion(context.Background(), &provider.CompletionRequest{
		Model:    "claude-sonnet-4-6",
		Messages: []provider.Message{{Role: provider.RoleUser, Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("ChatCompletion: %v", err)
	}
	if resp.Content != "hello" {
		t.Errorf("Content = %q", resp.Content)
	}
	if len(resp.Thinking) != 1 || resp.Thinking[0].Signature != "sig" {
		t.Errorf("Thinking = %+v", resp.Thinking)
	}
	if len(resp.ToolCalls) != 1 || resp.ToolCalls[0].Function.Name != "add" {
		t.Errorf("ToolCalls = %+v", resp.ToolCalls)
	}
	if resp.FinishReason != provider.FinishToolCalls {
		t.Errorf("FinishReason = %q", resp.FinishReason)
	}
	// 10 input + 1 cache_creation + 2 cache_read + 5 output = 18
	if resp.Usage.TotalTokens != 18 {
		t.Errorf("Usage = %+v", resp.Usage)
	}
	if resp.Usage.CachedTokens != 2 || resp.Usage.CacheCreationTokens != 1 {
		t.Errorf("Usage cache = %+v", resp.Usage)
	}
}

func TestChatCompletion_ToolUseEmptyInputBecomesEmptyJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{
			"id": "msg_1",
			"type": "message",
			"role": "assistant",
			"model": "claude-sonnet-4-6",
			"stop_reason": "tool_use",
			"content": [
				{"type": "tool_use", "id": "toolu_1", "name": "noop"}
			],
			"usage": {"input_tokens": 1, "output_tokens": 0}
		}`)
	}))
	defer srv.Close()
	noRetry := 0
	p := NewProvider(Config{APIKey: "test", BaseURL: srv.URL, MaxRetries: &noRetry})

	resp, err := p.ChatCompletion(context.Background(), &provider.CompletionRequest{
		Model:    "claude-sonnet-4-6",
		Messages: []provider.Message{{Role: provider.RoleUser, Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("ChatCompletion: %v", err)
	}
	if len(resp.ToolCalls) != 1 || resp.ToolCalls[0].Function.Arguments != "{}" {
		t.Errorf("ToolCalls = %+v", resp.ToolCalls)
	}
}

func TestChatStream_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		flusher := w.(http.Flusher)

		write := func(typ, data string) {
			_, _ = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", typ, data)
			flusher.Flush()
		}
		write("message_start", `{"type":"message_start","message":{"id":"msg_1","type":"message","role":"assistant","content":[],"model":"claude","stop_reason":null,"stop_sequence":null,"usage":{"input_tokens":10,"output_tokens":0,"cache_read_input_tokens":1,"cache_creation_input_tokens":0}}}`)
		write("content_block_start", `{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`)
		write("content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hi"}}`)
		write("content_block_stop", `{"type":"content_block_stop","index":0}`)
		write("content_block_start", `{"type":"content_block_start","index":1,"content_block":{"type":"thinking","thinking":""}}`)
		write("content_block_delta", `{"type":"content_block_delta","index":1,"delta":{"type":"thinking_delta","thinking":"think"}}`)
		write("content_block_delta", `{"type":"content_block_delta","index":1,"delta":{"type":"signature_delta","signature":"sig"}}`)
		write("content_block_stop", `{"type":"content_block_stop","index":1}`)
		write("content_block_start", `{"type":"content_block_start","index":2,"content_block":{"type":"tool_use","id":"toolu_1","name":"add","input":{}}}`)
		write("content_block_delta", `{"type":"content_block_delta","index":2,"delta":{"type":"input_json_delta","partial_json":"{\"a\":1}"}}`)
		write("content_block_stop", `{"type":"content_block_stop","index":2}`)
		write("message_delta", `{"type":"message_delta","delta":{"stop_reason":"end_turn","stop_sequence":null},"usage":{"input_tokens":10,"output_tokens":7,"cache_read_input_tokens":1,"cache_creation_input_tokens":0}}`)
		write("message_stop", `{"type":"message_stop"}`)
	}))
	defer srv.Close()
	noRetry := 0
	p := NewProvider(Config{APIKey: "test", BaseURL: srv.URL, MaxRetries: &noRetry})

	ch, err := p.ChatStream(context.Background(), &provider.CompletionRequest{
		Model:    "claude-sonnet-4-6",
		Messages: []provider.Message{{Role: provider.RoleUser, Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("ChatStream: %v", err)
	}

	var content strings.Builder
	var thinkText, thinkSig string
	var toolCallStarted bool
	var argsAccum strings.Builder
	var finish provider.FinishReason
	var usage *provider.Usage
	for chunk := range ch {
		if chunk.Err != nil {
			t.Fatalf("stream err: %v", chunk.Err)
		}
		if chunk.ContentDelta != "" {
			content.WriteString(chunk.ContentDelta)
		}
		if chunk.ThinkingDelta != nil {
			thinkText += chunk.ThinkingDelta.Text
			if chunk.ThinkingDelta.Signature != "" {
				thinkSig = chunk.ThinkingDelta.Signature
			}
		}
		if chunk.ToolCallDelta != nil {
			if chunk.ToolCallDelta.ID != "" {
				toolCallStarted = true
			}
			argsAccum.WriteString(chunk.ToolCallDelta.ArgsDelta)
		}
		if chunk.FinishReason != "" {
			finish = chunk.FinishReason
		}
		if chunk.Usage != nil {
			usage = chunk.Usage
		}
	}
	if content.String() != "Hi" {
		t.Errorf("content = %q", content.String())
	}
	if thinkText != "think" || thinkSig != "sig" {
		t.Errorf("thinking = %q sig=%q", thinkText, thinkSig)
	}
	if !toolCallStarted || argsAccum.String() != `{"a":1}` {
		t.Errorf("toolCall start=%v args=%q", toolCallStarted, argsAccum.String())
	}
	if finish != provider.FinishStop {
		t.Errorf("finish = %q", finish)
	}
	// 10 input + 1 cache_read + 7 output = 18
	if usage == nil || usage.TotalTokens != 18 {
		t.Errorf("usage = %+v", usage)
	}
}

func TestParseInputSchema(t *testing.T) {
	// empty
	got, err := parseInputSchema(nil)
	if err != nil || got.Required != nil {
		t.Errorf("empty: got %+v err %v", got, err)
	}

	// invalid JSON
	if _, err := parseInputSchema([]byte("not-json")); err == nil {
		t.Error("invalid JSON: expected error")
	}

	// full schema with required + extra fields
	got, err = parseInputSchema([]byte(`{
		"type": "object",
		"properties": {"a": {"type": "string"}},
		"required": ["a"],
		"$defs": {"x": {"type": "string"}},
		"additionalProperties": false
	}`))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(got.Required) != 1 || got.Required[0] != "a" {
		t.Errorf("Required = %v", got.Required)
	}
	if got.Properties == nil {
		t.Error("Properties nil")
	}
	if got.ExtraFields == nil || got.ExtraFields["$defs"] == nil {
		t.Errorf("ExtraFields = %+v", got.ExtraFields)
	}
	if _, hasType := got.ExtraFields["type"]; hasType {
		t.Error("ExtraFields should not include 'type'")
	}
}

// 部分 Anthropic 兼容代理（如 cctranai / 某些 OneAPI 衍生）会把缺 properties 的
// input_schema 误判为 "function parameters is empty" 并返回 400。Anthropic 原生
// API 自身不强制，但发空 properties 在所有家代理上都安全，所以这里统一兜底。
func TestParseInputSchema_DefaultsEmptyPropertiesWhenMissing(t *testing.T) {
	for _, in := range [][]byte{
		nil,
		[]byte(`{"type":"object"}`),
		[]byte(`{}`),
	} {
		got, err := parseInputSchema(in)
		if err != nil {
			t.Fatalf("parse(%q): %v", string(in), err)
		}
		m, ok := got.Properties.(map[string]any)
		if !ok {
			t.Fatalf("parse(%q): Properties should be map[string]any, got %T (%v)", string(in), got.Properties, got.Properties)
		}
		if len(m) != 0 {
			t.Errorf("parse(%q): Properties should be empty, got %v", string(in), m)
		}

		// 验证最终出线的 JSON 一定包含 properties 字段，否则 cctranai 等代理会 400。
		raw, err := got.MarshalJSON()
		if err != nil {
			t.Fatalf("marshal(%q): %v", string(in), err)
		}
		if !strings.Contains(string(raw), `"properties"`) {
			t.Errorf("parse(%q): marshaled JSON missing properties: %s", string(in), raw)
		}
	}
}

func TestBuildToolChoice_Cases(t *testing.T) {
	cases := []struct {
		in   *provider.ToolChoice
		want string
	}{
		{&provider.ToolChoice{Type: "auto"}, "auto"},
		{&provider.ToolChoice{Type: "required"}, "any"},
		{&provider.ToolChoice{Type: "none"}, "none"},
		{&provider.ToolChoice{Type: "tool", Name: "add"}, "tool"},
	}
	for _, tc := range cases {
		out, err := buildToolChoice(tc.in)
		if err != nil {
			t.Errorf("type=%q: err %v", tc.in.Type, err)
			continue
		}
		switch tc.want {
		case "auto":
			if out.OfAuto == nil {
				t.Errorf("type=%q: OfAuto nil", tc.in.Type)
			}
		case "any":
			if out.OfAny == nil {
				t.Errorf("type=%q: OfAny nil", tc.in.Type)
			}
		case "none":
			if out.OfNone == nil {
				t.Errorf("type=%q: OfNone nil", tc.in.Type)
			}
		case "tool":
			if out.OfTool == nil || out.OfTool.Name != "add" {
				t.Errorf("type=%q: OfTool = %+v", tc.in.Type, out.OfTool)
			}
		}
	}
}

func TestFromAnthropicStopReason(t *testing.T) {
	cases := map[anthropic.StopReason]provider.FinishReason{
		anthropic.StopReasonEndTurn:      provider.FinishStop,
		anthropic.StopReasonStopSequence: provider.FinishStop,
		anthropic.StopReasonPauseTurn:    provider.FinishStop,
		anthropic.StopReasonMaxTokens:    provider.FinishLength,
		anthropic.StopReasonToolUse:      provider.FinishToolCalls,
		anthropic.StopReasonRefusal:      provider.FinishContentFilter,
	}
	for in, want := range cases {
		if got := fromAnthropicStopReason(in); got != want {
			t.Errorf("%q → %q, want %q", in, got, want)
		}
	}
}

func TestFromAnthropicUsage(t *testing.T) {
	got := fromAnthropicUsage(anthropic.Usage{
		InputTokens:              10,
		OutputTokens:             5,
		CacheCreationInputTokens: 1,
		CacheReadInputTokens:     2,
	})
	if got.PromptTokens != 10 || got.CompletionTokens != 5 || got.CachedTokens != 2 || got.CacheCreationTokens != 1 {
		t.Errorf("got %+v", got)
	}
	if got.TotalTokens != 18 {
		t.Errorf("Total = %d", got.TotalTokens)
	}
}

func TestMergeDeltaUsage(t *testing.T) {
	dst := provider.Usage{}
	mergeDeltaUsage(&dst, anthropic.MessageDeltaUsage{
		InputTokens:              10,
		OutputTokens:             3,
		CacheReadInputTokens:     2,
		CacheCreationInputTokens: 1,
	})
	if dst.PromptTokens != 10 || dst.CompletionTokens != 3 || dst.CachedTokens != 2 || dst.CacheCreationTokens != 1 {
		t.Errorf("merged dst = %+v", dst)
	}
	// Subsequent delta with only OutputTokens should not reset input fields.
	mergeDeltaUsage(&dst, anthropic.MessageDeltaUsage{OutputTokens: 7})
	if dst.PromptTokens != 10 || dst.CompletionTokens != 7 {
		t.Errorf("after second merge: %+v", dst)
	}
}

func TestAnthropic_ChatCompletion_ImageMultiContent_Base64(t *testing.T) {
	var capturedBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedBody, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{
			"id":"msg_x","type":"message","role":"assistant","model":"claude-x",
			"content":[{"type":"text","text":"a cat"}],
			"stop_reason":"end_turn",
			"usage":{"input_tokens":1,"output_tokens":1}
		}`)
	}))
	defer srv.Close()
	noRetry := 0
	p := NewProvider(Config{APIKey: "test", BaseURL: srv.URL, MaxRetries: &noRetry})

	req := &provider.CompletionRequest{
		Model: "claude-x",
		Messages: []provider.Message{{
			Role: provider.RoleUser,
			MultiContent: []provider.MessagePart{
				{Type: provider.MessagePartText, Text: "what is this?"},
				{Type: provider.MessagePartImage, Image: &provider.MessageImage{
					MediaType: "image/png",
					Inline:    []byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a},
				}},
			},
		}},
	}
	if _, err := p.ChatCompletion(context.Background(), req); err != nil {
		t.Fatalf("chat: %v", err)
	}

	bodyStr := string(capturedBody)
	if !strings.Contains(bodyStr, `"type":"image"`) {
		t.Fatalf("body missing image content block:\n%s", bodyStr)
	}
	if !strings.Contains(bodyStr, `"media_type":"image/png"`) {
		t.Fatalf("body missing media_type:\n%s", bodyStr)
	}
	if !strings.Contains(bodyStr, `"type":"base64"`) {
		t.Fatalf("body missing base64 source type:\n%s", bodyStr)
	}
	if !strings.Contains(bodyStr, `"text":"what is this?"`) {
		t.Fatalf("body missing text part:\n%s", bodyStr)
	}
}

func TestAnthropic_ChatCompletion_ImageMultiContent_URL(t *testing.T) {
	var capturedBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedBody, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{
			"id":"msg_x","type":"message","role":"assistant","model":"claude-x",
			"content":[{"type":"text","text":"ok"}],
			"stop_reason":"end_turn",
			"usage":{"input_tokens":1,"output_tokens":1}
		}`)
	}))
	defer srv.Close()
	noRetry := 0
	p := NewProvider(Config{APIKey: "test", BaseURL: srv.URL, MaxRetries: &noRetry})

	req := &provider.CompletionRequest{
		Model: "claude-x",
		Messages: []provider.Message{{
			Role: provider.RoleUser,
			MultiContent: []provider.MessagePart{
				{Type: provider.MessagePartImage, Image: &provider.MessageImage{
					URL: "https://example.com/cat.png",
				}},
			},
		}},
	}
	if _, err := p.ChatCompletion(context.Background(), req); err != nil {
		t.Fatalf("chat: %v", err)
	}
	bodyStr := string(capturedBody)
	if !strings.Contains(bodyStr, `"type":"url"`) {
		t.Fatalf("body missing url source type:\n%s", bodyStr)
	}
	if !strings.Contains(bodyStr, `"url":"https://example.com/cat.png"`) {
		t.Fatalf("body missing image url:\n%s", bodyStr)
	}
}
