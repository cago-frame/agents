// Package brave 把 Brave Search API (https://brave.com/search/api) 包成 websearch.Provider。
package brave

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"

	"github.com/cago-frame/agents/tool/websearch"
)

// DefaultEndpoint 是 Brave Search 的默认搜索接口。
const DefaultEndpoint = "https://api.search.brave.com/res/v1/web/search"

// Option 构造选项。
type Option func(*provider)

type provider struct {
	apiKey     string
	client     *http.Client
	endpoint   string
	country    string
	safeSearch string
}

// Client 注入自定义 *http.Client。
func Client(c *http.Client) Option { return func(p *provider) { p.client = c } }

// Endpoint 覆盖默认 endpoint(测试 mock 用)。
func Endpoint(url string) Option { return func(p *provider) { p.endpoint = url } }

// Country 例如 "US" / "CN"。
func Country(s string) Option { return func(p *provider) { p.country = s } }

// SafeSearch 例如 "off" / "moderate" / "strict"。
func SafeSearch(s string) Option { return func(p *provider) { p.safeSearch = s } }

// New 构造 Brave provider。
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

type response struct {
	Web struct {
		Results []struct {
			URL         string `json:"url"`
			Title       string `json:"title"`
			Description string `json:"description"`
			Age         string `json:"age"`
		} `json:"results"`
	} `json:"web"`
}

func (p *provider) Search(ctx context.Context, query string, maxResults int) ([]websearch.Result, error) {
	q := url.Values{}
	q.Set("q", query)
	if maxResults > 0 {
		q.Set("count", strconv.Itoa(maxResults))
	}
	if p.country != "" {
		q.Set("country", p.country)
	}
	if p.safeSearch != "" {
		q.Set("safesearch", p.safeSearch)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.endpoint+"?"+q.Encode(), nil)
	if err != nil {
		return nil, fmt.Errorf("brave: %w", err)
	}
	req.Header.Set("X-Subscription-Token", p.apiKey)
	req.Header.Set("Accept", "application/json")

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("brave: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("brave: %d %s", resp.StatusCode, truncateRespBody(respBody))
	}

	var parsed response
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return nil, fmt.Errorf("brave: parse: %w", err)
	}
	out := make([]websearch.Result, 0, len(parsed.Web.Results))
	for _, r := range parsed.Web.Results {
		out = append(out, websearch.Result{
			URL:     r.URL,
			Title:   r.Title,
			Snippet: r.Description,
			PageAge: r.Age,
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
