// Package bash 暴露一个最小的"在 cwd 下跑 shell 命令"工具。
// 不持久化 shell：每次调用都是新进程、新 env、新 PWD（cwd 在构造时固定）。
// 行为对齐 pi-mono bash.ts：
//   - tail 截断到 50KB / 2000 行；超出时把全输出写到 ${TMPDIR}/cago-bash-XXXXXX.log
//   - 非零退出 → 返回 IsError=true ToolResultBlock（nil Go error），output 携带 "Command exited with code N"
//   - 超时 / 取消 → return (nil, ctx.Err()) — genuine Go error so the runner sees cancellation
package bash

import (
	"bytes"
	"context"
	cryptorand "crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"

	agent "github.com/cago-frame/agents/agent"
	"github.com/cago-frame/agents/tool"
	"github.com/cago-frame/agents/tool/internal/truncate"
)

// Name 是工具暴露给 LLM 的名字。
const Name = "bash"

// Description 与 pi-mono 对齐。
var Description = fmt.Sprintf(
	"Execute a bash command in the current working directory. Returns stdout and stderr. Output is truncated to last %d lines or %dKB (whichever is hit first). If truncated, full output is saved to a temp file. Optionally provide a timeout in seconds.",
	truncate.DefaultMaxLines, truncate.DefaultMaxBytes/1024,
)

// Option 可选构造项。
type Option func(*config)

type config struct {
	cwd            string
	shell          string
	shellArgs      []string
	commandPrefix  string
	defaultTimeout time.Duration
	env            []string
	maxLines       int
	maxBytes       int
	jobs           *JobManager
	serial         bool
}

// Cwd 指定命令的执行目录。
func Cwd(p string) Option { return func(c *config) { c.cwd = p } }

// Shell 覆盖默认 shell。args 为空时按 shell 名称选择默认命令参数。
func Shell(path string, args ...string) Option {
	return func(c *config) {
		c.shell = path
		if len(args) == 0 {
			c.shellArgs = defaultArgsForShell(path)
			return
		}
		c.shellArgs = args
	}
}

// CommandPrefix 在每次调用前拼到命令前面（如 ". ./setup.sh"）。
// 拼接方式：prefix + "\n" + 用户命令。
func CommandPrefix(s string) Option { return func(c *config) { c.commandPrefix = s } }

// DefaultTimeout 在用户没指定 timeout 时生效；零值代表不超时。
func DefaultTimeout(d time.Duration) Option { return func(c *config) { c.defaultTimeout = d } }

// Env 覆盖子进程环境变量；零值（nil）继承当前进程环境。
func Env(env []string) Option { return func(c *config) { c.env = env } }

// MaxLines 覆盖输出行数上限。
func MaxLines(n int) Option { return func(c *config) { c.maxLines = n } }

// MaxBytes 覆盖输出字节上限。
func MaxBytes(n int) Option { return func(c *config) { c.maxBytes = n } }

// Jobs 注入一个 *JobManager，让 bash 工具支持 run_in_background=true。
// 没注入时，run_in_background=true 返回 IsError=true（明确报错而非静默降级）。
func Jobs(m *JobManager) Option { return func(c *config) { c.jobs = m } }

// Serial 让该工具与同一轮的其他工具串行执行。
func Serial() Option { return func(c *config) { c.serial = true } }

// New 构造 bash 工具。
func New(opts ...Option) tool.Tool {
	cfg := &config{
		shell: defaultShell(),
	}
	cfg.shellArgs = defaultArgsForShell(cfg.shell)
	for _, o := range opts {
		o(cfg)
	}
	return &tool.RawTool{
		NameStr:   Name,
		DescStr:   Description,
		SchemaVal: bashSchema(),
		IsSerial:  cfg.serial,
		Handler: func(ctx context.Context, in map[string]any) (*agent.ToolResultBlock, error) {
			return run(ctx, cfg, in)
		},
		PromptSnippetStr: "Run a shell command and capture stdout/stderr.",
		PromptGuidelinesArr: []string{
			"Use kill_shell to terminate background jobs you no longer need.",
			"Quote paths with spaces; the working directory is the configured cwd.",
		},
	}
}

