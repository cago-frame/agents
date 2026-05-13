package agent

import (
	"errors"
	"io"
	"testing"

	"github.com/cago-frame/agents/provider"
)

func TestDefaultShouldRetry(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"nil 不重试", nil, false},
		{"普通错误不重试", errors.New("bad input"), false},
		{"ProviderError 503 重试", &provider.ProviderError{Err: errors.New("x"), StatusCode: 503}, true},
		{"ProviderError 429 重试", &provider.ProviderError{Err: errors.New("x"), StatusCode: 429}, true},
		{"ProviderError 408 重试", &provider.ProviderError{Err: errors.New("x"), StatusCode: 408}, true},
		{"ProviderError 400 不重试", &provider.ProviderError{Err: errors.New("x"), StatusCode: 400}, false},
		{"ProviderError 401 不重试", &provider.ProviderError{Err: errors.New("x"), StatusCode: 401}, false},
		{"io.EOF 重试", io.EOF, true},
		// 回归 issue: 服务端在 finish_reason 之前硬断时 openai provider emit
		// io.ErrUnexpectedEOF —— defaultShouldRetry 的字符串 fallback 必须识别。
		{"io.ErrUnexpectedEOF 重试", io.ErrUnexpectedEOF, true},
		{"connection reset 重试", errors.New("read tcp: connection reset by peer"), true},
		{"broken pipe 重试", errors.New("write: broken pipe"), true},
		{"i/o timeout 重试", errors.New("dial tcp: i/o timeout"), true},
		{"tls handshake timeout 重试", errors.New("net/http: TLS handshake timeout"), true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := defaultShouldRetry(tc.err)
			if got != tc.want {
				t.Fatalf("defaultShouldRetry(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}
