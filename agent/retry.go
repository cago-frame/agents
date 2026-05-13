package agent

import (
	"context"
	"errors"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/cago-frame/agents/provider"
)

// RetryPolicy configures automatic retry of transient ChatStream failures.
// When this option is not set, the runner does not retry — a single failure
// surfaces as EventError + EventTurnEnd as before.
//
// On a retryable failure the runner:
//  1. Finalizes the in-progress partial assistant message as PartialErrored
//     (preserving any text / thinking / RawArgs already streamed plus the
//     latest token usage), so the next ChatStream sees the streamed bytes
//     as historical context per BuildRequest's default strip behavior and
//     the model can continue from where it left off.
//  2. Emits one EventRetry frame carrying attempt count, backoff, and the
//     underlying error so UIs can render a "retrying…" indicator.
//  3. Sleeps the computed backoff (honoring ProviderError.RetryAfter when
//     non-empty) and re-issues ChatStream.
//
// When attempts are exhausted, the final failure surfaces as EventError +
// EventTurnEnd(StopError) just like the no-retry case.
type RetryPolicy struct {
	// MaxAttempts caps the number of ChatStream calls per step. A value of
	// 0 or 1 disables retry (single attempt, no retries). 3 means up to
	// 2 retries after the first failure.
	MaxAttempts int
	// InitialDelay is the backoff before the first retry. Subsequent
	// attempts double this up to MaxDelay (when BackoffFn is nil).
	InitialDelay time.Duration
	// MaxDelay caps the exponential backoff. Ignored when BackoffFn is set.
	MaxDelay time.Duration
	// BackoffFn, when non-nil, computes the delay before the next attempt
	// given the 1-indexed failure count and the underlying error. Honored
	// in preference to the default exponential schedule.
	BackoffFn func(attempt int, err error) time.Duration
	// ShouldRetry, when non-nil, decides whether an error qualifies for
	// retry. The default classifier treats *provider.ProviderError with
	// status 408/425/429/500/502/503/504 plus net.Error{Timeout} and
	// generic "connection reset" / "EOF" errors as retryable.
	ShouldRetry func(err error) bool
}

// Retry installs an automatic retry policy for the agent. See RetryPolicy.
func Retry(policy RetryPolicy) Option {
	return func(c *agentConfig) {
		c.retryPolicy = &policy
	}
}

// handleRetry finalizes the in-progress partial as PartialErrored (preserving
// streamed text / thinking / RawArgs + last usage), emits an EventRetry frame
// with the attempt count and backoff, then sleeps the delay. Returns false if
// the consumer dropped the iter (caller should return immediately).
func (r *Runner) handleRetry(ctx context.Context, turnID string, attempt int, delay time.Duration, cause error, usage *provider.Usage, yield func(Event) bool) bool {
	r.mu.Lock()
	idx := r.partialIndex
	r.partialIndex = -1
	r.mu.Unlock()
	if idx >= 0 {
		detail := ""
		if cause != nil {
			detail = cause.Error()
		}
		r.conv.finalizePartial(idx, PartialErrored, detail, usage)
	}
	ev := Event{
		Kind:   EventRetry,
		TurnID: turnID,
		Error:  cause,
		Retry: &RetryEvent{
			Attempt: attempt,
			Delay:   delay,
			Cause:   cause,
		},
	}
	r.dispatcher.emit(ctx, ev)
	if !yield(ev) {
		return false
	}
	if delay > 0 {
		t := time.NewTimer(delay)
		defer t.Stop()
		select {
		case <-ctx.Done():
			return false
		case <-t.C:
		}
	}
	return true
}

// shouldRetry decides whether the err is retryable per the policy and how
// long to wait before the next attempt. The 1-indexed `attempt` is the
// number of failures so far (so attempt=1 means the first attempt failed,
// returning true means a 2nd attempt will run after the returned delay).
func (r *Runner) shouldRetry(err error, attempt int) (time.Duration, bool) {
	p := r.agent.cfg.retryPolicy
	if p == nil || p.MaxAttempts <= 1 {
		return 0, false
	}
	if attempt >= p.MaxAttempts {
		return 0, false
	}
	classifier := p.ShouldRetry
	if classifier == nil {
		classifier = defaultShouldRetry
	}
	if !classifier(err) {
		return 0, false
	}
	// Honor explicit Retry-After hint if the provider gave one.
	if d, ok := retryAfterHint(err); ok {
		return d, true
	}
	if p.BackoffFn != nil {
		return p.BackoffFn(attempt, err), true
	}
	return defaultBackoff(p.InitialDelay, p.MaxDelay, attempt), true
}

func defaultBackoff(initial, maxDelay time.Duration, attempt int) time.Duration {
	if initial <= 0 {
		initial = 500 * time.Millisecond
	}
	if maxDelay <= 0 {
		maxDelay = 30 * time.Second
	}
	d := initial
	for i := 1; i < attempt; i++ {
		d *= 2
		if d >= maxDelay {
			return maxDelay
		}
	}
	return d
}

func retryAfterHint(err error) (time.Duration, bool) {
	var pe *provider.ProviderError
	if !errors.As(err, &pe) || pe.RetryAfter == "" {
		return 0, false
	}
	// RetryAfter is either a non-negative integer (seconds) or an HTTP-date.
	// We support seconds — HTTP-date is rare and not worth the dependency.
	secs, parseErr := strconv.Atoi(pe.RetryAfter)
	if parseErr != nil || secs < 0 {
		return 0, false
	}
	return time.Duration(secs) * time.Second, true
}

func defaultShouldRetry(err error) bool {
	if err == nil {
		return false
	}
	var pe *provider.ProviderError
	if errors.As(err, &pe) {
		switch pe.StatusCode {
		case 408, 425, 429, 500, 502, 503, 504:
			return true
		}
	}
	var ne net.Error
	if errors.As(err, &ne) && ne.Timeout() {
		return true
	}
	msg := strings.ToLower(err.Error())
	if msg == "eof" {
		return true
	}
	// 服务端中途硬断 / SSE 流被截断时，provider 应该 emit io.ErrUnexpectedEOF
	// （openai provider 在 finish_reason 之前收到 io.EOF 时走这条路径）。
	// "unexpected eof" 作为可重试错误识别，避免被静默当作正常流结束。
	for _, sub := range []string{"connection reset", "broken pipe", "i/o timeout", "tls handshake timeout", "unexpected eof"} {
		if strings.Contains(msg, sub) {
			return true
		}
	}
	return false
}
