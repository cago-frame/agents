// Package openai_response 实现基于 OpenAI 新版 /v1/responses 端点的 provider。
//
// 与 provider/openai 的差异：
//   - 端点 /v1/responses 而不是 /v1/chat/completions；
//   - 请求 body 结构不同（input/instructions 替代 messages、tools 字段扁平、
//     reasoning.effort 替代 reasoning_effort 等）；
//   - 使用官方 SDK github.com/openai/openai-go（其 responses 子包原生支持）。
//
// 仅支持 OpenAI 一系（含 OpenAI 兼容端实际实现 /v1/responses 的代理；目前 Ollama /
// vLLM 等大多数兼容端尚未支持，请用 provider/openai 走 chat/completions）。
package openai_response

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/openai/openai-go"
	"github.com/openai/openai-go/option"
	"github.com/openai/openai-go/packages/param"
	"github.com/openai/openai-go/packages/ssestream"
	"github.com/openai/openai-go/responses"
	"github.com/openai/openai-go/shared"
	"github.com/openai/openai-go/shared/constant"

	"github.com/cago-frame/agents/provider"
)

// Config 与 anthropics.Config / openai.ClientConfig 风格一致：消费方只关心 BaseURL +
// APIKey；MaxRetries 显式覆盖 SDK 默认值（2），测试里设 0 可以避免在错误响应上踩慢退避。
type Config struct {
	BaseURL    string
	APIKey     string
	MaxRetries *int
}

// Provider 实现 provider.Provider，基于 github.com/openai/openai-go responses 子包
// 与 OpenAI Responses API 对接。
type Provider struct {
	client openai.Client
}

// NewProvider 使用给定配置创建 Responses API provider。
func NewProvider(cfg Config) provider.Provider {
	opts := []option.RequestOption{option.WithAPIKey(cfg.APIKey)}
	if cfg.BaseURL != "" {
		opts = append(opts, option.WithBaseURL(cfg.BaseURL))
	}
	if cfg.MaxRetries != nil {
		opts = append(opts, option.WithMaxRetries(*cfg.MaxRetries))
	}
	return &Provider{client: openai.NewClient(opts...)}
}

func (p *Provider) Name() string { return "openai-response" }

// wrapProviderError 把 openai-go SDK 的 *openai.Error 包装为 *provider.ProviderError，
// 让 agent.RetryPolicy.defaultShouldRetry 能拿到 StatusCode 走重试白名单
// （408/425/429/500/502/503/504）。Retry-After 透传以兼顾上游退避建议。
// 非 SDK 错误（io.EOF / 网络错误 / context.Canceled 等）原样透传。
func wrapProviderError(err error) error {
	if err == nil {
		return nil
	}
	var apiErr *openai.Error
	if !errors.As(err, &apiErr) {
		return err
	}
	var retryAfter string
	if apiErr.Response != nil {
		retryAfter = apiErr.Response.Header.Get("Retry-After")
	}
	return &provider.ProviderError{
		Err:        err,
		RetryAfter: retryAfter,
		StatusCode: apiErr.StatusCode,
	}
}

func (p *Provider) ChatCompletion(ctx context.Context, req *provider.CompletionRequest) (*provider.CompletionResponse, error) {
	params, err := p.buildRequest(req)
	if err != nil {
		return nil, err
	}
	resp, err := p.client.Responses.New(ctx, params)
	if err != nil {
		return nil, wrapProviderError(err)
	}
	out := &provider.CompletionResponse{
		Role:         provider.RoleAssistant,
		Usage:        fromResponseUsage(resp.Usage),
		FinishReason: fromResponseStatus(resp),
	}
	var contentBuilder strings.Builder
	for _, item := range resp.Output {
		switch item.Type {
		case "message":
			msg := item.AsMessage()
			for _, c := range msg.Content {
				switch c.Type {
				case "output_text":
					contentBuilder.WriteString(c.Text)
				case "refusal":
					// 拒答按内容过滤处理；refusal 的 Text 字段在 union 里有，但没有
					// 单独的 finish_reason，所以这里覆盖掉 status 推出的 finish。
					out.FinishReason = provider.FinishContentFilter
				}
			}
		case "reasoning":
			r := item.AsReasoning()
			for _, s := range r.Summary {
				out.Thinking = append(out.Thinking, provider.ThinkingBlock{Text: s.Text})
			}
		case "function_call":
			fc := item.AsFunctionCall()
			args := fc.Arguments
			if args == "" {
				args = "{}"
			}
			out.ToolCalls = append(out.ToolCalls, provider.ToolCall{
				ID:   fc.CallID,
				Type: provider.ToolTypeFunction,
				Function: provider.ToolCallFunction{
					Name:      fc.Name,
					Arguments: args,
				},
			})
		}
	}
	out.Content = contentBuilder.String()
	if len(out.ToolCalls) > 0 && out.FinishReason == provider.FinishStop {
		out.FinishReason = provider.FinishToolCalls
	}
	return out, nil
}

