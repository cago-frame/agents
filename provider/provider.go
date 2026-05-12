package provider

import "context"

// Provider ai 模型供应商接口定义
// 目前主要基于OpenAI的接口设计，参数尽量精简方便后续扩展
type Provider interface {
	// Name 返回供应商名称 (如: openai, anthropic)
	Name() string
	// ChatCompletion 同步生成文本
	ChatCompletion(ctx context.Context, req *CompletionRequest) (*CompletionResponse, error)
	// ChatStream 流式生成文本 (返回 channel)
	ChatStream(ctx context.Context, req *CompletionRequest) (<-chan StreamChunk, error)
}
