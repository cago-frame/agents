package embedding

import "context"

// EmbeddingResult 单个文本的向量结果
type EmbeddingResult struct {
	Vector     []float32
	TokenCount int
}

// EmbeddingRequest 请求参数
type EmbeddingRequest struct {
	Texts []string // 需要向量化的文本列表
}

// EmbeddingResponse 批量向量化的响应
type EmbeddingResponse struct {
	Results     []EmbeddingResult
	Model       string
	Dimension   int
	TotalTokens int
}

// Embedder 通用 Embedding 接口
type Embedder interface {
	// Embed 对一批文本生成向量
	Embed(ctx context.Context, req *EmbeddingRequest) (*EmbeddingResponse, error)
	// Dimension 返回向量维度
	Dimension() int
	// ModelName 返回使用的模型名称
	ModelName() string
}
