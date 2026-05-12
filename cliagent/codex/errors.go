package codex

import (
	"errors"
	"fmt"
	"strings"

	"github.com/cago-frame/agents/cliagent/internal/runtime"
)

// ErrBridgeStart 代表 mcp bridge 启动失败。
var ErrBridgeStart = errors.New("codex: mcp bridge failed to start")

// ErrProcessDead codex app-server 进程意外退出。
var ErrProcessDead = errors.New("codex: process exited unexpectedly")

// Re-exports of the shared runtime sentinels. codex public API surface
// preserves these names so callers don't have to import the internal runtime
// package.
var (
	ErrSteeringUnavailable = runtime.ErrSteeringUnavailable
	ErrSessionBusy         = runtime.ErrSessionBusy
	ErrIncompatibleOption  = runtime.ErrIncompatibleOption
	ErrStreamClosed        = runtime.ErrStreamClosed
	ErrNoActiveStream      = runtime.ErrNoActiveStream
	ErrFollowUpUnavailable = runtime.ErrFollowUpUnavailable
)

// ExitError reports a non-zero exit from the codex app-server process.
type ExitError struct {
	Err    error
	Stderr string
}

func (e *ExitError) Error() string {
	if e == nil {
		return ""
	}
	stderr := strings.TrimSpace(e.Stderr)
	if stderr == "" {
		return fmt.Sprintf("codex: process exited: %v", e.Err)
	}
	return fmt.Sprintf("codex: process exited: %v: %s", e.Err, stderr)
}

func (e *ExitError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}
