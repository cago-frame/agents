// Package tavily 把 Tavily AI Search (https://tavily.com) 包成 websearch.Provider。
package tavily

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/cago-frame/agents/tool/websearch"
)

// DefaultEndpoint 是 Tavily 的默认搜索接口。
const DefaultEndpoint = "https://api.tavily.com/search"

// Option 构造选项。
type Option func(*provider)

type provider struct {
	apiKey        string
	client        *http.Client
	endpoint      string
	searchDepth   string
	includeAnswer bool
}

// Client 注入自定义 *http.Client。
func Client(c *http.Client) Option { return func(p *provider) { p.client = c } }

// Endpoint 覆盖默认 endpoint(测试 mock 用)。
func Endpoint(url string) Option { return func(p *provider) { p.endpoint = url } }

// SearchDepth 设置 Tavily 的 search_depth("basic" 或 "advanced")。默认 basic。
func SearchDepth(d string) Option { return func(p *provider) { p.searchDepth = d } }

// IncludeAnswer 让 Tavily 返回总结答案(默认 false,避免污染 snippet)。
func IncludeAnswer(b bool) Option { return func(p *provider) { p.includeAnswer = b } }

// New 构造 Tavily provider。
func New(apiKey string, opts ...Option) websearch.Provider {
	p := &provider{
		apiKey:      apiKey,
		client:      http.DefaultClient,
		endpoint:    DefaultEndpoint,
		searchDepth: "basic",
	}
	for _, o := range opts {
		o(p)
	}
	return p
}

// request 是 Tavily 的 search 入参。api_key 是它要求的 wire 字段名(Tavily 把 key 放 body 而非 header),不是泄漏的密钥。
type request struct {
	APIKey        string `json:"api_key"`
	Query         string `json:"query"`
	MaxResults    int    `json:"max_results,omitempty"`
	SearchDepth   string `json:"search_depth,omitempty"`
	IncludeAnswer bool   `json:"include_answer,omitempty"`
}

type response struct {
	Results []struct {
		URL           string `json:"url"`
		Title         string `json:"title"`
		Content       string `json:"content"`
		PublishedDate string `json:"published_date"`
	} `json:"results"`
}

func (p *provider) Search(ctx context.Context, query string, maxResults int) ([]websearch.Result, error) {
	body, err := json.Marshal(request{ //nolint:gosec // G117: api_key is Tavily's required body field, not a hardcoded credential
		APIKey:        p.apiKey,
		Query:         query,
		MaxResults:    maxResults,
		SearchDepth:   p.searchDepth,
		IncludeAnswer: p.includeAnswer,
	})
	if err != nil {
		return nil, fmt.Errorf("tavily: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("tavily: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("tavily: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("tavily: %d %s", resp.StatusCode, truncateRespBody(respBody))
	}

	var parsed response
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return nil, fmt.Errorf("tavily: parse: %w", err)
	}
	out := make([]websearch.Result, 0, len(parsed.Results))
	for _, r := range parsed.Results {
		out = append(out, websearch.Result{
			URL:     r.URL,
			Title:   r.Title,
			Snippet: r.Content,
			PageAge: r.PublishedDate,
		})
	}
	return out, nil
}

func truncateRespBody(b []byte) string {
	const limit = 200
	if len(b) > limit {
		return string(b[:limit]) + "..."
	}
	return string(b)
}
