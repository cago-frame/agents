package openai

import (
	"context"

	"github.com/sashabaranov/go-openai"

	"github.com/cago-frame/agents/rag/embedding"
)

const (
	defaultModel     = "text-embedding-3-small"
	defaultDimension = 1536
)

// Embedder 基于 OpenAI API 的 Embedding 实现
type Embedder struct {
	client    *openai.Client
	model     string
	dimension int
}

// Option 函数式选项
type Option func(*Embedder)

// WithModel 设置自定义模型名
func WithModel(model string) Option {
	return func(e *Embedder) {
		e.model = model
	}
}

// WithDimension 设置自定义向量维度
func WithDimension(dim int) Option {
	return func(e *Embedder) {
		e.dimension = dim
	}
}

// NewEmbedder 创建 OpenAI Embedding 实例
func NewEmbedder(config openai.ClientConfig, opts ...Option) embedding.Embedder {
	e := &Embedder{
		client:    openai.NewClientWithConfig(config),
		model:     defaultModel,
		dimension: defaultDimension,
	}
	for _, opt := range opts {
		opt(e)
	}
	return e
}

// Embed 对一批文本生成向量
func (e *Embedder) Embed(ctx context.Context, req *embedding.EmbeddingRequest) (*embedding.EmbeddingResponse, error) {
	model := e.model

	oaiReq := openai.EmbeddingRequest{
		Input:      req.Texts,
		Model:      openai.EmbeddingModel(model),
		Dimensions: e.dimension,
	}

	resp, err := e.client.CreateEmbeddings(ctx, oaiReq)
	if err != nil {
		return nil, err
	}

	results := make([]embedding.EmbeddingResult, len(resp.Data))
	for i, item := range resp.Data {
		results[i] = embedding.EmbeddingResult{
			Vector:     item.Embedding,
			TokenCount: resp.Usage.PromptTokens,
		}
	}

	return &embedding.EmbeddingResponse{
		Results:     results,
		Model:       string(resp.Model),
		Dimension:   len(resp.Data[0].Embedding),
		TotalTokens: resp.Usage.TotalTokens,
	}, nil
}

// Dimension 返回向量维度
func (e *Embedder) Dimension() int {
	return e.dimension
}

// ModelName 返回使用的模型名称
func (e *Embedder) ModelName() string {
	return e.model
}
