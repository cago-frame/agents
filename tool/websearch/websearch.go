// Package websearch 把"搜索"包装成一个 agent.Tool。本包只定义工具壳 + Provider
// 接口;具体的搜索后端(Serper / Tavily / Brave / 其他)在子包里实现。
//
// 用法:
//
//	import (
//	    "github.com/cago-frame/agents/tool/websearch"
//	    "github.com/cago-frame/agents/tool/websearch/serper"
//	)
//
//	tl := websearch.New(websearch.WithProvider(serper.New(os.Getenv("SERPER_API_KEY"))))
package websearch

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	agent "github.com/cago-frame/agents/agent"
	"github.com/cago-frame/agents/tool"
)

// Name 是工具暴露给 LLM 的名字。
const Name = "web_search"

// Description 暴露给 LLM 的工具描述。
const Description = "Search the web and return a list of results (title, url, snippet). Use this for finding pages to fetch with web_fetch, or for quick fact lookups."

// Result 一条搜索结果。
type Result struct {
	URL     string
	Title   string
	Snippet string
	PageAge string // 可选,例如 "3 months" / "2024-01-15"
}

// Provider 搜索后端接口。各家适配器(serper / tavily / brave)实现这个。
type Provider interface {
	Search(ctx context.Context, query string, maxResults int) ([]Result, error)
}

// Option 构造选项。
type Option func(*config)

type config struct {
	prov              Provider
	defaultMaxResults int
	maxResults        int
	allowed           []string
	blocked           []string
	serial            bool
}

// WithProvider 注入搜索后端。**必需**;零值时 Call 报 "no provider configured"。
// With 前缀避免和包级 Provider 类型同名。
func WithProvider(p Provider) Option { return func(c *config) { c.prov = p } }

// DefaultMaxResults 模型未传 max_results 时用,默认 5。
func DefaultMaxResults(n int) Option { return func(c *config) { c.defaultMaxResults = n } }

// MaxResults 上限 cap,默认 20;模型传超过时自动 clamp。
func MaxResults(n int) Option { return func(c *config) { c.maxResults = n } }

// AllowedDomains 后置过滤白名单(provider 返回后)。
func AllowedDomains(ds ...string) Option {
	return func(c *config) { c.allowed = append(c.allowed, ds...) }
}

// BlockedDomains 后置过滤黑名单。
func BlockedDomains(ds ...string) Option {
	return func(c *config) { c.blocked = append(c.blocked, ds...) }
}

// Serial 让该工具与同一轮的其他工具串行执行。
func Serial() Option { return func(c *config) { c.serial = true } }

// New 构造 web_search 工具。
func New(opts ...Option) tool.Tool {
	cfg := &config{
		defaultMaxResults: 5,
		maxResults:        20,
	}
	for _, o := range opts {
		o(cfg)
	}
	return &tool.RawTool{
		NameStr:   Name,
		DescStr:   Description,
		SchemaVal: schema(),
		IsSerial:  cfg.serial,
		Handler: func(ctx context.Context, in map[string]any) (*agent.ToolResultBlock, error) {
			return run(ctx, cfg, in)
		},
		PromptSnippetStr: "Search the web via the configured search provider.",
		PromptGuidelinesArr: []string{
			"Use websearch for facts you cannot derive from local code.",
		},
	}
}

func schema() agent.Schema {
	return agent.Schema{
		Type:     "object",
		Required: []string{"query"},
		Properties: map[string]*agent.Property{
			"query":       {Type: "string", Description: "Search query"},
			"max_results": {Type: "integer", Description: "Optional cap on results (default 5, max 20)"},
		},
	}
}

func run(ctx context.Context, cfg *config, in map[string]any) (*agent.ToolResultBlock, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if cfg.prov == nil {
		return tool.ErrorResult("websearch: no provider configured (use websearch.WithProvider)"), nil
	}

	query, _ := in["query"].(string)
	q := strings.TrimSpace(query)
	if q == "" {
		return tool.ErrorResult("websearch: query must not be empty"), nil
	}

	n := intOr(in["max_results"], 0)
	if n <= 0 {
		n = cfg.defaultMaxResults
	}
	if n > cfg.maxResults {
		n = cfg.maxResults
	}
	if n < 1 {
		n = 1
	}

	results, err := cfg.prov.Search(ctx, q, n)
	if err != nil {
		return nil, fmt.Errorf("websearch: %w", err)
	}

	results = filterByDomain(results, cfg.allowed, cfg.blocked)
	if len(results) > n {
		results = results[:n]
	}
	if len(results) == 0 {
		return tool.TextResult("[no results]"), nil
	}
	return tool.TextResult(formatResults(results)), nil
}

// intOr extracts an integer from a map[string]any value. JSON numbers are float64;
// this helper handles float64, int, int64, and nil.
func intOr(v any, def int) int {
	switch n := v.(type) {
	case nil:
		return def
	case float64:
		return int(n)
	case int:
		return n
	case int64:
		return int(n)
	default:
		return def
	}
}

func formatResults(rs []Result) string {
	var b strings.Builder
	for i, r := range rs {
		fmt.Fprintf(&b, "%d. [%s](%s)", i+1, r.Title, r.URL)
		if r.Snippet != "" {
			fmt.Fprintf(&b, " — %s", r.Snippet)
		}
		if r.PageAge != "" {
			fmt.Fprintf(&b, " (page age: %s)", r.PageAge)
		}
		b.WriteByte('\n')
	}
	return strings.TrimRight(b.String(), "\n")
}

func filterByDomain(rs []Result, allowed, blocked []string) []Result {
	if len(allowed) == 0 && len(blocked) == 0 {
		return rs
	}
	out := rs[:0:0]
	for _, r := range rs {
		host := hostOf(r.URL)
		if host == "" {
			continue
		}
		if anyMatch(host, blocked) {
			continue
		}
		if len(allowed) > 0 && !anyMatch(host, allowed) {
			continue
		}
		out = append(out, r)
	}
	return out
}

func hostOf(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	return strings.ToLower(u.Hostname())
}

func anyMatch(host string, entries []string) bool {
	for _, e := range entries {
		e = strings.ToLower(e)
		if host == e || strings.HasSuffix(host, "."+e) {
			return true
		}
	}
	return false
}

// MockProvider 是测试用的内存 Provider。给单元测试 / 集成测试一个无 IO 的 Provider。
type MockProvider struct {
	Results []Result
	Err     error
}

// Search 实现 Provider 接口。返回固定结果(按 maxResults 截取)。
func (m *MockProvider) Search(_ context.Context, _ string, maxResults int) ([]Result, error) {
	if m.Err != nil {
		return nil, m.Err
	}
	if maxResults <= 0 || maxResults >= len(m.Results) {
		return m.Results, nil
	}
	return m.Results[:maxResults], nil
}
