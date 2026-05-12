// Package providertest 为 provider.Provider 的消费方提供可脚本化的测试用实现。
package providertest

import (
	"context"
	"encoding/json"
	"errors"
	"sync"

	"github.com/cago-frame/agents/provider"
)

// Mock 实现 provider.Provider。通过 Queue / QueueStream 预置响应。
// 每次 ChatCompletion 或 ChatStream 消费队列头部一个 entry。
type Mock struct {
	mu       sync.Mutex
	calls    []entry
	received []*provider.CompletionRequest
	name     string
}

type entry struct {
	resp       *provider.CompletionResponse
	chunks     []provider.StreamChunk
	streamFunc func(context.Context) <-chan provider.StreamChunk
	err        error
}

// New 创建一个空的 Mock。
func New() *Mock { return &Mock{name: "mock"} }

// Name 返回 provider 名称。
func (m *Mock) Name() string { return m.name }

// Queue 预置一次 ChatCompletion 的响应。
func (m *Mock) Queue(resp *provider.CompletionResponse) *Mock {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls = append(m.calls, entry{resp: resp})
	return m
}

// QueueCompletion 是 Queue 的别名，语义对应 ChatCompletion（非流式）。
// 与 QueueStream 对称便于测试代码可读。
func (m *Mock) QueueCompletion(resp *provider.CompletionResponse) *Mock {
	return m.Queue(resp)
}

// QueueStream 预置一次 ChatStream 的 chunks（按顺序推送后关闭通道）。
func (m *Mock) QueueStream(chunks ...provider.StreamChunk) *Mock {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls = append(m.calls, entry{chunks: chunks})
	return m
}

// QueueError 预置一次调用直接返回 error。
func (m *Mock) QueueError(err error) *Mock {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls = append(m.calls, entry{err: err})
	return m
}

// QueueStreamFunc 预置一次 ChatStream 调用，通过回调函数返回 channel，
// 方便测试控制流（阻塞、按需发送、ctx 感知等）。
func (m *Mock) QueueStreamFunc(fn func(context.Context) <-chan provider.StreamChunk) *Mock {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls = append(m.calls, entry{streamFunc: fn})
	return m
}

// Received 返回迄今为止收到的所有请求（按时间顺序）。
func (m *Mock) Received() []*provider.CompletionRequest {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]*provider.CompletionRequest, len(m.received))
	copy(out, m.received)
	return out
}

func (m *Mock) pop() (entry, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.calls) == 0 {
		return entry{}, errors.New("providertest: no queued response")
	}
	e := m.calls[0]
	m.calls = m.calls[1:]
	return e, nil
}

// ChatCompletion 实现 provider.Provider。
func (m *Mock) ChatCompletion(ctx context.Context, req *provider.CompletionRequest) (*provider.CompletionResponse, error) {
	m.mu.Lock()
	m.received = append(m.received, cloneRequest(req))
	m.mu.Unlock()
	e, err := m.pop()
	if err != nil {
		return nil, err
	}
	if e.err != nil {
		return nil, e.err
	}
	if e.resp == nil {
		return nil, errors.New("providertest: queued entry has no resp (did you QueueStream for a ChatCompletion call?)")
	}
	return e.resp, nil
}

// ChatStream 实现 provider.Provider。
func (m *Mock) ChatStream(ctx context.Context, req *provider.CompletionRequest) (<-chan provider.StreamChunk, error) {
	m.mu.Lock()
	m.received = append(m.received, cloneRequest(req))
	m.mu.Unlock()
	e, err := m.pop()
	if err != nil {
		return nil, err
	}
	if e.err != nil {
		return nil, e.err
	}
	if e.streamFunc != nil {
		return e.streamFunc(ctx), nil
	}
	ch := make(chan provider.StreamChunk, len(e.chunks)+1)
	go func() {
		defer close(ch)
		for _, c := range e.chunks {
			select {
			case ch <- c:
			case <-ctx.Done():
				return
			}
		}
	}()
	return ch, nil
}

// cloneRequest 深拷贝请求中所有调用者可能在请求返回后再 mutate 的字段，
// 让 Received() 反映调用瞬间的快照，使测试断言确定性。
func cloneRequest(req *provider.CompletionRequest) *provider.CompletionRequest {
	if req == nil {
		return nil
	}
	cp := *req
	if req.Messages != nil {
		cp.Messages = make([]provider.Message, len(req.Messages))
		for i, m := range req.Messages {
			cm := m
			if m.Thinking != nil {
				cm.Thinking = append([]provider.ThinkingBlock(nil), m.Thinking...)
			}
			if m.ToolCalls != nil {
				cm.ToolCalls = append([]provider.ToolCall(nil), m.ToolCalls...)
			}
			cp.Messages[i] = cm
		}
	}
	if req.Tools != nil {
		cp.Tools = make([]provider.Tool, len(req.Tools))
		for i, t := range req.Tools {
			ct := t
			if t.Function != nil {
				fn := *t.Function
				if t.Function.Parameters != nil {
					fn.Parameters = append(json.RawMessage(nil), t.Function.Parameters...)
				}
				ct.Function = &fn
			}
			cp.Tools[i] = ct
		}
	}
	if req.ToolChoice != nil {
		tc := *req.ToolChoice
		cp.ToolChoice = &tc
	}
	if req.Temperature != nil {
		v := *req.Temperature
		cp.Temperature = &v
	}
	if req.TopP != nil {
		v := *req.TopP
		cp.TopP = &v
	}
	if req.MaxTokens != nil {
		v := *req.MaxTokens
		cp.MaxTokens = &v
	}
	if req.StopSequences != nil {
		cp.StopSequences = append([]string(nil), req.StopSequences...)
	}
	if req.Seed != nil {
		v := *req.Seed
		cp.Seed = &v
	}
	if req.ResponseFormat != nil {
		rf := *req.ResponseFormat
		if req.ResponseFormat.Schema != nil {
			s := *req.ResponseFormat.Schema
			if req.ResponseFormat.Schema.Schema != nil {
				s.Schema = append(json.RawMessage(nil), req.ResponseFormat.Schema.Schema...)
			}
			rf.Schema = &s
		}
		cp.ResponseFormat = &rf
	}
	if req.Thinking != nil {
		v := *req.Thinking
		cp.Thinking = &v
	}
	return &cp
}
