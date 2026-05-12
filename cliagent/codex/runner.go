package codex

import (
	"context"
	"crypto/rand"
	"encoding/hex"

	"github.com/cago-frame/agents/cliagent/internal/runtime"
)

// Runner 是 codex 包对外的稳定入口。Run/Stream/Session 都返回 codex 包内的
// native 类型,用户代码不再需要 import agent。
//
// New(opts ...Option) 用统一的 codex.Option,既包含 CLI 级
// (Cwd / Model / Sandbox / Approval / ...)也包含 runner 级
// (Tools / PreToolUse / ...)。
//
// 内部走 cliagent/internal/runtime 的 Stream/Session/Hook 实现,直接驱动 codex
// app-server JSON-RPC,不再绕道 agent backend 协议。
type Runner struct {
	cfg backendCfg
	pm  *processManager
}

// New 用一组 Option 构造 Runner;Option 既可以是 backend 级也可以是 runner 级。
//
// 例如:
//
//	r := codex.New(
//	    codex.Cwd("/tmp/work"),
//	    codex.Sandbox(codex.SandboxWorkspaceWrite),
//	    codex.Tools(myTool),
//	    codex.PreToolUse(".*", myHook),
//	)
func New(opts ...Option) *Runner {
	cfg := defaultBackendCfg()
	for _, o := range opts {
		o(&cfg)
	}
	return &Runner{cfg: cfg, pm: newProcessManager(cfg)}
}

// Session 构造一个支持持久化 history / state 的会话。
func (r *Runner) Session(opts ...SessionOption) *Session {
	so := sessionOptCfg{}
	for _, o := range opts {
		o(&so)
	}
	if so.id == "" {
		so.id = newSessionID()
	}
	rs := runtime.NewSession(so.id, so.store)
	return &Session{rt: rs, runner: r}
}

// Run 启动 ephemeral session 跑一轮并等待完成。
func (r *Runner) Run(ctx context.Context, prompt string, opts ...RunOption) (*Result, error) {
	sess := r.Session()
	return sess.Send(ctx, prompt, opts...)
}

// Text 启动 ephemeral session 跑一轮并返回最终文本。
func (r *Runner) Text(ctx context.Context, prompt string, opts ...RunOption) (string, error) {
	res, err := r.Run(ctx, prompt, opts...)
	if err != nil {
		return "", err
	}
	if res == nil {
		return "", nil
	}
	return res.Text, nil
}

// Stream 启动 ephemeral session 的 stream。
func (r *Runner) Stream(ctx context.Context, prompt string, opts ...RunOption) (*Stream, error) {
	sess := r.Session()
	return sess.Stream(ctx, prompt, opts...)
}

// Close 释放 backend 资源。
func (r *Runner) Close(ctx context.Context) error {
	return r.pm.shutdownAll(ctx)
}

// newSessionID returns a random hex-formatted session id.
func newSessionID() string {
	var buf [12]byte
	_, _ = rand.Read(buf[:])
	return "codex-" + hex.EncodeToString(buf[:])
}
