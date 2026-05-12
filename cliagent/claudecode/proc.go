package claudecode

import (
	"context"
	"io"
	"os"
	"os/exec"
)

// procOptions 启动一个子进程所需的全部信息。
type procOptions struct {
	Binary string
	Args   []string
	Cwd    string
	Env    []string // full env slice (KEY=VAL) — execRunner 直接 set
}

// processHandle 一个已启动的子进程。
type processHandle interface {
	Stdout() io.Reader
	Stderr() io.Reader
	Stdin() io.WriteCloser // 持久化模式写 user frame
	Wait() error
	Kill() error
	Signal(sig os.Signal) error
}

// processRunner 启动子进程的抽象。测试用 fake 实现。
type processRunner interface {
	Start(ctx context.Context, opts procOptions) (processHandle, error)
}

// execRunner 基于 os/exec 的默认实现。
type execRunner struct{}

// execHandle 包装 *exec.Cmd + pipes。
type execHandle struct {
	cmd    *exec.Cmd
	stdout io.ReadCloser
	stderr io.ReadCloser
	stdin  io.WriteCloser
}

func (h *execHandle) Stdout() io.Reader     { return h.stdout }
func (h *execHandle) Stderr() io.Reader     { return h.stderr }
func (h *execHandle) Stdin() io.WriteCloser { return h.stdin }
func (h *execHandle) Wait() error           { return h.cmd.Wait() }
func (h *execHandle) Kill() error {
	if h.cmd.Process == nil {
		return nil
	}
	return h.cmd.Process.Kill()
}

func (h *execHandle) Signal(sig os.Signal) error {
	if h.cmd.Process == nil {
		return nil
	}
	return h.cmd.Process.Signal(sig)
}

func (r *execRunner) Start(ctx context.Context, opts procOptions) (processHandle, error) {
	cmd := exec.CommandContext(ctx, opts.Binary, opts.Args...) //nolint:gosec // user-configured CLI binary & args; tainted input is the intended contract
	if opts.Cwd != "" {
		cmd.Dir = opts.Cwd
	}
	if opts.Env != nil {
		cmd.Env = opts.Env
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, err
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	return &execHandle{cmd: cmd, stdout: stdout, stderr: stderr, stdin: stdin}, nil
}
