package coding

import (
	"github.com/cago-frame/agents/tool"
	"github.com/cago-frame/agents/tool/bash"
	"github.com/cago-frame/agents/tool/edit"
	"github.com/cago-frame/agents/tool/find"
	"github.com/cago-frame/agents/tool/grep"
	"github.com/cago-frame/agents/tool/ls"
	"github.com/cago-frame/agents/tool/read"
	"github.com/cago-frame/agents/tool/state"
	"github.com/cago-frame/agents/tool/todo"
	"github.com/cago-frame/agents/tool/write"
)

// Tools 返回 7 件套（read+write+edit+bash+grep+find+ls），无状态、无 read tracker，
// edit / write 不强制 read-before-edit。需要 read-before-edit + bash 后台 + todo 时用 NewSession。
func Tools(cwd string) []tool.Tool {
	return []tool.Tool{
		read.New(read.Cwd(cwd)),
		write.New(write.Cwd(cwd)),
		edit.New(edit.Cwd(cwd)),
		bash.New(bash.Cwd(cwd)),
		grep.New(grep.Cwd(cwd)),
		find.New(find.Cwd(cwd)),
		ls.New(ls.Cwd(cwd)),
	}
}

// ReadOnly 返回 4 件只读（read+grep+find+ls），无 tracker。
func ReadOnly(cwd string) []tool.Tool {
	return []tool.Tool{
		read.New(read.Cwd(cwd)),
		grep.New(grep.Cwd(cwd)),
		find.New(find.Cwd(cwd)),
		ls.New(ls.Cwd(cwd)),
	}
}

// Session 把 cwd / *state.ReadTracker / *bash.JobManager / *todo.List 串到一组工具上。
// 跨方法调用（Coding / ReadOnly / All）共享同一组状态：read 完登记一次，后续 edit /
// write 都看得到；后台 bash 启动一次，后续 bash_output / kill_shell 都查得到。
type Session struct {
	cwd     string
	tracker *state.ReadTracker
	jobs    *bash.JobManager
	todos   *todo.List
}

// NewSession 创建一个挂载到 cwd 的 Session。每次调用都构造独立的 tracker / jobs / todos —
// 不同 Session 之间状态互不干扰。
func NewSession(cwd string) *Session {
	return &Session{
		cwd:     cwd,
		tracker: state.NewReadTracker(),
		jobs:    bash.NewJobManager(),
		todos:   todo.NewList(),
	}
}

// Tracker 暴露 Session 内部 *ReadTracker（如调用者要直接读 / 重置 read 记录）。
func (s *Session) Tracker() *state.ReadTracker { return s.tracker }

// Jobs 暴露 Session 内部 *JobManager（如调用者要枚举 / 主动 StopAll）。
func (s *Session) Jobs() *bash.JobManager { return s.jobs }

// Todos 暴露 Session 内部 *todo.List（宿主可观察 / 初始化任务列表）。
func (s *Session) Todos() *todo.List { return s.todos }

// bashTrio 返回挂同一 JobManager 的 bash + bash_output + kill_shell 三件套。
func (s *Session) bashTrio() []tool.Tool {
	return []tool.Tool{
		bash.New(bash.Cwd(s.cwd), bash.Jobs(s.jobs)),
		bash.NewOutput(s.jobs),
		bash.NewKill(s.jobs),
	}
}

// Coding 返回 read + write + edit + bash 系（含后台 + bash_output + kill_shell）共 6 件，
// read / write / edit 已挂同一 tracker。不含 grep / find / ls / todo。
func (s *Session) Coding() []tool.Tool {
	trio := s.bashTrio()
	tools := make([]tool.Tool, 0, 3+len(trio))
	tools = append(tools,
		read.New(read.Cwd(s.cwd), read.Tracker(s.tracker)),
		write.New(write.Cwd(s.cwd), write.Tracker(s.tracker)),
		edit.New(edit.Cwd(s.cwd), edit.Tracker(s.tracker)),
	)
	tools = append(tools, trio...)
	return tools
}

// ReadOnly 返回 read + grep + find + ls 四件只读（read 挂 tracker）。不含 todo。
func (s *Session) ReadOnly() []tool.Tool {
	return []tool.Tool{
		read.New(read.Cwd(s.cwd), read.Tracker(s.tracker)),
		grep.New(grep.Cwd(s.cwd)),
		find.New(find.Cwd(s.cwd)),
		ls.New(ls.Cwd(s.cwd)),
	}
}

// All 返回完整 10 件套：read + write + edit + bash + bash_output + kill_shell + grep + find + ls + todo_write。
// read / write / edit 挂同一 tracker；bash 系挂同一 JobManager；todo_write 挂同一 *todo.List。
func (s *Session) All() []tool.Tool {
	trio := s.bashTrio()
	tools := make([]tool.Tool, 0, 3+len(trio)+4)
	tools = append(tools,
		read.New(read.Cwd(s.cwd), read.Tracker(s.tracker)),
		write.New(write.Cwd(s.cwd), write.Tracker(s.tracker)),
		edit.New(edit.Cwd(s.cwd), edit.Tracker(s.tracker)),
	)
	tools = append(tools, trio...)
	tools = append(tools,
		grep.New(grep.Cwd(s.cwd)),
		find.New(find.Cwd(s.cwd)),
		ls.New(ls.Cwd(s.cwd)),
		todo.New(todo.WithList(s.todos)),
	)
	return tools
}
