package claudecode

import (
	"errors"
	"fmt"

	"github.com/cago-frame/agents/cliagent/internal/runtime"
)

// ErrBinaryNotFound backend 指定的 binary 在 PATH 中找不到。
var ErrBinaryNotFound = errors.New("claudecode: binary not found")

// ErrBridgeStart mcp bridge 启动失败。
var ErrBridgeStart = errors.New("claudecode: mcp bridge failed to start")

// Re-exports of the shared runtime sentinels. claudecode public API surface
// preserves these names so callers don't have to import the internal runtime
// package.
var (
	ErrSteeringUnavailable = runtime.ErrSteeringUnavailable
	ErrSessionBusy         = runtime.ErrSessionBusy
	ErrIncompatibleOption  = runtime.ErrIncompatibleOption
	ErrStreamClosed        = runtime.ErrStreamClosed
	ErrNoActiveStream      = runtime.ErrNoActiveStream
	ErrFollowUpUnavailable = runtime.ErrFollowUpUnavailable
	ErrPermissionResponse  = runtime.ErrPermissionResponse
)

// ProcessExitError CLI 进程以非 0 退出。
type ProcessExitError struct {
	Code   int
	Stderr string // 最后 ≤4KB stderr
}

func (e *ProcessExitError) Error() string {
	return fmt.Sprintf("claudecode: process exited with %d: %s", e.Code, e.Stderr)
}

// StreamParseWarning 协议层解析失败（不中断整个 Run）。
type StreamParseWarning struct {
	Line int
	Raw  string // 截断到 512 字节
	Err  error
}

func (w *StreamParseWarning) Error() string {
	return fmt.Sprintf("claudecode: parse warning line %d: %v: %s", w.Line, w.Err, w.Raw)
}

// ErrProcessDead 持久化模式下 claude 进程意外退出。
var ErrProcessDead = errors.New("claudecode: claude process exited unexpectedly")

// ErrSessionDead 调用方试图在已死的 session_id 上 Send；
// 需要带入原 session_id 的 --resume 来恢复。
var ErrSessionDead = errors.New("claudecode: session terminated (process died); open a new session with this id to resume")

// ErrInitTimeout 持久化模式下 spawn 后未在 InitTimeout 内收到 system.init 事件。
var ErrInitTimeout = errors.New("claudecode: claude process did not emit init in time")
