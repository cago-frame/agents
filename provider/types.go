package provider

import "encoding/json"

// Role 角色枚举
type Role string

const (
	RoleSystem    Role = "system"
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleTool      Role = "tool"
)

// ContentType 内容类型
type ContentType string

const (
	ContentTypeText     ContentType = "text"
	ContentTypeImageURL ContentType = "image_url"
)

type ToolType string

const (
	ToolTypeFunction ToolType = "function"
)

// Tool 工具定义（发给模型的 schema）
type Tool struct {
	Type     ToolType            `json:"type"`
	Function *FunctionDefinition `json:"function,omitempty"`
}

type FunctionDefinition struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters,omitempty"` // JSON Schema
}

// ToolCall 模型请求执行的工具调用
type ToolCall struct {
	ID       string           `json:"id"`
	Type     ToolType         `json:"type"`
	Function ToolCallFunction `json:"function"`
}

type ToolCallFunction struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"` // 原始 JSON 字符串（模型输出）
}

// ToolChoice 工具选择策略
type ToolChoice struct {
	Type string `json:"type"` // "auto" | "required" | "none" | "tool"
	Name string `json:"name,omitempty"`
}

// ThinkingBlock 思考块（Anthropic extended thinking 等）
type ThinkingBlock struct {
	Text      string `json:"text"`
	Signature string `json:"signature,omitempty"` // Anthropic 需要在下一轮原样传回
}

// ThinkingConfig 统一的思考配置。各 provider 自行翻译。
type ThinkingConfig struct {
	Effort ThinkingEffort `json:"effort"`
}

type ThinkingEffort string

const (
	ThinkingLow    ThinkingEffort = "low"
	ThinkingMedium ThinkingEffort = "medium"
	ThinkingHigh   ThinkingEffort = "high"
	// ThinkingXHigh 超高思考力度。Anthropic 映射到 32000 token budget；
	// OpenAI 兼容端按字符串透传（"xhigh"），实际是否生效由具体 API 决定。
	ThinkingXHigh ThinkingEffort = "xhigh"
	// ThinkingMax 顶格思考力度。Anthropic 映射到 64000 token budget（受 maxTokens 截断）；
	// OpenAI 兼容端按字符串透传（"max"，GPT-5 等支持）。
	ThinkingMax ThinkingEffort = "max"
)

// MessagePartType 多模态消息片段的类型枚举。
type MessagePartType string

const (
	MessagePartText  MessagePartType = "text"
	MessagePartImage MessagePartType = "image"
)

// MessageImage 图像内容。URL 与 Inline 互斥；Inline 时 MediaType 必填（如
// "image/png"）。URL 模式 MediaType 可省略，由对端 fetch 时自行判定。
type MessageImage struct {
	URL       string `json:"url,omitempty"`
	MediaType string `json:"media_type,omitempty"`
	Inline    []byte `json:"inline,omitempty"`
}

// MessagePart 多模态消息内容的单个片段。Type 决定哪个字段生效：
//
//	MessagePartText  -> Text
//	MessagePartImage -> Image
type MessagePart struct {
	Type  MessagePartType `json:"type"`
	Text  string          `json:"text,omitempty"`
	Image *MessageImage   `json:"image,omitempty"`
}

// Message 统一的消息结构
type Message struct {
	Role    Role   `json:"role"`
	Content string `json:"content,omitempty"`
	// MultiContent 多模态内容片段（图像 + 文本）。当 MultiContent 非空时，
	// provider 实现应优先使用 MultiContent 构造请求，忽略 Content；保留
	// Content 字段以维持现有纯文本调用路径的二进制兼容。
	MultiContent []MessagePart   `json:"multi_content,omitempty"`
	Thinking     []ThinkingBlock `json:"thinking,omitempty"`
	ToolCalls    []ToolCall      `json:"tool_calls,omitempty"`
	ToolCallID   string          `json:"tool_call_id,omitempty"` // role=tool 时必填
	Name         string          `json:"name,omitempty"`         // role=tool 时可选，便于调试
}

