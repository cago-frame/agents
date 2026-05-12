package bash

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"sync"
	"time"

	agent "github.com/cago-frame/agents/agent"
	"github.com/cago-frame/agents/tool"
	"github.com/cago-frame/agents/tool/internal/truncate"
)

// runBackground 启动一个后台 shell 进程，立刻返回带 shell_id 的 TextResult，
// 后续输出由 bash_output 工具读，进程由 kill_shell 终止。
func runBackground(ctx context.Context, cfg *config, displayCmd, resolvedCmd string) (*agent.ToolResultBlock, error) {
	jobCtx, cancel := context.WithCancel(context.Background())

	cmd := exec.CommandContext(jobCtx, cfg.shell, append(cfg.shellArgs, resolvedCmd)...) //nolint:gosec
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
		cancel()
		return nil, fmt.Errorf("bash: stdout pipe: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("bash: stderr pipe: %w", err)
	}
	if err := cmd.Start(); err != nil {
		cancel()
		return nil, fmt.Errorf("bash: start: %w", err)
	}

	job := &Job{
		ID:        newJobID(),
		Command:   displayCmd,
		StartedAt: time.Now(),
		cmd:       cmd,
		status:    StatusRunning,
		cancel:    cancel,
		doneCh:    make(chan struct{}),
	}
	cfg.jobs.register(job)

	var pumpWG sync.WaitGroup
	pumpWG.Add(2)
	go pumpToJob(&pumpWG, stdout, job)
	go pumpToJob(&pumpWG, stderr, job)

	go func() {
		pumpWG.Wait()
		waitErr := cmd.Wait()
		job.mu.Lock()
		job.endedAt = time.Now()
		switch {
		case job.killed:
			job.status = StatusKilled
		case waitErr == nil:
			job.status = StatusCompleted
			job.exitCode = 0
		default:
			var ee *exec.ExitError
			if errors.As(waitErr, &ee) {
				job.status = StatusCompleted
				job.exitCode = ee.ExitCode()
			} else {
				job.status = StatusError
				job.exitErr = waitErr
				job.exitCode = -1
			}
		}
		close(job.doneCh)
		job.mu.Unlock()
		cancel()
	}()

	// suppress unused-context warning from linter; ctx 是给 caller 用的，
	// background job 自己有 jobCtx，不复用 caller 的取消。
	_ = ctx

	return tool.TextResult(fmt.Sprintf(
		"Started bash %s (running in background). Use bash_output with shell_id=%q to retrieve stdout/stderr; kill_shell to terminate.",
		job.ID, job.ID,
	)), nil
}

func pumpToJob(wg *sync.WaitGroup, r io.Reader, j *Job) {
	defer wg.Done()
	buf := make([]byte, 4096)
	for {
		n, err := r.Read(buf)
		if n > 0 {
			j.mu.Lock()
			j.output.Write(buf[:n])
			j.mu.Unlock()
		}
		if err != nil {
			return
		}
	}
}

// NewOutput 构造一个 bash_output 工具：按 shell_id 读取后台 bash 的累计输出。
// 默认只返回上次未读取的增量；reset=true 时返回全量并复位游标。
func NewOutput(jm *JobManager) tool.Tool {
	if jm == nil {
		jm = NewJobManager()
	}
	return &tool.RawTool{
		NameStr:          "bash_output",
		DescStr:          "Retrieve stdout/stderr from a background bash shell started with run_in_background=true. By default returns only output appended since the last call (incremental); set reset=true for the full buffer.",
		SchemaVal:        bashOutputSchema(),
		PromptSnippetStr: "Tail the rolling output of a backgrounded bash job.",
		PromptGuidelinesArr: []string{
			"Pair with bash run_in_background=true; pass the returned shell_id.",
		},
		Handler: func(ctx context.Context, in map[string]any) (*agent.ToolResultBlock, error) {
			shellID, _ := in["shell_id"].(string)
			if shellID == "" {
				return tool.ErrorResult("bash_output: shell_id must not be empty"), nil
			}
			reset, _ := in["reset"].(bool)
			filter, _ := in["filter"].(string)

			job, ok := jm.Get(shellID)
			if !ok {
				return tool.ErrorResult(fmt.Sprintf("bash_output: %s: %s", errJobUnknown, shellID)), nil
			}
			out := job.Output(reset)
			if filter != "" {
				out = applyLineFilter(out, filter)
			}
			snap := job.Snapshot()
			return tool.TextResult(formatOutputResponse(out, snap)), nil
		},
	}
}

