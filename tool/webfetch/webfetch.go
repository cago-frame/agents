// Package webfetch 把 URL fetch + 文本解码 + HTML→Markdown 包装成一个 agent.Tool。
//
// 行为对齐 Claude Code 的 WebFetch:模型给一个 url(可带 prompt),工具拉回页面,
// 按 Content-Type 解包成统一文本(HTML 走 html-to-markdown,JSON 美化,其余 text/*
// 原样),再可选地用 provider.Provider 跑一次 prompt 抽取。
//
// 默认零值不会偷偷打外网:caller 必须在 agent 里显式 agent.Tools(webfetch.New(...))
// 才会拉到这个工具。
package webfetch

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	htmltomarkdown "github.com/JohannesKaufmann/html-to-markdown/v2"
	"golang.org/x/net/html"
	"golang.org/x/net/html/charset"

	agent "github.com/cago-frame/agents/agent"
	"github.com/cago-frame/agents/provider"
	"github.com/cago-frame/agents/tool"
)

// Name 是工具暴露给 LLM 的名字。
const Name = "web_fetch"

// Description 暴露给 LLM 的工具描述。
const Description = "Fetch an http(s) URL and return its contents as text. HTML is converted to Markdown; JSON is pretty-printed; other text content types are returned as-is. Binary types (PDF, images) are rejected. Optional `prompt` argument lets the model ask for specific info from the page."

// Option 构造选项。
type Option func(*config)

type config struct {
	client       *http.Client
	timeout      time.Duration
	maxBytes     int64
	maxRedirects int
	userAgent    string
	allowed      []string
	blocked      []string
	cache        *cache
	cacheTTL     time.Duration
	prov         provider.Provider
	model        string
	serial       bool
}

// Client 注入自定义 *http.Client。零值用默认 client(带 30s 超时)。
func Client(c *http.Client) Option { return func(cfg *config) { cfg.client = c } }

// Timeout 设置默认 client 的超时。仅在没用 Client(...) 时生效。
func Timeout(d time.Duration) Option { return func(cfg *config) { cfg.timeout = d } }

// MaxBytes 限制响应 body 字节数,超过报错(非截断)。零值/负值表示不限制(默认)。
func MaxBytes(n int64) Option { return func(cfg *config) { cfg.maxBytes = n } }

// MaxRedirects 跟随 redirect 上限,默认 5。
func MaxRedirects(n int) Option { return func(cfg *config) { cfg.maxRedirects = n } }

// UserAgent 自定义 User-Agent header,默认 cago-agents/webfetch。
func UserAgent(ua string) Option { return func(cfg *config) { cfg.userAgent = ua } }

// AllowedDomains 白名单。非空时 URL host 必须命中其中之一(精确等于或 .entry 后缀匹配)。
func AllowedDomains(ds ...string) Option {
	return func(cfg *config) { cfg.allowed = append(cfg.allowed, ds...) }
}

// BlockedDomains 黑名单。命中即报错;优先级高于白名单。
func BlockedDomains(ds ...string) Option {
	return func(cfg *config) { cfg.blocked = append(cfg.blocked, ds...) }
}

// Cache 启用进程内缓存(key=url,value=最终文本)。零值/负值表示不缓存。
// Provider 抽取的输出不缓存(只缓存 fetch + 解码后的文本)。
func Cache(ttl time.Duration) Option {
	return func(cfg *config) {
		cfg.cacheTTL = ttl
		if ttl > 0 && cfg.cache == nil {
			cfg.cache = &cache{entries: map[string]cacheEntry{}}
		}
	}
}

// WithProvider 注入 provider.Provider。配置后,prompt 字段非空时用模型对页面内容做一次抽取。
// 命名加 With 前缀避免与 provider.Provider 类型冲突。
func WithProvider(p provider.Provider) Option { return func(cfg *config) { cfg.prov = p } }

// Model 指定 Provider 抽取时用的 model 名。零值时让 provider 自己选默认。
func Model(name string) Option { return func(cfg *config) { cfg.model = name } }

// Serial 让该工具与同一轮的其他工具串行执行。
func Serial() Option { return func(cfg *config) { cfg.serial = true } }