func (p *Provider) ChatStream(ctx context.Context, req *provider.CompletionRequest) (<-chan provider.StreamChunk, error) {
	params, err := p.buildRequest(req)
	if err != nil {
		return nil, err
	}
	stream := p.client.Responses.NewStreaming(ctx, params)
	ch := make(chan provider.StreamChunk, 16)
	go consumeStream(ctx, stream, ch)
	return ch, nil
}

// consumeStream 把 openai-go 的 ssestream 转成 cago 的 StreamChunk 通道。提取成
// 独立函数方便单测注入伪造的 ssestream（实际场景由 ChatStream 调用）。
func consumeStream(ctx context.Context, stream *ssestream.Stream[responses.ResponseStreamEventUnion], ch chan provider.StreamChunk) {
	defer func() { _ = stream.Close() }()
	defer close(ch)
	defer func() {
		if r := recover(); r != nil {
			select {
			case ch <- provider.StreamChunk{Err: fmt.Errorf("openai_response provider panic: %v", r)}:
			default:
			}
		}
	}()

	emit := func(chunk provider.StreamChunk) bool {
		select {
		case ch <- chunk:
			return true
		case <-ctx.Done():
			return false
		}
	}

	// 同一条 function_call 的 arguments 分多次到达；用 item_id 维护一个稳定的
	// ToolCallDelta.Index，让消费方按 cago 的 index 协议拼装。
	toolIndexByItem := make(map[string]int)
	toolCounter := 0

	var finalUsage provider.Usage
	var finalFinish provider.FinishReason

	for stream.Next() {
		if ctx.Err() != nil {
			return
		}
		ev := stream.Current()
		switch ev.Type {
		case "response.output_text.delta":
			if ev.Delta.OfString != "" {
				if !emit(provider.StreamChunk{ContentDelta: ev.Delta.OfString}) {
					return
				}
			}
		case "response.reasoning_summary_text.delta", "response.reasoning_summary.delta":
			if ev.Delta.OfString != "" {
				if !emit(provider.StreamChunk{ThinkingDelta: &provider.ThinkingDelta{Text: ev.Delta.OfString}}) {
					return
				}
			}
		case "response.output_item.added":
			if ev.Item.Type == "function_call" {
				idx := toolCounter
				toolCounter++
				toolIndexByItem[ev.Item.ID] = idx
				if !emit(provider.StreamChunk{
					ToolCallDelta: &provider.ToolCallDelta{
						Index: idx,
						ID:    ev.Item.CallID,
						Name:  ev.Item.Name,
					},
				}) {
					return
				}
			}
		case "response.function_call_arguments.delta":
			idx, ok := toolIndexByItem[ev.ItemID]
			if !ok {
				idx = toolCounter
				toolCounter++
				toolIndexByItem[ev.ItemID] = idx
			}
			if ev.Delta.OfString != "" {
				if !emit(provider.StreamChunk{
					ToolCallDelta: &provider.ToolCallDelta{
						Index:     idx,
						ArgsDelta: ev.Delta.OfString,
					},
				}) {
					return
				}
			}
		case "response.completed":
			finalUsage = fromResponseUsage(ev.Response.Usage)
			finalFinish = fromResponseStatus(&ev.Response)
		case "response.failed":
			msg := ev.Response.Error.Message
			if msg == "" {
				msg = "openai_response: stream failed"
			}
			_ = emit(provider.StreamChunk{Err: errors.New(msg)})
			return
		case "response.incomplete":
			finalUsage = fromResponseUsage(ev.Response.Usage)
			finalFinish = provider.FinishLength
		case "error":
			msg := ev.Message
			if msg == "" {
				msg = "openai_response: stream error"
			}
			_ = emit(provider.StreamChunk{Err: errors.New(msg)})
			return
		}
	}
	if err := stream.Err(); err != nil {
		_ = emit(provider.StreamChunk{Err: wrapProviderError(err)})
		return
	}
	u := finalUsage
	_ = emit(provider.StreamChunk{FinishReason: finalFinish, Usage: &u})
}

