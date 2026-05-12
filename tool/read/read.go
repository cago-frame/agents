// Package read 暴露一个最小的"读文件"工具，行为对齐 pi-mono read.ts 的文本路径。
// 不带图像 / TUI 渲染：那是宿主侧的事；这里只把文件内容（按 offset/limit 切片）+
// 行/字节双限截断结果返回给模型。
package read

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strings"

	agent "github.com/cago-frame/agents/agent"
	"github.com/cago-frame/agents/tool"
	"github.com/cago-frame/agents/tool/internal/pathutil"
	"github.com/cago-frame/agents/tool/internal/truncate"
	"github.com/cago-frame/agents/tool/state"
)

// Name 是工具暴露给 LLM 的名字。
const Name = "read"

// Description 与 pi-mono 对齐（行/字节阈值用默认值占位）。
var Description = fmt.Sprintf(
	"Read the contents of a file. For text files, output is truncated to %d lines or %dKB (whichever is hit first). Use offset/limit for large files. When you need the full file, continue with offset until complete.",
	truncate.DefaultMaxLines, truncate.DefaultMaxBytes/1024,
)

// Option 可选构造项。
type Option func(*config)

type config struct {
	cwd         string
	maxLines    int
	maxBytes    int
	tracker     *state.ReadTracker
	lineNumbers bool
	serial      bool
}

// Cwd 指定相对路径解析的根。零值时不补默认 cwd（caller 负责传绝对路径）。
func Cwd(p string) Option { return func(c *config) { c.cwd = p } }

// MaxLines 覆盖默认行数上限（0 / 负数 = 用默认 2000）。
func MaxLines(n int) Option { return func(c *config) { c.maxLines = n } }

// MaxBytes 覆盖默认字节上限（0 / 负数 = 用默认 50KB）。
func MaxBytes(n int) Option { return func(c *config) { c.maxBytes = n } }

// Tracker 注册一个 *state.ReadTracker。read 成功后会把 (path, mtime, size) 记进去，
// 让搭配的 edit / write 工具能强制 "read-before-edit" / 检测 stale。零值代表不约束。
func Tracker(t *state.ReadTracker) Option { return func(c *config) { c.tracker = t } }

// LineNumbers 让输出附 cat -n 风格的行号前缀（"   N\tcontent"），与 Claude Code 的 read 对齐。
// 默认关闭（pi-mono 风格 raw 输出）。
func LineNumbers() Option { return func(c *config) { c.lineNumbers = true } }

// Serial 让该工具与同一轮的其他工具串行执行。
func Serial() Option { return func(c *config) { c.serial = true } }

// New 构造 read 工具。
func New(opts ...Option) tool.Tool {
	cfg := &config{}
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
		PromptSnippetStr: "Read file contents at a given path with optional offset/limit.",
		PromptGuidelinesArr: []string{
			"Use read to examine files instead of cat or sed.",
		},
	}
}

func schema() agent.Schema {
	return agent.Schema{
		Type:     "object",
		Required: []string{"path"},
		Properties: map[string]*agent.Property{
			"path":   {Type: "string", Description: "Path to the file to read (relative or absolute)"},
			"offset": {Type: "integer", Description: "Line number to start reading from (1-indexed)"},
			"limit":  {Type: "integer", Description: "Maximum number of lines to read"},
		},
	}
}

func run(ctx context.Context, cfg *config, in map[string]any) (*agent.ToolResultBlock, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	path, _ := in["path"].(string)
	if path == "" {
		return tool.ErrorResult("read: path must not be empty"), nil
	}
	offset := intOr(in["offset"], 0)
	limit := intOr(in["limit"], 0)

	abs := pathutil.ResolveReadPath(path, cfg.cwd)
	data, err := os.ReadFile(abs) //nolint:gosec // tool is intentionally unsandboxed; see CLAUDE.md
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return tool.ErrorResult(fmt.Sprintf("read: file not found: %s", path)), nil
		}
		return nil, fmt.Errorf("read: %w", err)
	}
	if cfg.tracker != nil {
		if st, err := os.Stat(abs); err == nil {
			cfg.tracker.Record(abs, st)
		}
	}

	content := string(data)
	allLines := strings.Split(content, "\n")
	totalLines := len(allLines)

	if offset > totalLines {
		return tool.ErrorResult(fmt.Sprintf("Offset %d is beyond end of file (%d lines total)", offset, totalLines)), nil
	}

	startLine := 0
	if offset > 1 {
		startLine = offset - 1
	}
	startDisplay := startLine + 1

	userLimited := false
	endLineExclusive := totalLines
	if limit > 0 {
		userLimited = true
		endLineExclusive = min(startLine+limit, totalLines)
	}

	selected := strings.Join(allLines[startLine:endLineExclusive], "\n")

	tr := truncate.Head(selected, truncate.Options{
		MaxLines: cfg.maxLines,
		MaxBytes: cfg.maxBytes,
	})

	maxBytes := cfg.maxBytes
	if maxBytes <= 0 {
		maxBytes = truncate.DefaultMaxBytes
	}

	if tr.FirstLineExceedsLimit {
		// 首行字节超限：返回纯提示，不带内容。
		firstSize := truncate.FormatSize(len(allLines[startLine]))
		return tool.TextResult(fmt.Sprintf(
			"[Line %d is %s, exceeds %s limit. Use bash: sed -n '%dp' %s | head -c %d]",
			startDisplay, firstSize, truncate.FormatSize(maxBytes),
			startDisplay, path, maxBytes,
		)), nil
	}

	if tr.Truncated {
		endDisplay := startDisplay + tr.OutputLines - 1
		nextOffset := endDisplay + 1
		var notice string
		if tr.TruncatedBy == "lines" {
			notice = fmt.Sprintf("[Showing lines %d-%d of %d. Use offset=%d to continue.]",
				startDisplay, endDisplay, totalLines, nextOffset)
		} else {
			notice = fmt.Sprintf("[Showing lines %d-%d of %d (%s limit). Use offset=%d to continue.]",
				startDisplay, endDisplay, totalLines, truncate.FormatSize(maxBytes), nextOffset)
		}
		return tool.TextResult(formatOutput(cfg, tr.Content, startDisplay) + "\n\n" + notice), nil
	}

	if userLimited && endLineExclusive < totalLines {
		remaining := totalLines - endLineExclusive
		nextOffset := endLineExclusive + 1
		notice := fmt.Sprintf("[%d more lines in file. Use offset=%d to continue.]", remaining, nextOffset)
		return tool.TextResult(formatOutput(cfg, tr.Content, startDisplay) + "\n\n" + notice), nil
	}

	return tool.TextResult(formatOutput(cfg, tr.Content, startDisplay)), nil
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

// formatOutput 在启用 LineNumbers 时把内容 cat -n 化（行号从 startDisplay 起）。
func formatOutput(cfg *config, content string, startDisplay int) string {
	if !cfg.lineNumbers || content == "" {
		return content
	}
	lines := strings.Split(content, "\n")
	var b strings.Builder
	for i, line := range lines {
		if i > 0 {
			b.WriteByte('\n')
		}
		fmt.Fprintf(&b, "%6d\t%s", startDisplay+i, line)
	}
	return b.String()
}
