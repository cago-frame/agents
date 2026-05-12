package claudecode

import (
	"context"

	"github.com/cago-frame/agents/cliagent/internal/runtime"
)

// Session 是 claudecode 暴露的会话句柄。包装 runtime.Session,事件 / Result 走
// claudecode native 类型。
//
// 不支持并发 Send/Stream/Text;Steer / FollowUp 可跨 goroutine 调用。
type Session struct {
	rt     *runtime.Session
	runner *Runner
}

// ID 返回 session 唯一标识符。
func (s *Session) ID() string { return s.rt.ID() }

// History 返回当前历史消息拷贝。
func (s *Session) History() []Message {
	return toNativeMessages(s.rt.History())
}

// State 返回当前 State 克隆。
func (s *Session) State() State {
	st := s.rt.State()
	return State{ThreadID: st.ThreadID, Values: cloneStringValues(st.Values)}
}

// Clear 清空历史消息、state、follow-up 队列。
func (s *Session) Clear(ctx context.Context) error { return s.rt.Clear(ctx) }

// Close 关闭 session。
func (s *Session) Close(ctx context.Context) error { return s.rt.Close(ctx) }

// Stream 启动一次 run。
func (s *Session) Stream(ctx context.Context, prompt string, opts ...RunOption) (*Stream, error) {
	var roc runOptCfg
	for _, o := range opts {
		o(&roc)
	}
	rstream, err := s.rt.AcquireStream(s.runner.cfg.eventBuffer)
	if err != nil {
		return nil, err
	}
	if err := s.runner.pm.startTurn(ctx, s.rt, rstream, prompt, roc); err != nil {
		s.rt.ReleaseStream(rstream)
		return nil, err
	}
	return &Stream{rt: rstream}, nil
}

// Send 启动 run 并阻塞至完成，返回聚合 Result。
func (s *Session) Send(ctx context.Context, prompt string, opts ...RunOption) (*Result, error) {
	st, err := s.Stream(ctx, prompt, opts...)
	if err != nil {
		return nil, err
	}
	for st.Next() {
		// drain events
	}
	return st.Result()
}

// Text 启动 run，只返回最终文本。
func (s *Session) Text(ctx context.Context, prompt string, opts ...RunOption) (string, error) {
	res, err := s.Send(ctx, prompt, opts...)
	if err != nil {
		return "", err
	}
	if res == nil {
		return "", nil
	}
	return res.Text, nil
}

// Steer 在流进行中插入 user 消息。
func (s *Session) Steer(ctx context.Context, text string) error {
	active := s.rt.ActiveStream()
	if active == nil {
		return runtime.ErrNoActiveStream
	}
	return active.Steer(ctx, text)
}

// FollowUp 把下一轮 user 消息排队。
func (s *Session) FollowUp(_ context.Context, text string) error {
	return s.rt.EnqueueFollowUp(text)
}
