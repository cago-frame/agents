package anthropics

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	"github.com/anthropics/anthropic-sdk-go/packages/param"

	"github.com/cago-frame/agents/provider"
)

// defaultMaxTokens Anthropic Messages API 要求 max_tokens 必填，用户未设置时使用的兜底值。
const defaultMaxTokens = 4096

// Provider 实现 provider.Provider，基于 github.com/anthropics/anthropic-sdk-go 与 Anthropic Messages API 对接。
type Provider struct {
	client anthropic.Client
}

type Config struct {
	BaseURL string
	APIKey  string
	// MaxRetries overrides the SDK default (2). Set to 0 to disable retries,
	// which is useful in tests to avoid slow retry backoff on deliberate error
	// responses. Nil leaves the SDK default unchanged.
	MaxRetries *int
}

func NewProvider(config Config) provider.Provider {
	opts := []option.RequestOption{option.WithAPIKey(config.APIKey)}
	if config.BaseURL != "" {
		opts = append(opts, option.WithBaseURL(config.BaseURL))
	}
	if config.MaxRetries != nil {
		opts = append(opts, option.WithMaxRetries(*config.MaxRetries))
	}
	return &Provider{client: anthropic.NewClient(opts...)}
}

// wrapProviderError converts an Anthropic SDK *anthropic.Error into a
// *provider.ProviderError carrying StatusCode and Retry-After so callers can
// honor backoff hints. Non-API errors pass through unchanged.
func wrapProviderError(err error) error {
	if err == nil {
		return nil
	}
	var apiErr *anthropic.Error
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

func (p *Provider) Name() string { return "anthropic" }

func (p *Provider) ChatCompletion(ctx context.Context, req *provider.CompletionRequest) (*provider.CompletionResponse, error) {
	params, err := p.buildRequest(req)
	if err != nil {
		return nil, err
	}
	msg, err := p.client.Messages.New(ctx, params)
	if err != nil {
		return nil, wrapProviderError(err)
	}

	var content strings.Builder
	var thinking []provider.ThinkingBlock
	var toolCalls []provider.ToolCall
	for _, blk := range msg.Content {
		switch blk.Type {
		case "text":
			content.WriteString(blk.Text)
		case "thinking":
			thinking = append(thinking, provider.ThinkingBlock{
				Text:      blk.Thinking,
				Signature: blk.Signature,
			})
		case "tool_use":
			args := string(blk.Input)
			if args == "" {
				args = "{}"
			}
			toolCalls = append(toolCalls, provider.ToolCall{
				ID:   blk.ID,
				Type: provider.ToolTypeFunction,
				Function: provider.ToolCallFunction{
					Name:      blk.Name,
					Arguments: args,
				},
			})
		}
	}

	return &provider.CompletionResponse{
		Content:      content.String(),
		Thinking:     thinking,
		Role:         provider.RoleAssistant,
		ToolCalls:    toolCalls,
		Usage:        fromAnthropicUsage(msg.Usage),
		FinishReason: fromAnthropicStopReason(msg.StopReason),
	}, nil
}

func (p *Provider) ChatStream(ctx context.Context, req *provider.CompletionRequest) (<-chan provider.StreamChunk, error) {
	params, err := p.buildRequest(req)
	if err != nil {
		return nil, err
	}
	stream := p.client.Messages.NewStreaming(ctx, params)
	ch := make(chan provider.StreamChunk, 16)
	go func() {
		defer func() { _ = stream.Close() }()
		defer close(ch)

		// tool_use content block 在 tool_calls 中的序号（流式拼装时消费方用 Index 字段区分）。
		toolIndexByBlock := make(map[int64]int)
		toolCounter := 0

		var usage provider.Usage
		var stopReason provider.FinishReason

		// 所有 send 走 emit，让 ctx 取消时不会卡在 ch <-。SDK 自身会在 ctx.Done
		// 时让 stream.Next() 返回 false，但若消费方放弃 channel，buffer 满后 send
		// 仍会阻塞 — emit 用 select 兜底。
		emit := func(chunk provider.StreamChunk) bool {
			select {
			case ch <- chunk:
				return true
			case <-ctx.Done():
				return false
			}
		}

		for stream.Next() {
			if ctx.Err() != nil {
				return
			}
			ev := stream.Current()
			switch ev.Type {
			case "message_start":
				// message_start 携带 input/cache 的初始统计。
				usage = fromAnthropicUsage(ev.Message.Usage)
			case "content_block_start":
				blk := ev.ContentBlock
				if blk.Type == "tool_use" {
					idx := toolCounter
					toolCounter++
					toolIndexByBlock[ev.Index] = idx
					if !emit(provider.StreamChunk{
						ToolCallDelta: &provider.ToolCallDelta{
							Index: idx,
							ID:    blk.ID,
							Name:  blk.Name,
						},
					}) {
						return
					}
				}
			case "content_block_delta":
				switch ev.Delta.Type {
				case "text_delta":
					if ev.Delta.Text != "" {
						if !emit(provider.StreamChunk{ContentDelta: ev.Delta.Text}) {
							return
						}
					}
				case "thinking_delta":
					if !emit(provider.StreamChunk{ThinkingDelta: &provider.ThinkingDelta{Text: ev.Delta.Thinking}}) {
						return
					}
				case "signature_delta":
					if !emit(provider.StreamChunk{ThinkingDelta: &provider.ThinkingDelta{Signature: ev.Delta.Signature}}) {
						return
					}
				case "input_json_delta":
					idx, ok := toolIndexByBlock[ev.Index]
					if !ok {
						continue
					}
					if !emit(provider.StreamChunk{
						ToolCallDelta: &provider.ToolCallDelta{
							Index:     idx,
							ArgsDelta: ev.Delta.PartialJSON,
						},
					}) {
						return
					}
				}
			case "content_block_stop":
				// 无需额外处理。
			case "message_delta":
				// 终态：stop_reason 在这里到达，usage 的 output / cache 在此更新为累计值。
				stopReason = fromAnthropicStopReason(ev.Delta.StopReason)
				mergeDeltaUsage(&usage, ev.Usage)
			case "message_stop":
				// 流结束。
			}
		}
		if err := stream.Err(); err != nil {
			_ = emit(provider.StreamChunk{Err: wrapProviderError(err)})
			return
		}
		// 最后一块：FinishReason + Usage 可以共存。
		u := usage
		_ = emit(provider.StreamChunk{FinishReason: stopReason, Usage: &u})
	}()
	return ch, nil
}

// -------- helpers --------

func (p *Provider) buildRequest(req *provider.CompletionRequest) (anthropic.MessageNewParams, error) {
	var system []anthropic.TextBlockParam
	var msgs []anthropic.MessageParam
	var pendingToolResults []anthropic.ContentBlockParamUnion
	// agent 层会把单个模型回合（thinking + text + tool_use）拆成多条 assistant
	// provider.Message。Anthropic Messages API 要求 thinking 块与同回合的
	// tool_use 处于同一条 assistant 消息内，否则 400 "The content[].thinking in
	// the thinking mode must be passed back to the API"。这里把连续的 assistant
	// 块累积成单条 anthropic.MessageParam。
	var pendingAssistantBlocks []anthropic.ContentBlockParamUnion

	flushAssistantBlocks := func() {
		if len(pendingAssistantBlocks) > 0 {
			msgs = append(msgs, anthropic.NewAssistantMessage(pendingAssistantBlocks...))
			pendingAssistantBlocks = nil
		}
	}

	flushToolResults := func() {
		if len(pendingToolResults) > 0 {
			msgs = append(msgs, anthropic.NewUserMessage(pendingToolResults...))
			pendingToolResults = nil
		}
	}

	for _, m := range req.Messages {
		switch m.Role {
		case provider.RoleSystem:
			flushAssistantBlocks()
			flushToolResults()
			if m.Content != "" {
				system = append(system, anthropic.TextBlockParam{Text: m.Content})
			}
		case provider.RoleUser:
			flushAssistantBlocks()
			flushToolResults()
			if len(m.MultiContent) > 0 {
				var blocks []anthropic.ContentBlockParamUnion
				for _, part := range m.MultiContent {
					switch part.Type {
					case provider.MessagePartText:
						if part.Text != "" {
							blocks = append(blocks, anthropic.NewTextBlock(part.Text))
						}
					case provider.MessagePartImage:
						if part.Image == nil {
							continue
						}
						if part.Image.URL != "" {
							blocks = append(blocks, anthropic.NewImageBlock(anthropic.URLImageSourceParam{URL: part.Image.URL}))
							continue
						}
						if len(part.Image.Inline) > 0 {
							mt := part.Image.MediaType
							if mt == "" {
								mt = "image/png"
							}
							encoded := base64.StdEncoding.EncodeToString(part.Image.Inline)
							blocks = append(blocks, anthropic.NewImageBlockBase64(mt, encoded))
						}
					}
				}
				if len(blocks) > 0 {
					msgs = append(msgs, anthropic.NewUserMessage(blocks...))
				}
				continue
			}
			if m.Content == "" {
				continue
			}
			msgs = append(msgs, anthropic.NewUserMessage(anthropic.NewTextBlock(m.Content)))
		case provider.RoleAssistant:
			flushToolResults()
			blocks, err := buildAssistantBlocks(m)
			if err != nil {
				return anthropic.MessageNewParams{}, err
			}
			if len(blocks) == 0 {
				continue
			}
			pendingAssistantBlocks = append(pendingAssistantBlocks, blocks...)
		case provider.RoleTool:
			// Anthropic 把 tool 结果放在 user 消息里，连续的 tool 消息合并成单条 user。
			flushAssistantBlocks()
			pendingToolResults = append(pendingToolResults, anthropic.NewToolResultBlock(m.ToolCallID, m.Content, false))
		default:
			return anthropic.MessageNewParams{}, fmt.Errorf("anthropics: unsupported role %q", m.Role)
		}
	}
	flushAssistantBlocks()
	flushToolResults()

	if len(msgs) == 0 {
		return anthropic.MessageNewParams{}, errors.New("anthropics: messages is empty")
	}

	tools, err := buildTools(req.Tools)
	if err != nil {
		return anthropic.MessageNewParams{}, err
	}

	// Cache breakpoint: last system block — preserves behavior ported from OpsKat.
	if n := len(system); n > 0 {
		system[n-1].CacheControl = anthropic.NewCacheControlEphemeralParam()
	}
	// Cache breakpoint: last tool definition — preserves behavior ported from OpsKat.
	if n := len(tools); n > 0 {
		// cago's buildTools only produces OfTool variants; the nil-guard is a safety net
		// for future ToolUnionParam variants (Bash/TextEditor/WebSearch) that don't carry CacheControl.
		if tools[n-1].OfTool != nil {
			tools[n-1].OfTool.CacheControl = anthropic.NewCacheControlEphemeralParam()
		}
	}

	maxTokens := int64(defaultMaxTokens)
	if req.MaxTokens != nil && *req.MaxTokens > 0 {
		maxTokens = int64(*req.MaxTokens)
	}

	params := anthropic.MessageNewParams{
		Model:     anthropic.Model(req.Model),
		Messages:  msgs,
		MaxTokens: maxTokens,
		System:    system,
		Tools:     tools,
	}
	if req.Temperature != nil {
		params.Temperature = param.NewOpt(float64(*req.Temperature))
	}
	if req.TopP != nil {
		params.TopP = param.NewOpt(float64(*req.TopP))
	}
	if len(req.StopSequences) > 0 {
		params.StopSequences = req.StopSequences
	}
	// Anthropic Messages API 不支持 seed / response_format —— 静默忽略，
	// 调用方需要 JSON 输出时请在 system / prompt 中指引或使用 tool_use。
	if req.ToolChoice != nil {
		tc, terr := buildToolChoice(req.ToolChoice)
		if terr != nil {
			return anthropic.MessageNewParams{}, terr
		}
		params.ToolChoice = tc
	}
	if req.Thinking != nil {
		params.Thinking = buildThinking(req.Thinking, maxTokens)
	}
	return params, nil
}

func buildAssistantBlocks(m provider.Message) ([]anthropic.ContentBlockParamUnion, error) {
	var blocks []anthropic.ContentBlockParamUnion
	// thinking 必须在 text / tool_use 之前，才能被下一轮带回验证签名。
	for _, t := range m.Thinking {
		blocks = append(blocks, anthropic.NewThinkingBlock(t.Signature, t.Text))
	}
	if m.Content != "" {
		blocks = append(blocks, anthropic.NewTextBlock(m.Content))
	}
	for _, tc := range m.ToolCalls {
		var input any
		if tc.Function.Arguments != "" {
			if err := json.Unmarshal([]byte(tc.Function.Arguments), &input); err != nil {
				return nil, fmt.Errorf("anthropics: invalid tool arguments for %s: %w", tc.Function.Name, err)
			}
		} else {
			input = map[string]any{}
		}
		blocks = append(blocks, anthropic.NewToolUseBlock(tc.ID, input, tc.Function.Name))
	}
	return blocks, nil
}

func buildTools(tools []provider.Tool) ([]anthropic.ToolUnionParam, error) {
	if len(tools) == 0 {
		return nil, nil
	}
	out := make([]anthropic.ToolUnionParam, 0, len(tools))
	for _, t := range tools {
		if t.Function == nil {
			continue
		}
		schema, err := parseInputSchema(t.Function.Parameters)
		if err != nil {
			return nil, fmt.Errorf("anthropics: invalid schema for tool %s: %w", t.Function.Name, err)
		}
		tp := anthropic.ToolParam{
			Name:        t.Function.Name,
			InputSchema: schema,
		}
		if t.Function.Description != "" {
			tp.Description = param.NewOpt(t.Function.Description)
		}
		out = append(out, anthropic.ToolUnionParam{OfTool: &tp})
	}
	return out, nil
}

func parseInputSchema(raw json.RawMessage) (anthropic.ToolInputSchemaParam, error) {
	// 兜底成空对象而不是 nil：Anthropic SDK 对 Properties=nil 走 omitzero，
	// 序列化出 {"type":"object"} 不带 properties 字段；部分代理（cctranai 等）
	// 会因此回 400 "function parameters is empty"。空 map 序列化为 "properties":{}，
	// 原生 API 与所有已知代理都接受。
	schema := anthropic.ToolInputSchemaParam{Properties: map[string]any{}}
	if len(raw) == 0 {
		return schema, nil
	}
	var obj map[string]any
	if err := json.Unmarshal(raw, &obj); err != nil {
		return schema, err
	}
	if props, ok := obj["properties"]; ok {
		schema.Properties = props
	}
	if req, ok := obj["required"].([]any); ok {
		required := make([]string, 0, len(req))
		for _, v := range req {
			if s, ok := v.(string); ok {
				required = append(required, s)
			}
		}
		schema.Required = required
	}
	// 保留其余字段（如 $defs、additionalProperties），Anthropic 侧可能需要。
	for k, v := range obj {
		if k == "properties" || k == "required" || k == "type" {
			continue
		}
		if schema.ExtraFields == nil {
			schema.ExtraFields = map[string]any{}
		}
		schema.ExtraFields[k] = v
	}
	return schema, nil
}

func buildToolChoice(tc *provider.ToolChoice) (anthropic.ToolChoiceUnionParam, error) {
	switch tc.Type {
	case "auto":
		return anthropic.ToolChoiceUnionParam{OfAuto: &anthropic.ToolChoiceAutoParam{}}, nil
	case "required":
		return anthropic.ToolChoiceUnionParam{OfAny: &anthropic.ToolChoiceAnyParam{}}, nil
	case "none":
		return anthropic.ToolChoiceUnionParam{OfNone: &anthropic.ToolChoiceNoneParam{}}, nil
	case "tool":
		if tc.Name == "" {
			return anthropic.ToolChoiceUnionParam{}, errors.New("anthropics: tool_choice type=tool requires Name")
		}
		return anthropic.ToolChoiceUnionParam{OfTool: &anthropic.ToolChoiceToolParam{Name: tc.Name}}, nil
	default:
		return anthropic.ToolChoiceUnionParam{}, fmt.Errorf("anthropics: unsupported tool_choice type %q", tc.Type)
	}
}

func buildThinking(cfg *provider.ThinkingConfig, maxTokens int64) anthropic.ThinkingConfigParamUnion {
	var budget int64
	switch cfg.Effort {
	case provider.ThinkingLow:
		budget = 1024
	case provider.ThinkingMedium:
		budget = 4096
	case provider.ThinkingHigh:
		budget = 16000
	case provider.ThinkingXHigh:
		budget = 32000
	case provider.ThinkingMax:
		budget = 64000
	default:
		budget = 1024
	}
	// Anthropic 要求 budget_tokens 必须严格小于 max_tokens。
	if budget >= maxTokens {
		budget = maxTokens - 1
	}
	if budget < 1024 {
		return anthropic.ThinkingConfigParamUnion{OfDisabled: &anthropic.ThinkingConfigDisabledParam{}}
	}
	return anthropic.ThinkingConfigParamOfEnabled(budget)
}

func fromAnthropicUsage(u anthropic.Usage) provider.Usage {
	return provider.Usage{
		PromptTokens:        int(u.InputTokens),
		CompletionTokens:    int(u.OutputTokens),
		CachedTokens:        int(u.CacheReadInputTokens),
		CacheCreationTokens: int(u.CacheCreationInputTokens),
		TotalTokens:         int(u.InputTokens) + int(u.CacheCreationInputTokens) + int(u.CacheReadInputTokens) + int(u.OutputTokens),
	}
}

// extractUsageFromDelta maps Anthropic MessageDeltaUsage onto provider.Usage.
// PromptTokens carries raw new input only; cache write/read are exposed separately.
func extractUsageFromDelta(u anthropic.MessageDeltaUsage) provider.Usage {
	return provider.Usage{
		PromptTokens:        int(u.InputTokens),
		CompletionTokens:    int(u.OutputTokens),
		CachedTokens:        int(u.CacheReadInputTokens),
		CacheCreationTokens: int(u.CacheCreationInputTokens),
		TotalTokens:         int(u.InputTokens) + int(u.CacheCreationInputTokens) + int(u.CacheReadInputTokens) + int(u.OutputTokens),
	}
}

// mergeDeltaUsage applies the streaming delta to the running usage by overwriting
// each non-zero field — the API reports cumulative counts on each delta.
func mergeDeltaUsage(dst *provider.Usage, u anthropic.MessageDeltaUsage) {
	got := extractUsageFromDelta(u)
	// Anthropic always sends InputTokens alongside any cache counts, so this
	// single guard correctly gates updating PromptTokens, CachedTokens, and
	// CacheCreationTokens together.
	if u.InputTokens > 0 {
		dst.PromptTokens = got.PromptTokens
		dst.CachedTokens = got.CachedTokens
		dst.CacheCreationTokens = got.CacheCreationTokens
	}
	if u.OutputTokens > 0 {
		dst.CompletionTokens = got.CompletionTokens
	}
	dst.TotalTokens = dst.PromptTokens + dst.CacheCreationTokens + dst.CachedTokens + dst.CompletionTokens
}

func fromAnthropicStopReason(s anthropic.StopReason) provider.FinishReason {
	switch s {
	case anthropic.StopReasonEndTurn, anthropic.StopReasonStopSequence, anthropic.StopReasonPauseTurn:
		return provider.FinishStop
	case anthropic.StopReasonMaxTokens:
		return provider.FinishLength
	case anthropic.StopReasonToolUse:
		return provider.FinishToolCalls
	case anthropic.StopReasonRefusal:
		return provider.FinishContentFilter
	case "":
		return ""
	default:
		return provider.FinishReason(s)
	}
}
