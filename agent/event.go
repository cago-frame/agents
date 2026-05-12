package agent

import "time"

type EventKind int

const (
	EventTextDelta EventKind = iota
	EventThinkingDelta
	EventMessageEnd
	EventPreToolUse
	EventPostToolUse
	EventToolDelta
	EventTurnEnd
	EventError
	EventCancelled
	EventCompacted
	EventDone
	// EventRetry signals a transient ChatStream failure that the framework
	// will retry. Distinct from EventError (which is fatal) so UI can render
	// an in-progress "retrying…" indicator. Always carries Event.Retry.
	EventRetry
	// EventSteerConsumed signals that a queued Steer message has been
	// appended to the conversation and will be visible to the LLM on the
	// next request. Emitted once per consumed entry, in queue order, at the
	// drain points inside runTurn. Carries the user-visible text in Delta
	// (displayText if WithSteerDisplay was used, else the LLM text). UIs
	// use this to finalize the current assistant bubble, insert the user
	// turn, and open a fresh assistant stream — without it, queued messages
	// silently bleed into the next LLM call and the UI can't tell the model
	// reacted to them.
	EventSteerConsumed
)

func (k EventKind) String() string {
	switch k {
	case EventTextDelta:
		return "text_delta"
	case EventThinkingDelta:
		return "thinking_delta"
	case EventMessageEnd:
		return "message_end"
	case EventPreToolUse:
		return "pre_tool_use"
	case EventPostToolUse:
		return "post_tool_use"
	case EventToolDelta:
		return "tool_delta"
	case EventTurnEnd:
		return "turn_end"
	case EventError:
		return "error"
	case EventCancelled:
		return "canceled"
	case EventCompacted:
		return "compacted"
	case EventDone:
		return "done"
	case EventRetry:
		return "retry"
	case EventSteerConsumed:
		return "steer_consumed"
	}
	return "unknown"
}

type StopReason int

const (
	StopEndTurn StopReason = iota
	StopMaxSteps
	StopHook
	StopError
	StopCancelled
	// StopTokenLimit means the LLM hit its output token cap (FinishLength).
	// The streamed content is intact and can be replayed for continuation.
	StopTokenLimit
	// StopTimeout means the active context deadline expired. Distinct from
	// StopCancelled (explicit Runner.Cancel / parent context.Cancel) so UIs
	// can show "timed out" vs "stopped by user".
	StopTimeout
)

func (s StopReason) String() string {
	switch s {
	case StopEndTurn:
		return "end_turn"
	case StopMaxSteps:
		return "max_steps"
	case StopHook:
		return "hook"
	case StopError:
		return "error"
	case StopCancelled:
		return "canceled"
	case StopTokenLimit:
		return "token_limit"
	case StopTimeout:
		return "timeout"
	}
	return "unknown"
}

type ToolEvent struct {
	ToolUseID string
	Name      string
	Input     map[string]any
	Output    *ToolResultBlock
}

// RetryEvent describes a transient failure that triggered a framework retry.
// Carried on Event.Retry whenever Event.Kind == EventRetry.
type RetryEvent struct {
	// Attempt is 1-indexed: the attempt number that just failed (so the
	// next attempt about to start is Attempt+1). UIs render this as
	// "retrying X of MaxAttempts".
	Attempt int
	// Delay is the backoff before the next attempt. UIs can use it for a
	// countdown indicator.
	Delay time.Duration
	// Cause is the underlying error that triggered the retry. Same instance
	// is also reflected on Event.Error for callers using a single switch.
	Cause error
}

type Event struct {
	Kind       EventKind
	TurnID     string
	Delta      string
	Tool       *ToolEvent
	Usage      *Usage
	Error      error
	StopReason StopReason
	// PartialReason is free-form text describing why a turn ended (e.g. the
	// argument passed to Runner.Cancel, or a hook's deny reason). It is
	// *not* the [PartialState] enum used on [Message.PartialReason]; that
	// is the state machine. Both fields share a name for historical
	// reasons but encode different concepts: the state vs the cause.
	PartialReason string
	// Retry is non-nil when Kind == EventRetry. See RetryEvent.
	Retry *RetryEvent
}