// -------- helpers --------

func (p *Provider) buildRequest(req *provider.CompletionRequest) (responses.ResponseNewParams, error) {
	out := responses.ResponseNewParams{
		Model: shared.ResponsesModel(req.Model),
	}

	// system messages 合并到 instructions 里（多条用换行分隔，OpenAI 行为与
	// chat completions 的 system role 等价）。
	var instructions []string
	var inputItems []responses.ResponseInputItemUnionParam
	for _, m := range req.Messages {
		switch m.Role {
		case provider.RoleSystem:
			if m.Content != "" {
				instructions = append(instructions, m.Content)
			}
		case provider.RoleUser:
			content, err := buildUserContent(m)
			if err != nil {
				return responses.ResponseNewParams{}, err
			}
			if len(content) == 0 {
				continue
			}
			inputItems = append(inputItems, responses.ResponseInputItemUnionParam{
				OfInputMessage: &responses.ResponseInputItemMessageParam{
					Role:    "user",
					Content: content,
				},
			})
		case provider.RoleAssistant:
			items := buildAssistantItems(m)
			inputItems = append(inputItems, items...)
		case provider.RoleTool:
			inputItems = append(inputItems, responses.ResponseInputItemUnionParam{
				OfFunctionCallOutput: &responses.ResponseInputItemFunctionCallOutputParam{
					CallID: m.ToolCallID,
					Output: m.Content,
				},
			})
		default:
			return responses.ResponseNewParams{}, fmt.Errorf("openai_response: unsupported role %q", m.Role)
		}
	}

	if len(instructions) > 0 {
		out.Instructions = param.NewOpt(strings.Join(instructions, "\n\n"))
	}
	if len(inputItems) > 0 {
		out.Input = responses.ResponseNewParamsInputUnion{OfInputItemList: inputItems}
	}

	tools, err := buildTools(req.Tools)
	if err != nil {
		return responses.ResponseNewParams{}, err
	}
	out.Tools = tools

	if req.Temperature != nil {
		out.Temperature = param.NewOpt(float64(*req.Temperature))
	}
	if req.TopP != nil {
		out.TopP = param.NewOpt(float64(*req.TopP))
	}
	if req.MaxTokens != nil && *req.MaxTokens > 0 {
		out.MaxOutputTokens = param.NewOpt(int64(*req.MaxTokens))
	}
	// Responses API 的 ResponseNewParams 没有暴露 stop / seed 字段（截至 openai-go
	// v1.12.0 / API 当前版本）—— OpenAI 把这两项归到 Chat Completions 才支持。这里
	// 静默忽略，调用方需要时改用 provider/openai。

	if req.ResponseFormat != nil {
		text, ferr := buildTextConfig(req.ResponseFormat)
		if ferr != nil {
			return responses.ResponseNewParams{}, ferr
		}
		out.Text = text
	}

	if req.ToolChoice != nil {
		tc, terr := buildToolChoice(req.ToolChoice)
		if terr != nil {
			return responses.ResponseNewParams{}, terr
		}
		out.ToolChoice = tc
	}

	if req.Thinking != nil {
		out.Reasoning = shared.ReasoningParam{Effort: mapEffort(req.Thinking.Effort)}
	}

	return out, nil
}

func buildUserContent(m provider.Message) (responses.ResponseInputMessageContentListParam, error) {
	if len(m.MultiContent) == 0 {
		if m.Content == "" {
			return nil, nil
		}
		return responses.ResponseInputMessageContentListParam{
			{OfInputText: &responses.ResponseInputTextParam{Text: m.Content}},
		}, nil
	}
	out := make(responses.ResponseInputMessageContentListParam, 0, len(m.MultiContent))
	for _, part := range m.MultiContent {
		switch part.Type {
		case provider.MessagePartText:
			if part.Text == "" {
				continue
			}
			out = append(out, responses.ResponseInputContentUnionParam{
				OfInputText: &responses.ResponseInputTextParam{Text: part.Text},
			})
		case provider.MessagePartImage:
			if part.Image == nil {
				continue
			}
			url := part.Image.URL
			if url == "" && len(part.Image.Inline) > 0 {
				mt := part.Image.MediaType
				if mt == "" {
					mt = "image/png"
				}
				url = "data:" + mt + ";base64," + base64.StdEncoding.EncodeToString(part.Image.Inline)
			}
			if url == "" {
				continue
			}
			out = append(out, responses.ResponseInputContentUnionParam{
				OfInputImage: &responses.ResponseInputImageParam{
					ImageURL: param.NewOpt(url),
					Detail:   responses.ResponseInputImageDetailAuto,
				},
			})
		}
	}
	return out, nil
}