// New 构造 web_fetch 工具。
func New(opts ...Option) tool.Tool {
	cfg := &config{
		timeout:      30 * time.Second,
		maxRedirects: 5,
		userAgent:    "cago-agents/webfetch",
	}
	for _, o := range opts {
		o(cfg)
	}
	if cfg.client == nil {
		cfg.client = &http.Client{
			Timeout: cfg.timeout,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				if len(via) >= cfg.maxRedirects {
					return fmt.Errorf("stopped after %d redirects", cfg.maxRedirects)
				}
				return nil
			},
		}
	}
	return &tool.RawTool{
		NameStr:   Name,
		DescStr:   Description,
		SchemaVal: schema(),
		IsSerial:  cfg.serial,
		Handler: func(ctx context.Context, in map[string]any) (*agent.ToolResultBlock, error) {
			return run(ctx, cfg, in)
		},
		PromptSnippetStr: "Fetch a URL and return its content (optionally summarized).",
		PromptGuidelinesArr: []string{
			"Use webfetch for documentation pages or specific URLs the user references.",
		},
	}
}

func schema() agent.Schema {
	return agent.Schema{
		Type:     "object",
		Required: []string{"url"},
		Properties: map[string]*agent.Property{
			"url":    {Type: "string", Description: "Absolute http(s) URL to fetch"},
			"prompt": {Type: "string", Description: "Optional question to extract or summarize from the page content"},
		},
	}
}

func run(ctx context.Context, cfg *config, in map[string]any) (*agent.ToolResultBlock, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	rawURL, _ := in["url"].(string)
	if rawURL == "" {
		return tool.ErrorResult("webfetch: url must not be empty"), nil
	}

	u, err := url.Parse(rawURL)
	if err != nil {
		return tool.ErrorResult(fmt.Sprintf("webfetch: invalid url: %v", err)), nil
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return tool.ErrorResult(fmt.Sprintf("webfetch: unsupported scheme %q (only http/https)", u.Scheme)), nil
	}
	if err := checkDomain(u.Hostname(), cfg.allowed, cfg.blocked); err != nil {
		return tool.ErrorResult(err.Error()), nil
	}

	text, fetchErr := fetchAndDecode(ctx, cfg, rawURL)
	if fetchErr != nil {
		// HTTP-level errors (4xx/5xx, binary content-type, MaxBytes) → ErrorResult.
		// Network transport errors → Go error.
		var netErr *networkError
		if errors.As(fetchErr, &netErr) {
			return nil, netErr.cause
		}
		return tool.ErrorResult(fetchErr.Error()), nil
	}

	prompt, _ := in["prompt"].(string)
	if cfg.prov != nil && strings.TrimSpace(prompt) != "" {
		extracted, provErr := extractWithProvider(ctx, cfg, prompt, text)
		if provErr != nil {
			return nil, provErr
		}
		return tool.TextResult(extracted), nil
	}
	return tool.TextResult(text), nil
}

// networkError wraps a genuine transport-layer error that should propagate as
// a Go error (not as an LLM-visible tool error).
type networkError struct {
	cause error
}

func (e *networkError) Error() string { return e.cause.Error() }

