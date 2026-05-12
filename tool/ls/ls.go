// Package ls 暴露一个最小的"非递归列目录"工具，行为对齐 pi-mono ls.ts。
// 包含 dotfile，目录加 "/" 后缀，按大小写不敏感字典序，stat 失败的条目静默跳过。
package ls

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
const Name = "ls"

// Description 与 pi-mono 对齐。
const Description = "List directory contents. Returns entries sorted alphabetically, with '/' suffix for directories. Includes dotfiles. Output is truncated to 500 entries or 50KB (whichever is hit first)."

const defaultLimit = 500

// Option 可选构造项。
type Option func(*config)

type config struct {
	cwd          string
	defaultLimit int
	maxBytes     int
	serial       bool
}

// Cwd 指定相对路径解析的根。
func Cwd(p string) Option { return func(c *config) { c.cwd = p } }

// DefaultLimit 覆盖默认条目上限（pi-mono 默认 500）。
func DefaultLimit(n int) Option { return func(c *config) { c.defaultLimit = n } }

// MaxBytes 覆盖输出字节上限。
func MaxBytes(n int) Option { return func(c *config) { c.maxBytes = n } }

// Serial 让该工具与同一轮的其他工具串行执行。
func Serial() Option { return func(c *config) { c.serial = true } }

// New 构造 ls 工具。
func New(opts ...Option) tool.Tool {
	cfg := &config{}
	for _, o := range opts {
		o(cfg)
	}
	if cfg.defaultLimit <= 0 {
		cfg.defaultLimit = defaultLimit
	}
	return &tool.RawTool{
		NameStr:   Name,
		DescStr:   Description,
		SchemaVal: schema(),
		IsSerial:  cfg.serial,
		Handler: func(ctx context.Context, in map[string]any) (*agent.ToolResultBlock, error) {
			return run(ctx, cfg, in)
		},
		PromptSnippetStr: "List directory contents.",
		PromptGuidelinesArr: []string{
			"Prefer ls over bash ls.",
		},
	}
}

func schema() agent.Schema {
	return agent.Schema{
		Type: "object",
		Properties: map[string]*agent.Property{
			"path":  {Type: "string", Description: "Directory to list (default: current directory)"},
			"limit": {Type: "integer", Description: "Maximum number of entries to return (default: 500)"},
		},
	}
}

func run(ctx context.Context, cfg *config, in map[string]any) (*agent.ToolResultBlock, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	pth, _ := in["path"].(string)
	if pth == "" {
		pth = "."
	}

	limit := intOr(in["limit"], 0)
	if limit <= 0 {
		limit = cfg.defaultLimit
	}

	abs := pathutil.ResolveToCwd(pth, cfg.cwd)
	st, err := os.Stat(abs)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return tool.ErrorResult(fmt.Sprintf("Path not found: %s", abs)), nil
		}
		return nil, fmt.Errorf("ls: %w", err)
	}
	if !st.IsDir() {
		return tool.ErrorResult(fmt.Sprintf("Not a directory: %s", abs)), nil
	}

	entries, err := os.ReadDir(abs)
	if err != nil {
		return nil, fmt.Errorf("ls: %w", err)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	sort.Slice(names, func(i, j int) bool {
		return strings.ToLower(names[i]) < strings.ToLower(names[j])
	})

	results := make([]string, 0, len(names))
	limitReached := false
	for _, name := range names {
		info, serr := os.Stat(filepath.Join(abs, name))
		if serr != nil {
			continue // broken symlink / no permission → silently skip
		}
		out := name
		if info.IsDir() {
			out += "/"
		}
		results = append(results, out)
		if len(results) >= limit {
			limitReached = true
			break
		}
	}

	if len(results) == 0 {
		return tool.TextResult("(empty directory)"), nil
	}
	raw := strings.Join(results, "\n")
	tr := truncate.Head(raw, truncate.Options{
		MaxLines: 1 << 30,
		MaxBytes: cfg.maxBytes,
	})

	notices := []string{}
	if limitReached {
		notices = append(notices,
			fmt.Sprintf("%d entries limit reached. Use limit=%d for more", limit, limit*2))
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
