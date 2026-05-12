package openai

import (
	"testing"

	"github.com/sashabaranov/go-openai"

	"github.com/cago-frame/agents/rag/embedding"
)

func TestNewEmbedder_Defaults(t *testing.T) {
	config := openai.DefaultConfig("test-key")
	e := NewEmbedder(config)

	if e.ModelName() != defaultModel {
		t.Errorf("expected model %q, got %q", defaultModel, e.ModelName())
	}
	if e.Dimension() != defaultDimension {
		t.Errorf("expected dimension %d, got %d", defaultDimension, e.Dimension())
	}
}

func TestNewEmbedder_WithModel(t *testing.T) {
	config := openai.DefaultConfig("test-key")
	customModel := "text-embedding-3-large"
	e := NewEmbedder(config, WithModel(customModel))

	if e.ModelName() != customModel {
		t.Errorf("expected model %q, got %q", customModel, e.ModelName())
	}
}

func TestNewEmbedder_WithDimension(t *testing.T) {
	config := openai.DefaultConfig("test-key")
	customDim := 3072
	e := NewEmbedder(config, WithDimension(customDim))

	if e.Dimension() != customDim {
		t.Errorf("expected dimension %d, got %d", customDim, e.Dimension())
	}
}

func TestNewEmbedder_WithAllOptions(t *testing.T) {
	config := openai.DefaultConfig("test-key")
	customModel := "text-embedding-3-large"
	customDim := 3072
	e := NewEmbedder(config, WithModel(customModel), WithDimension(customDim))

	if e.ModelName() != customModel {
		t.Errorf("expected model %q, got %q", customModel, e.ModelName())
	}
	if e.Dimension() != customDim {
		t.Errorf("expected dimension %d, got %d", customDim, e.Dimension())
	}
}

func TestNewEmbedder_ImplementsInterface(t *testing.T) {
	config := openai.DefaultConfig("test-key")
	e := NewEmbedder(config)

	// 编译时检查接口实现
	var _ = embedding.Embedder(e)
}
