// Package find 暴露一个最小的"按 glob 找文件"工具，行为对齐 pi-mono find.ts：
//   - 支持 *、**、?、[abc] 等 doublestar 风格
//   - 模式带 / 时按完整相对路径匹配（必要时自动 prepend ** /）；否则按 basename
//   - 默认跳过 .git / node_modules（轻量替代 fd 的 .gitignore；可关掉）
package find

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	agent "github.com/cago-frame/agents/agent"
	"github.com/cago-frame/agents/tool"
	"github.com/cago-frame/agents/tool/internal/pathutil"
	"github.com/cago-frame/agents/tool/internal/truncate"
)

// Name 是工具暴露给 LLM 的名字。
const Name = "find"

// Description 与 pi-mono 对齐。
const Description = "Search for files by glob pattern. Returns matching file paths relative to the search directory. Output is truncated to 1000 results or 50KB (whichever is hit first)."

const defaultLimit = 1000

// DefaultIgnoredDirs 是默认跳过的目录名（按 fd 默认行为粗略对齐）。
var DefaultIgnoredDirs = []string{".git", "node_modules", ".svn", ".hg"}

// Option 可选构造项。
type Option func(*config)

type config struct {
	cwd          string
	defaultLimit int
	maxBytes     int
	ignoredDirs  []string
	includeAll   bool
	serial       bool
}

// Cwd 指定相对路径解析的根。
func Cwd(p string) Option { return func(c *config) { c.cwd = p } }

// DefaultLimit 覆盖默认结果上限（pi-mono 默认 1000）。
func DefaultLimit(n int) Option { return func(c *config) { c.defaultLimit = n } }

// MaxBytes 覆盖输出字节上限。
func MaxBytes(n int) Option { return func(c *config) { c.maxBytes = n } }

// IgnoreDirs 覆盖默认跳过目录列表。
func IgnoreDirs(names ...string) Option { return func(c *config) { c.ignoredDirs = names } }

// IncludeAll 关闭默认跳过逻辑，搜全部目录（包括 .git）。
func IncludeAll() Option { return func(c *config) { c.includeAll = true } }

// Serial 让该工具与同一轮的其他工具串行执行。
func Serial() Option { return func(c *config) { c.serial = true } }

// New 构造 find 工具。
func New(opts ...Option) tool.Tool {
	cfg := &config{}
	for _, o := range opts {
		o(cfg)
	}
	if cfg.defaultLimit <= 0 {
		cfg.defaultLimit = defaultLimit
	}
	if cfg.ignoredDirs == nil {
		cfg.ignoredDirs = DefaultIgnoredDirs
	}
	return &tool.RawTool{
		NameStr:   Name,
		DescStr:   Description,
		SchemaVal: schema(),
		IsSerial:  cfg.serial,
		Handler: func(ctx context.Context, in map[string]any) (*agent.ToolResultBlock, error) {
			return run(ctx, cfg, in)
		},
		PromptSnippetStr: "Find files by name/glob; respects .gitignore.",
		PromptGuidelinesArr: []string{
			"Prefer find over bash find for file discovery.",
		},
	}
}

func schema() agent.Schema {
	return agent.Schema{
		Type:     "object",
		Required: []string{"pattern"},
		Properties: map[string]*agent.Property{
			"pattern": {Type: "string", Description: "Glob pattern to match files, e.g. '*.go', '**/*.json', or 'src/**/*_test.go'"},
			"path":    {Type: "string", Description: "Directory to search in (default: current directory)"},
			"limit":   {Type: "integer", Description: "Maximum number of results (default: 1000)"},
		},
	}
}

