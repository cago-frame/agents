package bash

import (
	"bytes"
	"context"
	cryptorand "crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"os/exec"
	"sync"
	"time"
)

// JobStatus 描述 bash 后台任务在某时刻的状态。
type JobStatus string

const (
	StatusRunning   JobStatus = "running"
	StatusCompleted JobStatus = "completed"
	StatusKilled    JobStatus = "killed"
	StatusError     JobStatus = "error"
)

// JobManager 管理一组 bash 后台任务（shell_id → *Job）。线程安全。
type JobManager struct {
	mu   sync.Mutex
	jobs map[string]*Job
}

// NewJobManager 构造一个空 JobManager。
func NewJobManager() *JobManager { return &JobManager{jobs: map[string]*Job{}} }

// Job 是单个 bash 后台进程的 handle。Output() / Status() / Kill() 都线程安全。
type Job struct {
	ID        string
	Command   string
	StartedAt time.Time

	mu       sync.Mutex
	cmd      *exec.Cmd
	output   bytes.Buffer
	cursor   int
	status   JobStatus
	exitCode int
	exitErr  error
	endedAt  time.Time
	killed   bool
	cancel   context.CancelFunc
	doneCh   chan struct{}
}

// Status 返回当前快照。
func (j *Job) Snapshot() JobSnapshot {
	j.mu.Lock()
	defer j.mu.Unlock()
	return JobSnapshot{
		ID:        j.ID,
		Command:   j.Command,
		Status:    j.status,
		StartedAt: j.StartedAt,
		EndedAt:   j.endedAt,
		ExitCode:  j.exitCode,
		ExitError: j.exitErr,
		BytesRead: j.cursor,
		BytesAll:  j.output.Len(),
	}
}

// JobSnapshot 是不持锁的状态摘要。
type JobSnapshot struct {
	ID        string
	Command   string
	Status    JobStatus
	StartedAt time.Time
	EndedAt   time.Time
	ExitCode  int
	ExitError error
	BytesRead int
	BytesAll  int
}

// Output 返回从上次 Output 调用以来的增量；reset=true 时返回全量并重置游标。
func (j *Job) Output(reset bool) string {
	j.mu.Lock()
	defer j.mu.Unlock()
	full := j.output.Bytes()
	if reset {
		j.cursor = 0
	}
	if j.cursor >= len(full) {
		out := ""
		j.cursor = len(full)
		return out
	}
	out := string(full[j.cursor:])
	j.cursor = len(full)
	return out
}

// FullOutput 返回累计完整输出（不动游标）。
func (j *Job) FullOutput() string {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.output.String()
}

// Wait 阻塞直到 job 终止（超时由 ctx 控）。
func (j *Job) Wait(ctx context.Context) error {
	select {
	case <-j.doneCh:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Kill 终止任务（unix 下杀进程组）。重复调用安全。
func (j *Job) Kill() error {
	j.mu.Lock()
	if j.cmd == nil || j.cmd.Process == nil {
		j.mu.Unlock()
		return nil
	}
	if j.status != StatusRunning {
		j.mu.Unlock()
		return nil
	}
	j.killed = true
	cmd := j.cmd
	cancel := j.cancel
	j.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	return killGroup(cmd)
}

// List 列出全部任务（最近启动的在前）。
func (m *JobManager) List() []JobSnapshot {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]JobSnapshot, 0, len(m.jobs))
	for _, j := range m.jobs {
		out = append(out, j.Snapshot())
	}
	// 简单按 StartedAt 倒序
	for i := 1; i < len(out); i++ {
		for k := i; k > 0 && out[k].StartedAt.After(out[k-1].StartedAt); k-- {
			out[k], out[k-1] = out[k-1], out[k]
		}
	}
	return out
}

// Get 取出一个任务。
func (m *JobManager) Get(id string) (*Job, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	j, ok := m.jobs[id]
	return j, ok
}

// register 注册新任务到表里。
func (m *JobManager) register(j *Job) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.jobs[j.ID] = j
}

// newJobID 用 crypto/rand 生成 12 位 hex 作为 shell id。
func newJobID() string {
	b := make([]byte, 6)
	if _, err := io.ReadFull(cryptorand.Reader, b); err != nil {
		// 退化到时间戳，仍要保证唯一
		return hex.EncodeToString([]byte(time.Now().Format(time.RFC3339Nano)))
	}
	return hex.EncodeToString(b)
}

// StopAll 终止所有注册过的后台 job。逐个 Kill，遇到的错误用 errors.Join 累计返回。
// 已经退出的 job（StatusCompleted / StatusKilled / StatusError）会被 Kill 跳过（Kill 内部 cancel 是幂等的）。
func (m *JobManager) StopAll() error {
	m.mu.Lock()
	jobs := make([]*Job, 0, len(m.jobs))
	for _, j := range m.jobs {
		jobs = append(jobs, j)
	}
	m.mu.Unlock()

	var errs []error
	for _, j := range jobs {
		if err := j.Kill(); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// errJobUnknown 在 bash_output / kill_shell 找不到 shell_id 时返回。
var errJobUnknown = errors.New("unknown shell_id")
