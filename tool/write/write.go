// Package write 暴露一个最小的"写文件"工具：覆盖写、自动建父目录。
// 与 pi-mono write.ts 行为对齐；通过 filemu.Default 与 edit 共用 per-path mutex。
package write

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	agent "github.com/cago-frame/agents/agent"
	"github.com/cago-frame/agents/tool"
	"github.com/cago-frame/agents/tool/internal/filemu"
	"github.com/cago-frame/agents/tool/internal/pathutil"
	"github.com/cago-frame/agents/tool/state"
)

// Name 是工具暴露给 LLM 的名字。
const Name = "write"

// Description 与 pi-mono 对齐。
const Description = "Write content to a file. Creates the file if it doesn't exist, overwrites if it does. Automatically creates parent directories."

// Option 可选构造项。
type Option func(*config)

type config struct {
	cwd     string
	pool    *filemu.Pool
	tracker *state.ReadTracker
	serial  bool
}

// Cwd 指定相对路径解析的根。
func Cwd(p string) Option { return func(c *config) { c.cwd = p } }

// Pool 覆盖默认的 per-path 文件互斥池（默认是 filemu.Default）。
func Pool(p *filemu.Pool) Option { return func(c *config) { c.pool = p } }

// Tracker 接入 *state.ReadTracker。挂上后，覆写已存在文件前必须先 read，
// 且 read 之后被外部改过会报 stale。新文件不受约束（write 本来就用来创建）。
func Tracker(t *state.ReadTracker) Option { return func(c *config) { c.tracker = t } }

// Serial 让该工具与同一轮的其他工具串行执行。
func Serial() Option { return func(c *config) { c.serial = true } }

// New 构造 write 工具。
func New(opts ...Option) tool.Tool {
	cfg := &config{}
	for _, o := range opts {
		o(cfg)
	}
	if cfg.pool == nil {
		cfg.pool = &filemu.Default
	}
	return &tool.RawTool{
		NameStr:   Name,
		DescStr:   Description,
		SchemaVal: schema(),
		IsSerial:  cfg.serial,
		Handler: func(ctx context.Context, in map[string]any) (*agent.ToolResultBlock, error) {
			return run(ctx, cfg, in)
		},
		PromptSnippetStr: "Write a complete file at a given path, creating or overwriting.",
		PromptGuidelinesArr: []string{
			"Read the file first if it already exists; write replaces it entirely.",
			"Prefer edit for partial changes.",
		},
	}
}

func schema() agent.Schema {
	return agent.Schema{
		Type:     "object",
		Required: []string{"path", "content"},
		Properties: map[string]*agent.Property{
			"path":    {Type: "string", Description: "Path to the file to write (relative or absolute)"},
			"content": {Type: "string", Description: "Content to write to the file"},
		},
	}
}

func run(ctx context.Context, cfg *config, in map[string]any) (*agent.ToolResultBlock, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	path, _ := in["path"].(string)
	if path == "" {
		return tool.ErrorResult("write: path must not be empty"), nil
	}
	content, _ := in["content"].(string)

	abs := pathutil.ResolveToCwd(path, cfg.cwd)

	// toolErr captures tracker-originated messages that must reach the LLM as content.
	var toolErr string
	var resp string
	err := cfg.pool.With(abs, func() error {
		if cfg.tracker != nil {
			if _, err := cfg.tracker.Check(abs); err != nil {
				switch {
				case errors.Is(err, state.ErrNotRead):
					if _, statErr := os.Stat(abs); statErr == nil {
						toolErr = fmt.Sprintf("%s has not been read in this session. Use read first to confirm current contents before overwriting.", path)
						return nil
					}
				case errors.Is(err, state.ErrStale):
					toolErr = fmt.Sprintf("%s has been modified since the last read. Read it again before overwriting.", path)
					return nil
				default:
					return fmt.Errorf("tracker check: %w", err)
				}
			}
		}
		if toolErr != "" {
			return nil
		}
		if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(abs, []byte(content), 0o600); err != nil {
			return err
		}
		if cfg.tracker != nil {
			cfg.tracker.Forget(abs)
		}
		resp = fmt.Sprintf("Successfully wrote %d bytes to %s", len(content), path)
		return nil
	})
	if toolErr != "" {
		return tool.ErrorResult(toolErr), nil
	}
	if err != nil {
		return nil, fmt.Errorf("write: %w", err)
	}
	return tool.TextResult(resp), nil
}