func run(ctx context.Context, cfg *config, in map[string]any) (*agent.ToolResultBlock, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	pattern, _ := in["pattern"].(string)
	if pattern == "" {
		return tool.ErrorResult("find: pattern must not be empty"), nil
	}

	pth, _ := in["path"].(string)
	if pth == "" {
		pth = "."
	}

	limit := intOr(in["limit"], 0)
	if limit <= 0 {
		limit = cfg.defaultLimit
	}

	root := pathutil.ResolveToCwd(pth, cfg.cwd)
	st, err := os.Stat(root)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return tool.ErrorResult(fmt.Sprintf("Path not found: %s", root)), nil
		}
		return nil, fmt.Errorf("find: %w", err)
	}
	if !st.IsDir() {
		return tool.ErrorResult(fmt.Sprintf("Not a directory: %s", root)), nil
	}

	normalizedPattern := normalizePattern(pattern)
	matchByBase := !strings.Contains(pattern, "/")

	ignored := make(map[string]struct{}, len(cfg.ignoredDirs))
	if !cfg.includeAll {
		for _, d := range cfg.ignoredDirs {
			ignored[d] = struct{}{}
		}
	}

	results := make([]string, 0, 64)
	limitReached := false
	walkErr := filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return nil //nolint:nilerr // skip unreadable entries silently like fd does
		}
		if d.IsDir() {
			if p == root {
				return nil
			}
			if _, skip := ignored[d.Name()]; skip {
				return filepath.SkipDir
			}
			return nil
		}
		rel, rerr := filepath.Rel(root, p)
		if rerr != nil {
			return nil //nolint:nilerr // skip entries we can't relativize
		}
		rel = filepath.ToSlash(rel)
		var ok bool
		if matchByBase {
			ok, _ = filepath.Match(pattern, filepath.Base(p))
		} else {
			ok = matchGlob(normalizedPattern, rel)
		}
		if ok {
			results = append(results, rel)
			if len(results) >= limit {
				limitReached = true
				return errors.New("__limit_reached__")
			}
		}
		return nil
	})
	if walkErr != nil && walkErr.Error() != "__limit_reached__" {
		return nil, fmt.Errorf("find: %w", walkErr)
	}

	if len(results) == 0 {
		return tool.TextResult("No files found matching pattern"), nil
	}
	sort.Strings(results)
	raw := strings.Join(results, "\n")
	tr := truncate.Head(raw, truncate.Options{
		MaxLines: 1 << 30,
		MaxBytes: cfg.maxBytes,
	})

	notices := []string{}
	if limitReached {
		notices = append(notices,
			fmt.Sprintf("%d results limit reached. Use limit=%d for more, or refine pattern", limit, limit*2))
	}
	if tr.Truncated {
		mb := cfg.maxBytes
		if mb <= 0 {
			mb = truncate.DefaultMaxBytes
		}
		notices = append(notices, fmt.Sprintf("%s limit reached", truncate.FormatSize(mb)))
	}
	body := tr.Content
	if len(notices) > 0 {
		body = body + "\n\n[" + strings.Join(notices, ". ") + "]"
	}
	return tool.TextResult(body), nil
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

// normalizePattern 复刻 pi-mono 的 "pattern 含 / 时自动 prepend **/" 规则。
func normalizePattern(p string) string {
	if !strings.Contains(p, "/") {
		return p
	}
	if strings.HasPrefix(p, "/") || strings.HasPrefix(p, "**/") || p == "**" {
		return strings.TrimPrefix(p, "/")
	}
	return "**/" + p
}

// matchGlob 是 doublestar 风格匹配。pattern 与 path 都按 / 切分；
// "**" 段匹配 0 或多个路径组件，其它段交给 filepath.Match。
func matchGlob(pattern, path string) bool {
	pat := strings.Split(pattern, "/")
	parts := strings.Split(path, "/")
	return matchSegments(pat, parts)
}

func matchSegments(pat, parts []string) bool {
	for len(pat) > 0 && len(parts) > 0 {
		if pat[0] == "**" {
			rest := pat[1:]
			if len(rest) == 0 {
				return true
			}
			for i := 0; i <= len(parts); i++ {
				if matchSegments(rest, parts[i:]) {
					return true
				}
			}
			return false
		}
		ok, err := filepath.Match(pat[0], parts[0])
		if err != nil || !ok {
			return false
		}
		pat = pat[1:]
		parts = parts[1:]
	}
	if len(pat) == 0 && len(parts) == 0 {
		return true
	}
	if len(parts) == 0 {
		for _, p := range pat {
			if p != "**" {
				return false
			}
		}
		return true
	}
	return false
}
