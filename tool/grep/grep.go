// Package grep 暴露一个最小的"按正则搜文件内容"工具，行为对齐 pi-mono grep.ts：
//   - 输出格式 "relpath:N: text"（context 行用 "relpath-N- text"）
//   - 默认 100 matches、50KB、单行 500 字符
//   - glob 过滤待搜文件路径
//   - 默认跳过 .git / node_modules（轻量替代 rg 的 .gitignore 行为）
package grep

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	agent "github.com/cago-frame/agents/agent"
	"github.com/cago-frame/agents/tool"
	"github.com/cago-frame/agents/tool/internal/pathutil"
	"github.com/cago-frame/agents/tool/internal/truncate"
)

// Name 是工具暴露给 LLM 的名字。
const Name = "grep"

// Description 与 pi-mono 对齐。
const Description = "Search file contents for a pattern. Returns matching lines with file paths and line numbers. Output is truncated to 100 matches or 50KB (whichever is hit first). Long lines are truncated to 500 chars."

const defaultLimit = 100

// DefaultIgnoredDirs 是默认跳过的目录名。
var DefaultIgnoredDirs = []string{".git", "node_modules", ".svn", ".hg"}

// Option 可选构造项。
type Option func(*config)

type config struct {
	cwd          string
	defaultLimit int
	maxBytes     int
	maxLineChars int
	ignoredDirs  []string
	includeAll   bool
	serial       bool
}

// Cwd 指定相对路径解析的根。
func Cwd(p string) Option { return func(c *config) { c.cwd = p } }

// DefaultLimit 覆盖默认匹配上限（pi-mono 默认 100）。
func DefaultLimit(n int) Option { return func(c *config) { c.defaultLimit = n } }

// MaxBytes 覆盖输出字节上限。
func MaxBytes(n int) Option { return func(c *config) { c.maxBytes = n } }

// MaxLineChars 覆盖单行字符上限（pi-mono 默认 500）。
func MaxLineChars(n int) Option { return func(c *config) { c.maxLineChars = n } }

// IgnoreDirs 覆盖默认跳过目录列表。
func IgnoreDirs(names ...string) Option { return func(c *config) { c.ignoredDirs = names } }

// IncludeAll 关闭默认跳过逻辑，搜全部目录。
func IncludeAll() Option { return func(c *config) { c.includeAll = true } }

// Serial 让该工具与同一轮的其他工具串行执行。
func Serial() Option { return func(c *config) { c.serial = true } }

// New 构造 grep 工具。
func New(opts ...Option) tool.Tool {
	cfg := &config{}
	for _, o := range opts {
		o(cfg)
	}
	if cfg.defaultLimit <= 0 {
		cfg.defaultLimit = defaultLimit
	}
	if cfg.maxLineChars <= 0 {
		cfg.maxLineChars = truncate.GrepMaxLineLength
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
		PromptSnippetStr: "Search file contents with ripgrep semantics; respects .gitignore.",
		PromptGuidelinesArr: []string{
			"Prefer grep over bash rg for code search.",
		},
	}
}

func schema() agent.Schema {
	return agent.Schema{
		Type:     "object",
		Required: []string{"pattern"},
		Properties: map[string]*agent.Property{
			"pattern":    {Type: "string", Description: "Search pattern (regex or literal string)"},
			"path":       {Type: "string", Description: "Directory or file to search (default: current directory)"},
			"glob":       {Type: "string", Description: "Filter files by glob pattern, e.g. '*.go' or '**/*.test.go'"},
			"ignoreCase": {Type: "boolean", Description: "Case-insensitive search (default: false)"},
			"literal":    {Type: "boolean", Description: "Treat pattern as literal string instead of regex (default: false)"},
			"context":    {Type: "integer", Description: "Number of lines to show before and after each match (default: 0)"},
			"limit":      {Type: "integer", Description: "Maximum number of matches to return (default: 100)"},
		},
	}
}

