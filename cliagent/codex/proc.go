package codex

import (
	"context"
	"io"
	"os"
	"os/exec"
)

type procOptions struct {
	Binary string
	Args   []string
	Cwd    string
	Env    []string
}

type processHandle interface {
	Stdin() io.Writer
	Stdout() io.Reader
	Stderr() io.Reader
	Wait() error
	Kill() error
	Signal(sig os.Signal) error
}

type appServerRunner interface {
	Start(ctx context.Context, opts procOptions) (processHandle, error)
}

type execAppServerRunner struct{ execRunner }

type execRunner struct{}

type execHandle struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout io.ReadCloser
	stderr io.ReadCloser
}

func (h *execHandle) Stdin() io.Writer  { return h.stdin }
func (h *execHandle) Stdout() io.Reader { return h.stdout }
func (h *execHandle) Stderr() io.Reader { return h.stderr }
func (h *execHandle) Wait() error       { return h.cmd.Wait() }

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
	cmd := exec.CommandContext(ctx, opts.Binary, opts.Args...) //nolint:gosec // user-configured CLI binary and args are intentional.
	if opts.Cwd != "" {
		cmd.Dir = opts.Cwd
	}
	if opts.Env != nil {
		cmd.Env = opts.Env
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	return &execHandle{cmd: cmd, stdin: stdin, stdout: stdout, stderr: stderr}, nil
}