// fetchAndDecode 拉 URL,按 Content-Type 解码成统一文本。带可选 cache。
func fetchAndDecode(ctx context.Context, cfg *config, rawURL string) (string, error) {
	if cfg.cache != nil {
		if v, ok := cfg.cache.get(rawURL, cfg.cacheTTL); ok {
			return v, nil
		}
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return "", &networkError{cause: fmt.Errorf("webfetch: %w", err)}
	}
	req.Header.Set("User-Agent", cfg.userAgent)

	resp, err := cfg.client.Do(req)
	if err != nil {
		return "", &networkError{cause: fmt.Errorf("webfetch: %w", err)}
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= 400 {
		// HTTP error codes are LLM-visible tool errors, not Go errors.
		return "", fmt.Errorf("webfetch: http %d %s", resp.StatusCode, http.StatusText(resp.StatusCode))
	}

	var reader io.Reader = resp.Body
	if cfg.maxBytes > 0 {
		reader = &limitedReader{R: resp.Body, N: cfg.maxBytes}
	}
	body, err := io.ReadAll(reader)
	if err != nil {
		if errors.Is(err, errBodyTooLarge) {
			return "", fmt.Errorf("webfetch: response body exceeds MaxBytes (%d)", cfg.maxBytes)
		}
		return "", &networkError{cause: fmt.Errorf("webfetch: read body: %w", err)}
	}

	ct := resp.Header.Get("Content-Type")
	text, err := decodeByContentType(body, ct)
	if err != nil {
		// Unsupported content-type → LLM-visible error.
		return "", err
	}

	if cfg.cache != nil {
		cfg.cache.put(rawURL, text)
	}
	return text, nil
}

// decodeByContentType 按 Content-Type 大类把 body 转成统一文本。
// HTML 走 html-to-markdown,JSON 美化,XML / text/* 原样,二进制拒绝。
func decodeByContentType(body []byte, contentType string) (string, error) {
	mime := strings.ToLower(strings.TrimSpace(strings.SplitN(contentType, ";", 2)[0]))
	switch {
	case isHTML(mime, body):
		return htmlToText(body, contentType), nil
	case isJSON(mime):
		var buf bytes.Buffer
		if err := json.Indent(&buf, body, "", "  "); err == nil {
			return buf.String(), nil
		}
		return string(body), nil
	case isXML(mime), strings.HasPrefix(mime, "text/"), mime == "":
		// text/* 系列 + 没 Content-Type 但内容像 plain → 原样返回
		return string(body), nil
	default:
		return "", fmt.Errorf("webfetch: unsupported content-type: %s (binary)", mime)
	}
}

func isHTML(mime string, body []byte) bool {
	if mime == "text/html" || mime == "application/xhtml+xml" {
		return true
	}
	// 没 Content-Type 时按首字符嗅探
	if mime == "" {
		t := bytes.TrimLeft(body, " \t\r\n")
		if bytes.HasPrefix(t, []byte("<")) {
			return true
		}
	}
	return false
}

func isJSON(mime string) bool {
	if mime == "application/json" || mime == "application/ld+json" {
		return true
	}
	return strings.HasSuffix(mime, "+json")
}

func isXML(mime string) bool {
	if mime == "application/xml" || mime == "text/xml" {
		return true
	}
	return strings.HasSuffix(mime, "+xml")
}

// htmlToText 三层兜底:html-to-markdown → x/net/html token strip → raw bytes。
// 绝不返回错误 — fetch 网络成功的话,宁可返回脏文本也不让整个 tool call 失败。
func htmlToText(body []byte, contentType string) string {
	// step 1: 现成库 html-to-markdown,自带 lenient HTML5 parser
	md, err := func() (md string, err error) {
		defer func() {
			if r := recover(); r != nil {
				err = fmt.Errorf("html-to-markdown panic: %v", r)
			}
		}()
		// 用 charset.NewReader 把非 UTF-8 charset 转成 UTF-8 再喂给库
		r, rerr := charset.NewReader(bytes.NewReader(body), contentType)
		if rerr != nil {
			r = bytes.NewReader(body) // charset 嗅探失败 → 按 UTF-8 处理
		}
		out, cerr := htmltomarkdown.ConvertReader(r)
		if cerr != nil {
			return "", cerr
		}
		return string(out), nil
	}()
	if err == nil && strings.TrimSpace(md) != "" {
		return md
	}

	// step 2: x/net/html tokenizer 剥标签
	if stripped, sErr := stripTags(body, contentType); sErr == nil && strings.TrimSpace(stripped) != "" {
		return stripped
	}

	// step 3: raw bytes + 前缀提示
	prefix := "[webfetch: html parse failed; returning raw body]\n"
	if err != nil {
		prefix = fmt.Sprintf("[webfetch: html parse failed: %v; returning raw body]\n", err)
	}
	return prefix + string(body)
}

// stripTags 是 htmltomarkdown 失败时的兜底:遍历 tokenizer 只留 TextToken,
// 跳过 script/style/nav/aside/footer 块,html.UnescapeString 后折叠空白。
func stripTags(body []byte, contentType string) (_ string, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("strip-tags panic: %v", r)
		}
	}()
	r, cErr := charset.NewReader(bytes.NewReader(body), contentType)
	if cErr != nil {
		r = bytes.NewReader(body)
	}
	z := html.NewTokenizer(r)
	var out strings.Builder
	skipDepth := 0
	for {
		tt := z.Next()
		switch tt {
		case html.ErrorToken:
			if errors.Is(z.Err(), io.EOF) {
				return collapseSpaces(out.String()), nil
			}
			return collapseSpaces(out.String()), nil
		case html.StartTagToken:
			name, _ := z.TagName()
			if isSkippable(string(name)) {
				skipDepth++
			}
		case html.EndTagToken:
			name, _ := z.TagName()
			if isSkippable(string(name)) && skipDepth > 0 {
				skipDepth--
			}
		case html.TextToken:
			if skipDepth == 0 {
				out.Write(z.Text())
				out.WriteByte(' ')
			}
		}
	}
}

