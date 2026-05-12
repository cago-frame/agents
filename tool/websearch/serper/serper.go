// Package serper 把 Serper.dev (https://serper.dev) 包成 websearch.Provider。
package serper

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/cago-frame/agents/tool/websearch"
)

// DefaultEndpoint 是 Serper 的默认搜索接口。
const DefaultEndpoint = "https://google.serper.dev/search"

// Option 构造选项。
type Option func(*provider)

type provider struct {
	apiKey   string
	client   *http.Client
	endpoint string
	country  string
	locale   string
}

// Client 注入自定义 *http.Client。
func Client(c *http.Client) Option { return func(p *provider) { p.client = c } }

// Endpoint 覆盖默认 endpoint(测试 mock 用)。
func Endpoint(url string) Option { return func(p *provider) { p.endpoint = url } }

// Country gl 参数(如 "us" / "cn")。
func Country(s string) Option { return func(p *provider) { p.country = s } }

// Locale hl 参数(如 "en" / "zh-cn")。
func Locale(s string) Option { return func(p *provider) { p.locale = s } }

// New 构造 Serper provider。
func New(apiKey string, opts ...Option) websearch.Provider {
	p := &provider{
		apiKey:   apiKey,
		client:   http.DefaultClient,
		endpoint: DefaultEndpoint,
	}
	for _, o := range opts {
		o(p)
	}
	return p
}

type request struct {
	Q   string `json:"q"`
	Num int    `json:"num,omitempty"`
	GL  string `json:"gl,omitempty"`
	HL  string `json:"hl,omitempty"`
}

type response struct {
	Organic []struct {
		Link    string `json:"link"`
		Title   string `json:"title"`
		Snippet string `json:"snippet"`
		Date    string `json:"date"`
	} `json:"organic"`
}

func (p *provider) Search(ctx context.Context, query string, maxResults int) ([]websearch.Result, error) {
	body, err := json.Marshal(request{Q: query, Num: maxResults, GL: p.country, HL: p.locale})
	if err != nil {
		return nil, fmt.Errorf("serper: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("serper: %w", err)
	}
	req.Header.Set("X-API-KEY", p.apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("serper: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("serper: %d %s", resp.StatusCode, truncateRespBody(respBody))
	}

	var parsed response
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return nil, fmt.Errorf("serper: parse: %w", err)
	}
	out := make([]websearch.Result, 0, len(parsed.Organic))
	for _, o := range parsed.Organic {
		out = append(out, websearch.Result{
			URL:     o.Link,
			Title:   o.Title,
			Snippet: o.Snippet,
			PageAge: o.Date,
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
