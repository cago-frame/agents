package claudecode

import (
	"io"
	"sync"
	"sync/atomic"

	"github.com/cago-frame/agents/cliagent/internal/runtime"
)

// liveProcess 一个持久化 claude 进程的运行时状态。
// 由 processManager.mu 保护其在 processManager.live 中的登记状态；
// 自身的 turnMu / closedCh 由 reader/demuxer goroutine 协调。
type liveProcess struct {
	proc  processHandle
	stdin io.WriteCloser

	threadID string // session_id returned by system.init; keyed in processManager.live

	events             chan runtime.Event  // reader goroutine 写；turn demuxer 读；reader 退出时 close
	readyCh            chan struct{}       // close 后表示 threadID 已捕获
	closedCh           chan struct{}       // close 后表示 reader goroutine 已退出
	exitErr            atomic.Value        // error; proc.Wait 的结果
	mcpRegisteredNames map[string]struct{} // MCP-bridge tool names, passed to translateStream

	turnMu  sync.Mutex  // 同一 session 一次只能跑一轮
	closing atomic.Bool // closeSessionByID 已开始执行——reader 看到 EOF 时别把 sid 加进 deadSessions
}