// buildAssistantItems 把 assistant 回合拆成 Responses API 期待的 input items：
// thinking → reasoning item（如果带 signature 当 encrypted_content）；
// content → output_message item；
// tool_calls → function_call items。
//
// agent 层一个回合通常一条 assistant message 携带 thinking + content + tool_calls，
// Responses API 也允许它们作为相邻的 input items 传回。
func buildAssistantItems(m provider.Message) []responses.ResponseInputItemUnionParam {
	var items []responses.ResponseInputItemUnionParam
	for i, t := range m.Thinking {
		if t.Text == "" && t.Signature == "" {
			continue
		}
		ri := &responses.ResponseReasoningItemParam{
			ID:      fmt.Sprintf("rs_local_%d", i),
			Summary: []responses.ResponseReasoningItemSummaryParam{{Text: t.Text}},
		}
		if t.Signature != "" {
			ri.EncryptedContent = param.NewOpt(t.Signature)
		}
		items = append(items, responses.ResponseInputItemUnionParam{OfReasoning: ri})
	}
	if m.Content != "" {
		items = append(items, responses.ResponseInputItemUnionParam{
			OfOutputMessage: &responses.ResponseOutputMessageParam{
				ID:     "msg_local",
				Status: "completed",
				Content: []responses.ResponseOutputMessageContentUnionParam{
					{OfOutputText: &responses.ResponseOutputTextParam{Text: m.Content}},
				},
			},
		})
	}
	for _, tc := range m.ToolCalls {
		args := tc.Function.Arguments
		if args == "" {
			args = "{}"
		}
		items = append(items, responses.ResponseInputItemUnionParam{
			OfFunctionCall: &responses.ResponseFunctionToolCallParam{
				CallID:    tc.ID,
				Name:      tc.Function.Name,
				Arguments: args,
			},
		})
	}
	return items
}

func buildTools(tools []provider.Tool) ([]responses.ToolUnionParam, error) {
	if len(tools) == 0 {
		return nil, nil
	}
	out := make([]responses.ToolUnionParam, 0, len(tools))
	for _, t := range tools {
		if t.Function == nil {
			continue
		}
		var params map[string]any
		if len(t.Function.Parameters) > 0 {
			if err := json.Unmarshal(t.Function.Parameters, &params); err != nil {
				return nil, fmt.Errorf("openai_response: invalid schema for tool %s: %w", t.Function.Name, err)
			}
		}
		// 与 provider/openai 一致：缺 properties 时兜底空对象，避免某些代理
		// 把缺失当成 "function parameters is empty" 回 400。
		if params == nil {
			params = map[string]any{"type": "object", "properties": map[string]any{}}
		} else if _, ok := params["properties"]; !ok {
			params["properties"] = map[string]any{}
		}
		fn := &responses.FunctionToolParam{
			Name:       t.Function.Name,
			Parameters: params,
		}
		if t.Function.Description != "" {
			fn.Description = param.NewOpt(t.Function.Description)
		}
		out = append(out, responses.ToolUnionParam{OfFunction: fn})
	}
	return out, nil
}

func buildToolChoice(tc *provider.ToolChoice) (responses.ResponseNewParamsToolChoiceUnion, error) {
	switch tc.Type {
	case "auto":
		return responses.ResponseNewParamsToolChoiceUnion{
			OfToolChoiceMode: param.NewOpt(responses.ToolChoiceOptionsAuto),
		}, nil
	case "required":
		return responses.ResponseNewParamsToolChoiceUnion{
			OfToolChoiceMode: param.NewOpt(responses.ToolChoiceOptionsRequired),
		}, nil
	case "none":
		return responses.ResponseNewParamsToolChoiceUnion{
			OfToolChoiceMode: param.NewOpt(responses.ToolChoiceOptionsNone),
		}, nil
	case "tool":
		if tc.Name == "" {
			return responses.ResponseNewParamsToolChoiceUnion{}, errors.New("openai_response: tool_choice type=tool requires Name")
		}
		return responses.ResponseNewParamsToolChoiceUnion{
			OfFunctionTool: &responses.ToolChoiceFunctionParam{Name: tc.Name},
		}, nil
	default:
		return responses.ResponseNewParamsToolChoiceUnion{}, fmt.Errorf("openai_response: unsupported tool_choice type %q", tc.Type)
	}
}

