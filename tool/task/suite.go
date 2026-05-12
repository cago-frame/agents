package task

import "github.com/cago-frame/agents/tool"

// NewSuite 一次性返回 5 个 task_* 工具：create / list / get / update / delete。
// 所有工具共享 opts 里的 *Store（或各自匿名新建——一般要 WithStore 串起来）。
func NewSuite(opts ...Option) []tool.Tool {
	return []tool.Tool{
		NewCreate(opts...),
		NewList(opts...),
		NewGet(opts...),
		NewUpdate(opts...),
		NewDelete(opts...),
	}
}
