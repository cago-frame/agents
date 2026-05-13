package openai

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/sashabaranov/go-openai"

	"github.com/cago-frame/agents/provider"
)

// ErrEmptyChoices ChatCompletion 返回 0 条 choice 时的 sentinel；调用方可用
// errors.Is 判定后自行决定重试或上抛。
var ErrEmptyChoices = errors.New("openai: empty choices")

// wrapProviderError 把 go-openai SDK 的 *openai.APIError / *openai.RequestError
// 包装为 *provider.ProviderError，让 agent.RetryPolicy.defaultShouldRetry 能拿到
// StatusCode 走重试白名单（408/425/429/500/502/503/504）。go-openai SDK 不暴露
// 原始 HTTP response headers，因此 Retry-After 留空，cago 会退化到指数退避。
// 非 OpenAI API 错误（io.EOF / 网络错误 / context.Canceled 等）原样透传。
func wrapProviderError(err error) error {
	if err == nil {
		return nil
	}
	var apiErr *openai.APIError
	if errors.As(err, &apiErr) {
		return &provider.ProviderError{
			Err:        err,
			StatusCode: apiErr.HTTPStatusCode,
		}
	}
	var reqErr *openai.RequestError
	if errors.As(err, &reqErr) {
		return &provider.ProviderError{
			Err:        err,
			StatusCode: reqErr.HTTPStatusCode,
		}
	}
	return err
}

// Provider 实现 provider.Provider，基于 github.com/sashabaranov/go-openai 与 OpenAI / OpenAI 兼容接口对接。
type Provider struct {
	client *openai.Client
}

// NewProvider 使用给定配置创建 OpenAI provider。
func NewProvider(config openai.ClientConfig) provider.Provider {
	return &Provider{client: openai.NewClientWithConfig(config)}
}

func (p *Provider) Name() string { return "openai" }

func (p *Provider) ChatCompletion(ctx context.Context, req *provider.CompletionRequest) (*provider.CompletionResponse, error) {
	oaiReq, err := p.buildRequest(req, false)
	if err != nil {
		return nil, err
	}
	resp, err := p.client.CreateChatCompletion(ctx, oaiReq)
	if err != nil {
		return nil, wrapProviderError(err)
	}
	if len(resp.Choices) == 0 {
		return nil, ErrEmptyChoices
	}
	choice := resp.Choices[0]
	return &provider.CompletionResponse{
		Content:      choice.Message.Content,
		Thinking:     fromOAIReasoningContent(choice.Message.ReasoningContent),
		Role:         provider.Role(choice.Message.Role),
		ToolCalls:    fromOAIToolCalls(choice.Message.ToolCalls),
		Usage:        fromOAIUsage(resp.Usage),
		FinishReason: fromOAIFinishReason(string(choice.FinishReason)),
	}, nil
}