// CompletionRequest 统一的请求结构
type CompletionRequest struct {
	Model       string
	Messages    []Message
	Tools       []Tool
	ToolChoice  *ToolChoice
	Temperature *float32
	TopP        *float32
	MaxTokens   *int
	// StopSequences 自定义停止序列。OpenAI 映射为 stop，Anthropic 映射为 stop_sequences。
	StopSequences []string
	// Seed 采样种子，仅 OpenAI 系列原生支持；Anthropic 等不支持的 provider 会忽略。
	Seed *int
	// ResponseFormat 强制输出格式（JSON mode / JSON Schema）。
	// 仅 OpenAI 系列原生支持；Anthropic 等不支持的 provider 会忽略，需要时请用 prompt 指引。
	ResponseFormat *ResponseFormat
	Thinking       *ThinkingConfig
}

// ResponseFormatType 输出格式类型
type ResponseFormatType string

const (
	ResponseFormatText       ResponseFormatType = "text"
	ResponseFormatJSONObject ResponseFormatType = "json_object"
	ResponseFormatJSONSchema ResponseFormatType = "json_schema"
)

// ResponseFormat 输出格式约束。
type ResponseFormat struct {
	Type   ResponseFormatType `json:"type"`
	Schema *ResponseSchema    `json:"json_schema,omitempty"` // Type == json_schema 时必填
}

// ResponseSchema JSON Schema 输出（OpenAI structured output）。
type ResponseSchema struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Schema      json.RawMessage `json:"schema"`
	Strict      bool            `json:"strict,omitempty"`
}

// FinishReason 模型停止原因
type FinishReason string

const (
	FinishStop          FinishReason = "stop"
	FinishToolCalls     FinishReason = "tool_calls"
	FinishLength        FinishReason = "length"
	FinishContentFilter FinishReason = "content_filter"
)

// Usage token 消耗统计
type Usage struct {
	PromptTokens        int
	CompletionTokens    int
	ReasoningTokens     int // 思考 token（独立）
	CachedTokens        int // prompt cache 命中（read）
	CacheCreationTokens int // prompt cache 写入（Anthropic only today）
	TotalTokens         int
}

// CompletionResponse 同步返回
type CompletionResponse struct {
	Content      string
	Thinking     []ThinkingBlock
	Role         Role
	ToolCalls    []ToolCall
	Usage        Usage
	FinishReason FinishReason
}

// ThinkingDelta 流式思考增量
type ThinkingDelta struct {
	Text      string
	Signature string
}

// ToolCallDelta 流式 tool call 增量
type ToolCallDelta struct {
	Index     int    // 第几个 tool call（流式拼装用）
	ID        string // 首块带（后续块为空）
	Name      string // 首块带
	ArgsDelta string // 每块追加的参数片段
}

// ProviderError wraps API errors with retry metadata. Provider implementations
// should return a *ProviderError on StreamChunk.Err (or as the error from
// ChatCompletion) when the upstream returned a transient HTTP status carrying
// Retry-After, so the caller can honor backoff hints.
type ProviderError struct {
	Err        error
	RetryAfter string // raw Retry-After header value (seconds or HTTP-date)
	StatusCode int
}

func (e *ProviderError) Error() string { return e.Err.Error() }
func (e *ProviderError) Unwrap() error { return e.Err }

// StreamChunk 流式返回的片段。字段只会有一个非零（FinishReason+Usage 可共存于最后一块）。
//
// Usage 语义：**累计快照**，不是增量。每个 provider 实现承诺 Usage 字段携带的是
// 当前流到此为止的累计值（OpenAI 仅在末尾 IncludeUsage 发一次终态；Anthropic 在
// goroutine 内部把 message_start + message_delta 合并到末尾的单一 chunk）。
// 消费方收到多个非 nil Usage 时应**覆盖**而不是累加；agent.consumeStream 即按此
// 假设直接覆盖 usage 指针。
//
// Err 语义：non-nil 表示流已因错误终止，本 chunk 的其它字段忽略；channel 也会随后
// close。Err 与 io.EOF 不同义 —— EOF 由 producer 内部消化，不经此字段透出。
type StreamChunk struct {
	ContentDelta  string
	ThinkingDelta *ThinkingDelta
	ToolCallDelta *ToolCallDelta

	FinishReason FinishReason
	Usage        *Usage
	Err          error
}