func buildTextConfig(rf *provider.ResponseFormat) (responses.ResponseTextConfigParam, error) {
	switch rf.Type {
	case provider.ResponseFormatText:
		return responses.ResponseTextConfigParam{
			Format: responses.ResponseFormatTextConfigUnionParam{
				OfText: &shared.ResponseFormatTextParam{},
			},
		}, nil
	case provider.ResponseFormatJSONObject:
		return responses.ResponseTextConfigParam{
			Format: responses.ResponseFormatTextConfigUnionParam{
				OfJSONObject: &shared.ResponseFormatJSONObjectParam{},
			},
		}, nil
	case provider.ResponseFormatJSONSchema:
		if rf.Schema == nil {
			return responses.ResponseTextConfigParam{}, errors.New("openai_response: response_format json_schema requires Schema")
		}
		var schema map[string]any
		if len(rf.Schema.Schema) > 0 {
			if err := json.Unmarshal(rf.Schema.Schema, &schema); err != nil {
				return responses.ResponseTextConfigParam{}, fmt.Errorf("openai_response: invalid json_schema: %w", err)
			}
		}
		js := &responses.ResponseFormatTextJSONSchemaConfigParam{
			Name:   rf.Schema.Name,
			Schema: schema,
		}
		if rf.Schema.Description != "" {
			js.Description = param.NewOpt(rf.Schema.Description)
		}
		if rf.Schema.Strict {
			js.Strict = param.NewOpt(true)
		}
		return responses.ResponseTextConfigParam{
			Format: responses.ResponseFormatTextConfigUnionParam{OfJSONSchema: js},
		}, nil
	default:
		return responses.ResponseTextConfigParam{}, errors.New("openai_response: unsupported response_format type: " + string(rf.Type))
	}
}

// mapEffort 把 cago 的 5 档 ThinkingEffort 折叠到 OpenAI 的 3 档（low/medium/high）。
// xhigh / max 上溯到 high —— Responses API 当前没有更高档；如未来 SDK 暴露更细的
// 档位再调整。
func mapEffort(e provider.ThinkingEffort) shared.ReasoningEffort {
	switch e {
	case provider.ThinkingLow:
		return shared.ReasoningEffortLow
	case provider.ThinkingMedium:
		return shared.ReasoningEffortMedium
	case provider.ThinkingHigh, provider.ThinkingXHigh, provider.ThinkingMax:
		return shared.ReasoningEffortHigh
	default:
		return shared.ReasoningEffortMedium
	}
}

func fromResponseUsage(u responses.ResponseUsage) provider.Usage {
	return provider.Usage{
		PromptTokens:     int(u.InputTokens),
		CompletionTokens: int(u.OutputTokens),
		TotalTokens:      int(u.TotalTokens),
		ReasoningTokens:  int(u.OutputTokensDetails.ReasoningTokens),
		CachedTokens:     int(u.InputTokensDetails.CachedTokens),
	}
}

// fromResponseStatus 把 Response.Status / IncompleteDetails 映射成 cago 的 FinishReason。
// 出现 function_call 时调用方会 override 为 FinishToolCalls。
func fromResponseStatus(r *responses.Response) provider.FinishReason {
	switch r.Status {
	case responses.ResponseStatusCompleted, "":
		return provider.FinishStop
	case responses.ResponseStatusIncomplete:
		switch r.IncompleteDetails.Reason {
		case "max_output_tokens":
			return provider.FinishLength
		case "content_filter":
			return provider.FinishContentFilter
		default:
			return provider.FinishLength
		}
	case responses.ResponseStatusFailed, responses.ResponseStatusCancelled:
		return provider.FinishContentFilter
	default:
		return provider.FinishReason(r.Status)
	}
}

// 防止 "imported and not used" —— constant 包用于 SDK 内部序列化校验，本文件没有
// 直接引用其常量，但保留 import 让未来扩展（比如 web_search/file_search 等内置工具）
// 不需要重新加。
var _ = constant.Function("function")