func isSkippable(tag string) bool {
	switch strings.ToLower(tag) {
	case "script", "style", "nav", "aside", "footer", "noscript":
		return true
	}
	return false
}

func collapseSpaces(s string) string {
	s = html.UnescapeString(s)
	var b strings.Builder
	prevSpace := false
	for _, r := range s {
		if r == ' ' || r == '\t' || r == '\r' {
			if !prevSpace {
				b.WriteByte(' ')
				prevSpace = true
			}
			continue
		}
		if r == '\n' {
			b.WriteByte('\n')
			prevSpace = true
			continue
		}
		b.WriteRune(r)
		prevSpace = false
	}
	return strings.TrimSpace(b.String())
}

// extractWithProvider 用 provider.Provider 跑一次 ChatCompletion,把模型答案当工具输出。
func extractWithProvider(ctx context.Context, cfg *config, prompt, content string) (string, error) {
	req := &provider.CompletionRequest{
		Model: cfg.model,
		Messages: []provider.Message{
			{Role: provider.RoleSystem, Content: "Extract info per the user prompt from the content below. Be concise and quote relevant snippets."},
			{Role: provider.RoleUser, Content: "Prompt: " + prompt + "\n\nContent:\n" + content},
		},
	}
	resp, err := cfg.prov.ChatCompletion(ctx, req)
	if err != nil {
		return "", fmt.Errorf("webfetch: provider extraction failed: %w", err)
	}
	return resp.Content, nil
}

// checkDomain 校验白/黑名单。entry 命中规则:host 等于 entry,或 host 以 .entry 结尾。
func checkDomain(host string, allowed, blocked []string) error {
	host = strings.ToLower(host)
	for _, b := range blocked {
		if matchHost(host, strings.ToLower(b)) {
			return fmt.Errorf("webfetch: domain blocked: %s", host)
		}
	}
	if len(allowed) == 0 {
		return nil
	}
	for _, a := range allowed {
		if matchHost(host, strings.ToLower(a)) {
			return nil
		}
	}
	return fmt.Errorf("webfetch: domain not in allowlist: %s", host)
}

func matchHost(host, entry string) bool {
	if host == entry {
		return true
	}
	return strings.HasSuffix(host, "."+entry)
}

// limitedReader 类似 io.LimitReader 但超额时返回 errBodyTooLarge 而不是 silent EOF。
type limitedReader struct {
	R io.Reader
	N int64
}

var errBodyTooLarge = errors.New("body too large")

func (l *limitedReader) Read(p []byte) (int, error) {
	if l.N <= 0 {
		return 0, errBodyTooLarge
	}
	if int64(len(p)) > l.N {
		p = p[:l.N]
	}
	n, err := l.R.Read(p)
	l.N -= int64(n)
	if l.N <= 0 && err == nil {
		// 再 peek 一字节看是否还有内容
		var peek [1]byte
		m, _ := l.R.Read(peek[:])
		if m > 0 {
			return n, errBodyTooLarge
		}
	}
	return n, err
}

// cache 是进程内 url→text 缓存,带 TTL。
type cache struct {
	mu      sync.Mutex
	entries map[string]cacheEntry
}

type cacheEntry struct {
	text string
	at   time.Time
}

func (c *cache) get(key string, ttl time.Duration) (string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.entries[key]
	if !ok {
		return "", false
	}
	if time.Since(e.at) > ttl {
		delete(c.entries, key)
		return "", false
	}
	return e.text, true
}

func (c *cache) put(key, text string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries[key] = cacheEntry{text: text, at: time.Now()}
}