func bashSchema() agent.Schema {
	return agent.Schema{
		Type:     "object",
		Required: []string{"command"},
		Properties: map[string]*agent.Property{
			"command":           {Type: "string", Description: "Bash command to execute"},
			"timeout":           {Type: "number", Description: "Timeout in seconds (optional, no default timeout)"},
			"run_in_background": {Type: "boolean", Description: "If true, run the command in the background and return a shell_id immediately. Use bash_output to retrieve stdout/stderr later, kill_shell to terminate. Default false."},
		},
	}
}

func run(ctx context.Context, cfg *config, in map[string]any) (*agent.ToolResultBlock, error) {
	command, _ := in["command"].(string)
	if command == "" {
		return tool.ErrorResult("bash: command must not be empty"), nil
	}

	bg, _ := in["run_in_background"].(bool)

	if cfg.cwd != "" {
		if st, err := os.Stat(cfg.cwd); err != nil || !st.IsDir() {
			return tool.ErrorResult(fmt.Sprintf("Working directory does not exist: %s\nCannot execute bash commands.", cfg.cwd)), nil
		}
	}

	resolvedCmd := command
	if cfg.commandPrefix != "" {
		resolvedCmd = cfg.commandPrefix + "\n" + command
	}

	if bg {
		if cfg.jobs == nil {
			return tool.ErrorResult("bash: run_in_background=true requires a JobManager; configure with bash.Jobs(jm)"), nil
		}
		return runBackground(ctx, cfg, command, resolvedCmd)
	}

	// Extract timeout from map (JSON numbers come as float64).
	var timeoutSec float64
	switch v := in["timeout"].(type) {
	case float64:
		timeoutSec = v
	case int:
		timeoutSec = float64(v)
	case int64:
		timeoutSec = float64(v)
	}

	timeout := cfg.defaultTimeout
	if timeoutSec > 0 {
		timeout = time.Duration(timeoutSec * float64(time.Second))
	}

	runCtx := ctx
	var cancel context.CancelFunc
	if timeout > 0 {
		runCtx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}

	cmd := exec.CommandContext(runCtx, cfg.shell, append(cfg.shellArgs, resolvedCmd)...) //nolint:gosec // command is a deliberate tool input
	if cfg.cwd != "" {
		cmd.Dir = cfg.cwd
	}
	if cfg.env != nil {
		cmd.Env = cfg.env
	}
	setProcessGroup(cmd)
	cmd.Cancel = func() error { return killGroup(cmd) }

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("bash: stdout pipe: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, fmt.Errorf("bash: stderr pipe: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("bash: start: %w", err)
	}

	maxBytes := cfg.maxBytes
	if maxBytes <= 0 {
		maxBytes = truncate.DefaultMaxBytes
	}
	collector := newCollector(maxBytes)

	var wg sync.WaitGroup
	wg.Add(2)
	go pump(&wg, stdout, collector)
	go pump(&wg, stderr, collector)
	wg.Wait()
	waitErr := cmd.Wait()
	collector.close()

	full := collector.bytes()
	tr := truncate.Tail(string(full), truncate.Options{
		MaxLines: cfg.maxLines,
		MaxBytes: maxBytes,
	})

	tempPath := ""
	if tr.Truncated {
		tempPath = collector.tempPath()
	}

	body := tr.Content
	if body == "" {
		body = "(no output)"
	}
	notice := buildNotice(tr, tempPath, maxBytes)
	if notice != "" {
		body = body + "\n\n" + notice
	}

	if waitErr != nil {
		// timeout / cancel — genuine Go errors (runner must see them)
		switch {
		case errors.Is(runCtx.Err(), context.DeadlineExceeded):
			body = trimEmptyLeader(tr.Content) + fmt.Sprintf("\n\nCommand timed out after %g seconds", timeout.Seconds())
			return nil, errors.New(body)
		case errors.Is(runCtx.Err(), context.Canceled), errors.Is(ctx.Err(), context.Canceled):
			body = trimEmptyLeader(tr.Content) + "\n\nCommand aborted"
			return nil, errors.New(body)
		}
		// Non-zero exit: IsError=true ToolResultBlock, nil Go error.
		exitCode := -1
		var ee *exec.ExitError
		if errors.As(waitErr, &ee) {
			exitCode = ee.ExitCode()
		}
		errBody := trimEmptyLeader(tr.Content) + fmt.Sprintf("\n\nCommand exited with code %d", exitCode)
		if notice != "" {
			errBody = errBody + "\n\n" + notice
		}
		return tool.ErrorResult(errBody), nil
	}

	return tool.TextResult(body), nil
}

