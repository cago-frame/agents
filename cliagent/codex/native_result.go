package codex

import "github.com/cago-frame/agents/provider"

// State 运行间持久化的轻量状态(thread id + 自定义键值)。
type State struct {
	ThreadID string
	Values   map[string]string
}

// Clone 深拷贝 Values map,返回独立副本。
func (s State) Clone() State {
	out := State{ThreadID: s.ThreadID}
	if s.Values != nil {
		out.Values = make(map[string]string, len(s.Values))
		for k, v := range s.Values {
			out.Values[k] = v
		}
	}
	return out
}

// Result 一次 stream 完成后的聚合结果。
type Result struct {
	Text     string
	Messages []Message
	Usage    provider.Usage
	Stop     StopReason
	State    State
}
