package codex

import (
	"context"

	"github.com/cago-frame/agents/cliagent/internal/runtime"
)

// Stream 是 codex 暴露的事件流。包装 runtime.Stream,翻译每个事件成
// codex.Event 后再返回给消费者。
//
// 用法:
//
//	stream, err := sess.Stream(ctx, "hello")
//	for stream.Next() {
//	    ev := stream.Event()
//	    // ...
//	}
//	res, err := stream.Result()
type Stream struct {
	rt   *runtime.Stream
	sess *Session
	cur  Event
}

// Next 阻塞读下一个事件,返回 false 表示流已结束。
func (s *Stream) Next() bool {
	if !s.rt.Next() {
		return false
	}
	s.cur = toNativeEvent(s.rt.Event())
	return true
}

// Event 返回当前事件。
func (s *Stream) Event() Event { return s.cur }

// Result 在流结束后返回聚合结果。
func (s *Stream) Result() (*Result, error) {
	res, err := s.rt.Result()
	if err != nil {
		return nil, err
	}
	return toNativeResult(res), nil
}

// Close 主动结束流。
func (s *Stream) Close(ctx context.Context) error { return s.rt.Close(ctx) }

// SessionID 返回 backend 报上来的 session id。
func (s *Stream) SessionID() string { return s.rt.SessionID() }

// State 返回流当前观察到的 State 快照。
func (s *Stream) State() State {
	st := s.rt.State()
	return State{ThreadID: st.ThreadID, Values: cloneValues(st.Values)}
}

// Steer 在流进行中插入一条 user 消息。
func (s *Stream) Steer(ctx context.Context, text string) error {
	return s.rt.Steer(ctx, text)
}

// FollowUp 把下一轮 user 消息排队。
func (s *Stream) FollowUp(ctx context.Context, text string) error {
	return s.rt.FollowUp(ctx, text)
}