func (p *Provider) ChatStream(ctx context.Context, req *provider.CompletionRequest) (<-chan provider.StreamChunk, error) {
	oaiReq, err := p.buildRequest(req, true)
	if err != nil {
		return nil, err
	}
	oaiReq.StreamOptions = &openai.StreamOptions{IncludeUsage: true}
	stream, err := p.client.CreateChatCompletionStream(ctx, oaiReq)
	if err != nil {
		return nil, wrapProviderError(err)
	}
	ch := make(chan provider.StreamChunk, 16)
	go func() {
		defer func() { _ = stream.Close() }()
		defer close(ch)
		// SDK / emit 路径若 panic（基础设施 bug、内存问题等），不能让 channel 被
		// defer close 静默关掉 —— 上游会当成"正常结束"漏掉错误。recover 后 emit
		// 一条 ProviderError，让 Runner 走 retry / EventError 链路。
		defer func() {
			if r := recover(); r != nil {
				select {
				case ch <- provider.StreamChunk{Err: fmt.Errorf("openai provider panic: %v", r)}:
				default:
				}
			}
		}()
		// ctx 取消通过 HTTP 连接传导：go-openai 客户端在 ctx.Done 时关闭底层请求体，
		// stream.Recv() 随之返回 error，本循环自然退出。但若消费方提前放弃 channel，
		// 16-buffer 满后 send 会卡住；send 都用 emit 包了 ctx select 兜底。
		emit := func(chunk provider.StreamChunk) bool {
			select {
			case ch <- chunk:
				return true
			case <-ctx.Done():
				return false
			}
		}
		// finishSeen 跟踪服务端是否已下发 finish_reason —— io.EOF 在 finish_reason 之前
		// 到达视为"流被服务端中途切断"，必须 emit 一个 error chunk 让上游能 retry；
		// finish_reason 之后的 io.EOF 才是真正的正常收尾。go-openai SDK 在两种场景
		// 都返回 io.EOF，自身无法区分。
		finishSeen := false
		for {
			resp, rerr := stream.Recv()
			if errors.Is(rerr, io.EOF) {
				if !finishSeen {
					_ = emit(provider.StreamChunk{Err: io.ErrUnexpectedEOF})
				}
				return
			}
			if rerr != nil {
				_ = emit(provider.StreamChunk{Err: wrapProviderError(rerr)})
				return
			}
			if resp.Usage != nil {
				u := fromOAIUsage(*resp.Usage)
				if !emit(provider.StreamChunk{Usage: &u}) {
					return
				}
			}
			if len(resp.Choices) == 0 {
				continue
			}
			c := resp.Choices[0]
			if c.Delta.ReasoningContent != "" {
				if !emit(provider.StreamChunk{ThinkingDelta: &provider.ThinkingDelta{Text: c.Delta.ReasoningContent}}) {
					return
				}
			}
			if c.Delta.Content != "" {
				if !emit(provider.StreamChunk{ContentDelta: c.Delta.Content}) {
					return
				}
			}
			for _, tc := range c.Delta.ToolCalls {
				idx := 0
				if tc.Index != nil {
					idx = *tc.Index
				}
				if !emit(provider.StreamChunk{
					ToolCallDelta: &provider.ToolCallDelta{
						Index:     idx,
						ID:        tc.ID,
						Name:      tc.Function.Name,
						ArgsDelta: tc.Function.Arguments,
					},
				}) {
					return
				}
			}
			if c.FinishReason != "" {
				finishSeen = true
				if !emit(provider.StreamChunk{FinishReason: fromOAIFinishReason(string(c.FinishReason))}) {
					return
				}
			}
		}
	}()
	return ch, nil
}

// -------- helpers --------