func trimEmptyLeader(s string) string {
	if s == "" {
		return "(no output)"
	}
	return s
}

func buildNotice(tr truncate.Result, tempPath string, maxBytes int) string {
	if !tr.Truncated {
		return ""
	}
	startLine := max(tr.TotalLines-tr.OutputLines+1, 1)
	endLine := tr.TotalLines
	if tr.LastLinePartial {
		return fmt.Sprintf(
			"[Showing last %s of line %d. Full output: %s]",
			truncate.FormatSize(tr.OutputBytes), endLine, tempPath,
		)
	}
	if tr.TruncatedBy == "lines" {
		return fmt.Sprintf("[Showing lines %d-%d of %d. Full output: %s]",
			startLine, endLine, tr.TotalLines, tempPath)
	}
	return fmt.Sprintf("[Showing lines %d-%d of %d (%s limit). Full output: %s]",
		startLine, endLine, tr.TotalLines, truncate.FormatSize(maxBytes), tempPath)
}

// collector 把 stdout+stderr 合并；超过 2× maxBytes 时把全输出写到 temp file，
// 并保留尾部 ~2× maxBytes 在内存。
type collector struct {
	mu        sync.Mutex
	maxBytes  int
	cap       int
	buf       *bytes.Buffer
	totalSize int
	tmp       *os.File
	tmpName   string
}

func newCollector(maxBytes int) *collector {
	return &collector{
		maxBytes: maxBytes,
		cap:      maxBytes * 2,
		buf:      &bytes.Buffer{},
	}
}

func (c *collector) write(p []byte) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.totalSize += len(p)
	if c.tmp == nil && c.totalSize > c.maxBytes {
		// 超出阈值：开 temp file，把目前为止全输出写进去
		nameBytes := make([]byte, 8)
		_, _ = io.ReadFull(cryptorand.Reader, nameBytes)
		name := filepath.Join(os.TempDir(), "cago-bash-"+hex.EncodeToString(nameBytes)+".log")
		f, err := os.Create(name) //nolint:gosec // tool intentionally writes to OS temp
		if err == nil {
			c.tmp = f
			c.tmpName = name
			_, _ = c.tmp.Write(c.buf.Bytes())
		}
	}
	if c.tmp != nil {
		_, _ = c.tmp.Write(p)
	}
	c.buf.Write(p)
	if c.buf.Len() > c.cap {
		excess := c.buf.Len() - c.cap
		_ = c.buf.Next(excess)
	}
}

func (c *collector) bytes() []byte {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.buf.Bytes()
}

func (c *collector) close() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.tmp != nil {
		_ = c.tmp.Close()
	}
}

func (c *collector) tempPath() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.tmpName
}

func pump(wg *sync.WaitGroup, r io.Reader, c *collector) {
	defer wg.Done()
	buf := make([]byte, 4096)
	for {
		n, err := r.Read(buf)
		if n > 0 {
			c.write(buf[:n])
		}
		if err != nil {
			return
		}
	}
}