func run(ctx context.Context, cfg *config, in map[string]any) (*agent.ToolResultBlock, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	pattern, _ := in["pattern"].(string)
	if pattern == "" {
		return tool.ErrorResult("grep: pattern must not be empty"), nil
	}

	pth, _ := in["path"].(string)
	if pth == "" {
		pth = "."
	}

	glob, _ := in["glob"].(string)
	ignoreCase, _ := in["ignoreCase"].(bool)
	literal, _ := in["literal"].(bool)
	contextLines := intOr(in["context"], 0)
	limit := intOr(in["limit"], 0)

	if limit <= 0 {
		limit = cfg.defaultLimit
	}
	if limit < 1 {
		limit = 1
	}

	root := pathutil.ResolveToCwd(pth, cfg.cwd)
	st, err := os.Stat(root)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return tool.ErrorResult(fmt.Sprintf("Path not found: %s", root)), nil
		}
		return nil, fmt.Errorf("grep: %w", err)
	}

	rePattern := pattern
	if literal {
		rePattern = regexp.QuoteMeta(pattern)
	}
	if ignoreCase {
		rePattern = "(?i)" + rePattern
	}
	re, err := regexp.Compile(rePattern)
	if err != nil {
		return tool.ErrorResult(fmt.Sprintf("grep: invalid pattern: %v", err)), nil
	}

	globPattern := glob
	globByBase := globPattern != "" && !strings.Contains(globPattern, "/")
	if globPattern != "" && !globByBase {
		globPattern = normalizeGlob(globPattern)
	}

	ignored := make(map[string]struct{}, len(cfg.ignoredDirs))
	if !cfg.includeAll {
		for _, d := range cfg.ignoredDirs {
			ignored[d] = struct{}{}
		}
	}

	var (
		out          strings.Builder
		matches      int
		linesTrunced bool
		limitReached bool
		searchRoot   = root
		searchIsDir  = st.IsDir()
	)

	emit := func(filePath, prefix string, lineNo int, text string) bool {
		rel := relPath(filePath, searchRoot, searchIsDir)
		text = strings.TrimRight(text, "\r\n")
		t, trunc := truncate.Line(text, cfg.maxLineChars)
		if trunc {
			linesTrunced = true
		}
		out.WriteString(rel)
		out.WriteString(prefix)
		fmt.Fprintf(&out, "%d", lineNo)
		out.WriteString(prefix)
		out.WriteByte(' ')
		out.WriteString(t)
		out.WriteByte('\n')
		matches++
		return matches < limit
	}

	walk := func(p string) error {
		f, err := os.Open(p) //nolint:gosec // tool intentionally reads any path passed to it
		if err != nil {
			return nil
		}
		defer func() { _ = f.Close() }()
		scanner := bufio.NewScanner(f)
		scanner.Buffer(make([]byte, 0, 64*1024), 1<<20)
		var prev []string
		var afterRemaining int
		lineNo := 0
		for scanner.Scan() {
			lineNo++
			line := scanner.Text()
			if afterRemaining > 0 {
				if !emit(p, "-", lineNo, line) {
					limitReached = true
					return errors.New("__limit__")
				}
				afterRemaining--
			}
			if re.MatchString(line) {
				if contextLines > 0 && len(prev) > 0 {
					start := lineNo - len(prev)
					for i, pl := range prev {
						if !emit(p, "-", start+i, pl) {
							limitReached = true
							return errors.New("__limit__")
						}
					}
				}
				if !emit(p, ":", lineNo, line) {
					limitReached = true
					return errors.New("__limit__")
				}
				afterRemaining = contextLines
			}
			if contextLines > 0 {
				prev = append(prev, line)
				if len(prev) > contextLines {
					prev = prev[1:]
				}
			}
		}
		return scanner.Err()
	}

	walkErr := error(nil)
	if !searchIsDir {
		walkErr = walk(root)
	} else {
		walkErr = filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
			if err != nil {
				return nil //nolint:nilerr // skip unreadable entries silently like rg does
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
			if globPattern != "" {
				rel, _ := filepath.Rel(root, p)
				rel = filepath.ToSlash(rel)
				var ok bool
				if globByBase {
					ok, _ = filepath.Match(glob, filepath.Base(p))
				} else {
					ok = matchGlob(globPattern, rel)
				}
				if !ok {
					return nil
				}
			}
			return walk(p)
		})
	}
	if walkErr != nil && walkErr.Error() != "__limit__" {
		return nil, fmt.Errorf("grep: %w", walkErr)
	}

	if matches == 0 {
		return tool.TextResult("No matches found"), nil
	}

	tr := truncate.Head(strings.TrimRight(out.String(), "\n"), truncate.Options{
		MaxLines: 1 << 30,
		MaxBytes: cfg.maxBytes,
	})

	notices := []string{}
	if limitReached {
		notices = append(notices,
			fmt.Sprintf("%d matches limit reached. Use limit=%d for more, or refine pattern", limit, limit*2))
	}
	if tr.Truncated {
		mb := cfg.maxBytes
		if mb <= 0 {
			mb = truncate.DefaultMaxBytes
		}
		notices = append(notices, fmt.Sprintf("%s limit reached", truncate.FormatSize(mb)))
	}
	if linesTrunced {
		notices = append(notices, "Some lines truncated to 500 chars. Use read tool to see full lines")
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

func relPath(file, root string, rootIsDir bool) string {
	if !rootIsDir {
		return filepath.Base(file)
	}
	rel, err := filepath.Rel(root, file)
	if err != nil || strings.HasPrefix(rel, "..") {
		return filepath.Base(file)
	}
	return filepath.ToSlash(rel)
}

// normalizeGlob 复刻 pi-mono 的 "pattern 含 / 时自动 prepend **/" 规则。
func normalizeGlob(p string) string {
	if strings.HasPrefix(p, "/") || strings.HasPrefix(p, "**/") || p == "**" {
		return strings.TrimPrefix(p, "/")
	}
	return "**/" + p
}

// matchGlob 是 doublestar 风格匹配，与 tool/find 同一份语义。
func matchGlob(pattern, path string) bool {
	return matchSegments(strings.Split(pattern, "/"), strings.Split(path, "/"))
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