func bashOutputSchema() agent.Schema {
	return agent.Schema{
		Type:     "object",
		Required: []string{"shell_id"},
		Properties: map[string]*agent.Property{
			"shell_id": {Type: "string", Description: "Shell id returned by bash run_in_background=true."},
			"reset":    {Type: "boolean", Description: "If true, return full output and reset the read cursor to start. Default false (incremental)."},
			"filter":   {Type: "string", Description: "Optional regex to filter output lines (matched per line)."},
		},
	}
}

func formatOutputResponse(body string, s JobSnapshot) string {
	header := fmt.Sprintf("shell %s status=%s", s.ID, s.Status)
	switch s.Status {
	case StatusCompleted:
		header += fmt.Sprintf(" exit=%d", s.ExitCode)
	case StatusError:
		if s.ExitError != nil {
			header += fmt.Sprintf(" error=%s", s.ExitError)
		}
	}
	if body == "" {
		return header + "\n(no new output)"
	}
	tr := truncate.Tail(body, truncate.Options{})
	if tr.Truncated {
		return header + "\n" + tr.Content + fmt.Sprintf("\n[Showing last %s of accumulated output; total %d bytes. Call bash_output again to read more, or reset=true for full buffer.]",
			truncate.FormatSize(tr.OutputBytes), s.BytesAll)
	}
	return header + "\n" + body
}

func applyLineFilter(body, pattern string) string {
	re, err := compileFilter(pattern)
	if err != nil || re == nil {
		return body
	}
	var b []byte
	start := 0
	for i := 0; i <= len(body); i++ {
		if i == len(body) || body[i] == '\n' {
			line := body[start:i]
			if re.MatchString(line) {
				b = append(b, line...)
				if i < len(body) {
					b = append(b, '\n')
				}
			}
			start = i + 1
		}
	}
	return string(b)
}

// NewKill 构造一个 kill_shell 工具：终止指定 shell_id 的后台 bash。
func NewKill(jm *JobManager) tool.Tool {
	if jm == nil {
		jm = NewJobManager()
	}
	return &tool.RawTool{
		NameStr:          "kill_shell",
		DescStr:          "Terminate a background bash shell started with run_in_background=true.",
		IsSerial:         false,
		SchemaVal:        bashKillSchema(),
		PromptSnippetStr: "Terminate a backgrounded bash job by shell_id.",
		PromptGuidelinesArr: []string{
			"Always kill background jobs you started once they are no longer needed.",
		},
		Handler: func(ctx context.Context, in map[string]any) (*agent.ToolResultBlock, error) {
			shellID, _ := in["shell_id"].(string)
			if shellID == "" {
				return tool.ErrorResult("kill_shell: shell_id must not be empty"), nil
			}
			job, ok := jm.Get(shellID)
			if !ok {
				return tool.ErrorResult(fmt.Sprintf("kill_shell: %s: %s", errJobUnknown, shellID)), nil
			}
			if err := job.Kill(); err != nil {
				return nil, fmt.Errorf("kill_shell: %w", err)
			}
			waitCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
			defer cancel()
			_ = job.Wait(waitCtx)
			snap := job.Snapshot()
			return tool.TextResult(fmt.Sprintf("shell %s status=%s", snap.ID, snap.Status)), nil
		},
	}
}

func bashKillSchema() agent.Schema {
	return agent.Schema{
		Type:     "object",
		Required: []string{"shell_id"},
		Properties: map[string]*agent.Property{
			"shell_id": {Type: "string", Description: "Shell id returned by bash run_in_background=true."},
		},
	}
}