func (p *Provider) buildRequest(req *provider.CompletionRequest, stream bool) (openai.ChatCompletionRequest, error) {
	msgs := make([]openai.ChatCompletionMessage, 0, len(req.Messages))
	for _, m := range req.Messages {
		om := openai.ChatCompletionMessage{
			Role:       string(m.Role),
			Name:       m.Name,
			ToolCallID: m.ToolCallID,
		}
		if len(m.MultiContent) > 0 {
			for _, part := range m.MultiContent {
				switch part.Type {
				case provider.MessagePartText:
					om.MultiContent = append(om.MultiContent, openai.ChatMessagePart{
						Type: openai.ChatMessagePartTypeText,
						Text: part.Text,
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
					om.MultiContent = append(om.MultiContent, openai.ChatMessagePart{
						Type: openai.ChatMessagePartTypeImageURL,
						ImageURL: &openai.ChatMessageImageURL{
							URL: url,
						},
					})
				}
			}
		} else {
			om.Content = m.Content
		}
		if m.Role == provider.RoleAssistant {
			om.ReasoningContent = thinkingText(m.Thinking)
		}
		for _, tc := range m.ToolCalls {
			om.ToolCalls = append(om.ToolCalls, openai.ToolCall{
				ID:   tc.ID,
				Type: openai.ToolType(tc.Type),
				Function: openai.FunctionCall{
					Name:      tc.Function.Name,
					Arguments: tc.Function.Arguments,
				},
			})
		}
		msgs = append(msgs, om)
	}

	var tools []openai.Tool
	for _, t := range req.Tools {
		if t.Function == nil {
			continue
		}
		var params map[string]any
		if len(t.Function.Parameters) > 0 {
			if err := json.Unmarshal(t.Function.Parameters, &params); err != nil {
				return openai.ChatCompletionRequest{}, err
			}
		}
		// 缺 properties 时统一兜底成空对象：部分 OpenAI 兼容代理（OneAPI/new-api
		// 衍生，如 cctranai）会把 "type":"object" 没有 properties 当成 "function
		// parameters is empty" 并回 400。OpenAI 官方不要求，但补上不影响。
		if params == nil {
			params = map[string]any{"type": "object", "properties": map[string]any{}}
		} else if _, ok := params["properties"]; !ok {
			params["properties"] = map[string]any{}
		}
		tools = append(tools, openai.Tool{
			Type: openai.ToolType(t.Type),
			Function: &openai.FunctionDefinition{
				Name:        t.Function.Name,
				Description: t.Function.Description,
				Parameters:  params,
			},
		})
	}

	oaiReq := openai.ChatCompletionRequest{
		Model:    req.Model,
		Messages: msgs,
		Tools:    tools,
		Stream:   stream,
	}
	if req.Temperature != nil {
		oaiReq.Temperature = *req.Temperature
	}
	if req.TopP != nil {
		oaiReq.TopP = *req.TopP
	}
	if req.MaxTokens != nil {
		oaiReq.MaxCompletionTokens = *req.MaxTokens
	}
	if len(req.StopSequences) > 0 {
		oaiReq.Stop = req.StopSequences
	}
	if req.Seed != nil {
		seed := *req.Seed
		oaiReq.Seed = &seed
	}
	if req.ResponseFormat != nil {
		rf, rerr := buildResponseFormat(req.ResponseFormat)
		if rerr != nil {
			return openai.ChatCompletionRequest{}, rerr
		}
		oaiReq.ResponseFormat = rf
	}
	if req.ToolChoice != nil {
		tc, terr := buildToolChoice(req.ToolChoice)
		if terr != nil {
			return openai.ChatCompletionRequest{}, terr
		}
		oaiReq.ToolChoice = tc
	}
	if req.Thinking != nil {
		oaiReq.ReasoningEffort = string(req.Thinking.Effort)
	}
	return oaiReq, nil
}

func buildResponseFormat(rf *provider.ResponseFormat) (*openai.ChatCompletionResponseFormat, error) {
	switch rf.Type {
	case provider.ResponseFormatText:
		return &openai.ChatCompletionResponseFormat{Type: openai.ChatCompletionResponseFormatTypeText}, nil
	case provider.ResponseFormatJSONObject:
		return &openai.ChatCompletionResponseFormat{Type: openai.ChatCompletionResponseFormatTypeJSONObject}, nil
	case provider.ResponseFormatJSONSchema:
		if rf.Schema == nil {
			return nil, errors.New("openai: response_format json_schema requires Schema")
		}
		return &openai.ChatCompletionResponseFormat{
			Type: openai.ChatCompletionResponseFormatTypeJSONSchema,
			JSONSchema: &openai.ChatCompletionResponseFormatJSONSchema{
				Name:        rf.Schema.Name,
				Description: rf.Schema.Description,
				Schema:      json.RawMessage(rf.Schema.Schema),
				Strict:      rf.Schema.Strict,
			},
		}, nil
	default:
		return nil, errors.New("openai: unsupported response_format type: " + string(rf.Type))
	}
}

func buildToolChoice(tc *provider.ToolChoice) (any, error) {
	switch tc.Type {
	case "auto", "required", "none":
		return tc.Type, nil
	case "tool":
		if tc.Name == "" {
			return nil, errors.New("openai: tool_choice type=tool requires Name")
		}
		return map[string]any{
			"type":     "function",
			"function": map[string]any{"name": tc.Name},
		}, nil
	default:
		return nil, fmt.Errorf("openai: unsupported tool_choice type %q", tc.Type)
	}
}

func fromOAIToolCalls(in []openai.ToolCall) []provider.ToolCall {
	if len(in) == 0 {
		return nil
	}
	out := make([]provider.ToolCall, 0, len(in))
	for _, tc := range in {
		out = append(out, provider.ToolCall{
			ID:   tc.ID,
			Type: provider.ToolType(tc.Type),
			Function: provider.ToolCallFunction{
				Name:      tc.Function.Name,
				Arguments: tc.Function.Arguments,
			},
		})
	}
	return out
}

func fromOAIReasoningContent(text string) []provider.ThinkingBlock {
	if text == "" {
		return nil
	}
	return []provider.ThinkingBlock{{Text: text}}
}

func thinkingText(blocks []provider.ThinkingBlock) string {
	var out string
	for _, b := range blocks {
		out += b.Text
	}
	return out
}

func fromOAIUsage(u openai.Usage) provider.Usage {
	out := provider.Usage{
		PromptTokens:     u.PromptTokens,
		CompletionTokens: u.CompletionTokens,
		TotalTokens:      u.TotalTokens,
	}
	if u.CompletionTokensDetails != nil {
		out.ReasoningTokens = u.CompletionTokensDetails.ReasoningTokens
	}
	if u.PromptTokensDetails != nil {
		out.CachedTokens = u.PromptTokensDetails.CachedTokens
	}
	return out
}

func fromOAIFinishReason(s string) provider.FinishReason {
	switch s {
	case "stop":
		return provider.FinishStop
	case "tool_calls":
		return provider.FinishToolCalls
	case "length":
		return provider.FinishLength
	case "content_filter":
		return provider.FinishContentFilter
	case "null", "":
		return ""
	default:
		return provider.FinishReason(s)
	}
}
