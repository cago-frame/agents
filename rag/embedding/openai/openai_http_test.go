package openai

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	openai "github.com/sashabaranov/go-openai"

	"github.com/cago-frame/agents/rag/embedding"
)

func TestEmbed_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var got map[string]any
		_ = json.Unmarshal(body, &got)
		if got["model"] != "text-embedding-3-small" {
			t.Errorf("model = %v", got["model"])
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{
			"object": "list",
			"data": [
				{"object":"embedding","index":0,"embedding":[0.1,0.2,0.3]},
				{"object":"embedding","index":1,"embedding":[0.4,0.5,0.6]}
			],
			"model": "text-embedding-3-small",
			"usage": {"prompt_tokens": 7, "total_tokens": 7}
		}`)
	}))
	defer srv.Close()

	cfg := openai.DefaultConfig("test")
	cfg.BaseURL = srv.URL
	e := NewEmbedder(cfg)

	resp, err := e.Embed(context.Background(), &embedding.EmbeddingRequest{Texts: []string{"a", "b"}})
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}
	if len(resp.Results) != 2 {
		t.Fatalf("results len = %d, want 2", len(resp.Results))
	}
	if got := resp.Results[0].Vector; len(got) != 3 || got[0] != 0.1 {
		t.Errorf("vector[0] = %v", got)
	}
	if resp.Dimension != 3 {
		t.Errorf("Dimension = %d, want 3", resp.Dimension)
	}
	if resp.TotalTokens != 7 {
		t.Errorf("TotalTokens = %d, want 7", resp.TotalTokens)
	}
	if resp.Model != "text-embedding-3-small" {
		t.Errorf("Model = %q", resp.Model)
	}
	if resp.Results[0].TokenCount != 7 {
		t.Errorf("TokenCount = %d, want 7", resp.Results[0].TokenCount)
	}
}

func TestEmbed_HTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = io.WriteString(w, `{"error":{"message":"boom"}}`)
	}))
	defer srv.Close()

	cfg := openai.DefaultConfig("test")
	cfg.BaseURL = srv.URL
	e := NewEmbedder(cfg)

	_, err := e.Embed(context.Background(), &embedding.EmbeddingRequest{Texts: []string{"a"}})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestEmbed_CustomModelAndDimension(t *testing.T) {
	var sawModel string
	var sawDim float64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var got map[string]any
		_ = json.Unmarshal(body, &got)
		sawModel, _ = got["model"].(string)
		sawDim, _ = got["dimensions"].(float64)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"object":"list","data":[{"embedding":[0.1]}],"model":"text-embedding-3-large","usage":{"prompt_tokens":1,"total_tokens":1}}`)
	}))
	defer srv.Close()

	cfg := openai.DefaultConfig("test")
	cfg.BaseURL = srv.URL
	e := NewEmbedder(cfg, WithModel("text-embedding-3-large"), WithDimension(2048))

	if _, err := e.Embed(context.Background(), &embedding.EmbeddingRequest{Texts: []string{"x"}}); err != nil {
		t.Fatalf("Embed: %v", err)
	}
	if sawModel != "text-embedding-3-large" {
		t.Errorf("server saw model %q", sawModel)
	}
	if sawDim != 2048 {
		t.Errorf("server saw dimensions %v, want 2048", sawDim)
	}
}
