package agent_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	agent "github.com/cago-frame/agents/agent"
	"github.com/cago-frame/agents/provider"
	"github.com/cago-frame/agents/provider/providertest"
)

func TestRunner_TextOnlyTurn(t *testing.T) {
	prov := providertest.New().QueueStream(
		provider.StreamChunk{ContentDelta: "Hello"},
		provider.StreamChunk{ContentDelta: " world"},
		provider.StreamChunk{ContentDelta: "!"},
		provider.StreamChunk{FinishReason: provider.FinishStop},
	)
	a := agent.New(prov)
	conv := agent.NewConversation()
	r := a.Runner(conv)
	defer func() { _ = r.Close() }()

	events, err := r.Send(context.Background(), "say hi")
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	var deltas []string
	var sawTurnEnd, sawDone bool
	for ev := range events {
		switch ev.Kind {
		case agent.EventTextDelta:
			deltas = append(deltas, ev.Delta)
		case agent.EventTurnEnd:
			sawTurnEnd = true
		case agent.EventDone:
			sawDone = true
		}
	}
	if got := strings.Join(deltas, ""); got != "Hello world!" {
		t.Fatalf("text = %q", got)
	}
	if !sawTurnEnd || !sawDone {
		t.Fatalf("missing TurnEnd or Done")
	}
	// Conversation must have user + assistant
	if conv.Len() != 2 {
		t.Fatalf("conv len = %d, want 2", conv.Len())
	}
	last, _ := conv.MessageAt(1)
	if last.Role != agent.RoleAssistant {
		t.Fatalf("role mismatch")
	}
	if last.PartialReason != agent.PartialNone {
		t.Fatalf("PartialReason should be empty (finalized), got %q", last.PartialReason)
	}
	if got := textOfBlocks(last.Content); got != "Hello world!" {
		t.Fatalf("assistant text = %q", got)
	}
}

func textOfBlocks(blocks []agent.ContentBlock) string {
	var s strings.Builder
	for _, b := range blocks {
		switch v := b.(type) {
		case agent.TextBlock:
			s.WriteString(v.Text)
		case agent.SummaryBlock:
			s.WriteString(v.Text)
		case agent.DisplayTextBlock:
			s.WriteString(v.Text)
		}
	}
	return s.String()
}

func thinkingOfBlocks(blocks []agent.ContentBlock) string {
	var s strings.Builder
	for _, b := range blocks {
		if t, ok := b.(agent.ThinkingBlock); ok {
			s.WriteString(t.Text)
		}
	}
	return s.String()
}

// TestRunner_ThinkingDelta_AccumulatesIntoPartial verifies that streaming
// thinking deltas are mirrored into the partial assistant message as a
// ThinkingBlock and that EventThinkingDelta is emitted per non-empty chunk.
func TestRunner_ThinkingDelta_AccumulatesIntoPartial(t *testing.T) {
	prov := providertest.New().QueueStream(
		provider.StreamChunk{ThinkingDelta: &provider.ThinkingDelta{Text: "let me "}},
		provider.StreamChunk{ThinkingDelta: &provider.ThinkingDelta{Text: "think..."}},
		provider.StreamChunk{ThinkingDelta: &provider.ThinkingDelta{Signature: "sig-final"}},
		provider.StreamChunk{ContentDelta: "answer"},
		provider.StreamChunk{FinishReason: provider.FinishStop},
	)
	a := agent.New(prov)
	conv := agent.NewConversation()
	r := a.Runner(conv)
	defer func() { _ = r.Close() }()

	events, _ := r.Send(context.Background(), "go")
	var thinkingDeltas []string
	for ev := range events {
		if ev.Kind == agent.EventThinkingDelta {
			thinkingDeltas = append(thinkingDeltas, ev.Delta)
		}
	}
	if got := strings.Join(thinkingDeltas, ""); got != "let me think..." {
		t.Fatalf("EventThinkingDelta concat = %q", got)
	}

	last, _ := conv.MessageAt(conv.Len() - 1)
	if got := thinkingOfBlocks(last.Content); got != "let me think..." {
		t.Fatalf("thinking in partial = %q", got)
	}
	// Signature should land on the same ThinkingBlock.
	var sig string
	for _, b := range last.Content {
		if tb, ok := b.(agent.ThinkingBlock); ok {
			sig = tb.Signature
		}
	}
	if sig != "sig-final" {
		t.Fatalf("thinking signature = %q, want sig-final", sig)
	}
	// Block ordering: thinking before text.
	if _, ok := last.Content[0].(agent.ThinkingBlock); !ok {
		t.Fatalf("first block should be thinking, got %T", last.Content[0])
	}
	if _, ok := last.Content[1].(agent.TextBlock); !ok {
		t.Fatalf("second block should be text, got %T", last.Content[1])
	}
}

// TestRunner_ToolCallDelta_MirrorsRawArgsIntoPartial verifies that tool-call
// streaming chunks land on the partial assistant message as ToolUseBlocks
// carrying RawArgs even before the JSON args are complete.
func TestRunner_ToolCallDelta_MirrorsRawArgsIntoPartial(t *testing.T) {
	prov := providertest.New().QueueStream(
		provider.StreamChunk{ToolCallDelta: &provider.ToolCallDelta{
			Index: 0, ID: "tu_x", Name: "Echo", ArgsDelta: `{"text":"hel`,
		}},
		provider.StreamChunk{ToolCallDelta: &provider.ToolCallDelta{
			Index: 0, ArgsDelta: `lo"}`,
		}},
		// No FinishReason — caller will Cancel to lock in the partial.
	)
	a := agent.New(prov)
	conv := agent.NewConversation()
	r := a.Runner(conv)
	defer func() { _ = r.Close() }()

	events, _ := r.Send(context.Background(), "go")
	cancelOnce := false
	argDeltas := 0
	for ev := range events {
		if ev.Kind == agent.EventToolDelta && !cancelOnce {
			argDeltas++
			if argDeltas == 2 {
				cancelOnce = true
				_ = r.Cancel("stop after args")
			}
		}
	}
	if argDeltas != 2 {
		t.Fatalf("args-stream EventToolDelta count = %d, want 2", argDeltas)
	}

	last, _ := conv.MessageAt(conv.Len() - 1)
	if last.PartialReason != agent.PartialCancelled {
		t.Fatalf("PartialReason = %q", last.PartialReason)
	}
	var found *agent.ToolUseBlock
	for _, b := range last.Content {
		if tu, ok := b.(agent.ToolUseBlock); ok {
			tu := tu
			found = &tu
			break
		}
	}
	if found == nil {
		t.Fatalf("partial missing ToolUseBlock; content=%+v", last.Content)
	}
	if found.ID != "tu_x" || found.Name != "Echo" {
		t.Fatalf("tool use id/name = %q/%q", found.ID, found.Name)
	}
	if found.RawArgs != `{"text":"hello"}` {
		t.Fatalf("RawArgs = %q", found.RawArgs)
	}
	// Input is intentionally nil because finalize never ran. The explicit
	// State field is the canonical discriminator going forward — earlier
	// callers reading Input == nil should migrate to checking State.
	if found.Input != nil {
		t.Fatalf("Input should be nil for canceled mid-stream tool use, got %v", found.Input)
	}
	if found.State != agent.ToolUseStreaming {
		t.Fatalf("State = %v, want ToolUseStreaming for mid-stream cancel", found.State)
	}
}

// TestRunner_FinishLength_MapsToTokenLimit verifies FinishLength becomes
// StopTokenLimit + PartialTokenLimit so UI can render a "continue" affordance.
func TestRunner_FinishLength_MapsToTokenLimit(t *testing.T) {
	prov := providertest.New().QueueStream(
		provider.StreamChunk{ContentDelta: "truncated answer"},
		provider.StreamChunk{FinishReason: provider.FinishLength},
	)
	a := agent.New(prov)
	conv := agent.NewConversation()
	r := a.Runner(conv)
	defer func() { _ = r.Close() }()

	events, _ := r.Send(context.Background(), "go")
	var teStop agent.StopReason
	for ev := range events {
		if ev.Kind == agent.EventTurnEnd {
			teStop = ev.StopReason
		}
	}
	if teStop != agent.StopTokenLimit {
		t.Fatalf("EventTurnEnd.StopReason = %v, want StopTokenLimit", teStop)
	}
	last, _ := conv.MessageAt(conv.Len() - 1)
	if last.PartialReason != agent.PartialTokenLimit {
		t.Fatalf("PartialReason = %q, want %q", last.PartialReason, agent.PartialTokenLimit)
	}
}

// TestRunner_DeadlineExceeded_MapsToTimeout verifies that a context deadline
// expiry is reported as StopTimeout / PartialTimeout, distinct from explicit
// Cancel.
func TestRunner_DeadlineExceeded_MapsToTimeout(t *testing.T) {
	prov := providertest.New().QueueStreamFunc(func(ctx context.Context) <-chan provider.StreamChunk {
		ch := make(chan provider.StreamChunk)
		go func() {
			defer close(ch)
			<-ctx.Done()
		}()
		return ch
	})
	a := agent.New(prov)
	conv := agent.NewConversation()
	r := a.Runner(conv)
	defer func() { _ = r.Close() }()

	deadline, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	events, _ := r.Send(deadline, "go")
	var teStop agent.StopReason
	for ev := range events {
		if ev.Kind == agent.EventTurnEnd {
			teStop = ev.StopReason
		}
	}
	if teStop != agent.StopTimeout {
		t.Fatalf("EventTurnEnd.StopReason = %v, want StopTimeout", teStop)
	}
	last, _ := conv.MessageAt(conv.Len() - 1)
	if last.PartialReason != agent.PartialTimeout {
		t.Fatalf("PartialReason = %q, want %q", last.PartialReason, agent.PartialTimeout)
	}
}

// TestRunner_Cancel_PreservesUsage verifies that a usage chunk arriving
// before Cancel is preserved on the partial.
func TestRunner_Cancel_PreservesUsage(t *testing.T) {
	prov := providertest.New().QueueStream(
		provider.StreamChunk{ContentDelta: "hello", Usage: &provider.Usage{PromptTokens: 10, CompletionTokens: 1, TotalTokens: 11}},
		// Block forever — caller will Cancel.
	)
	a := agent.New(prov)
	conv := agent.NewConversation()
	r := a.Runner(conv)
	defer func() { _ = r.Close() }()

	events, _ := r.Send(context.Background(), "go")
	for ev := range events {
		if ev.Kind == agent.EventTextDelta {
			_ = r.Cancel("stop")
		}
	}
	last, _ := conv.MessageAt(conv.Len() - 1)
	if last.Usage == nil {
		t.Fatalf("Usage should be preserved on canceled partial")
	}
	if last.Usage.TotalTokens != 11 {
		t.Fatalf("Usage.TotalTokens = %d, want 11", last.Usage.TotalTokens)
	}
}

// TestRunner_Steer_ClosedAfterTurnEnd verifies that once a turn fully exits,
// Steer returns ErrSteerNoActiveTurn and the queue cannot leak into the
// next Send.
func TestRunner_Steer_ClosedAfterTurnEnd(t *testing.T) {
	prov := providertest.New().
		QueueStream(
			provider.StreamChunk{ContentDelta: "answer"},
			provider.StreamChunk{FinishReason: provider.FinishStop},
		).
		QueueStream(
			provider.StreamChunk{ContentDelta: "next"},
			provider.StreamChunk{FinishReason: provider.FinishStop},
		)
	a := agent.New(prov)
	conv := agent.NewConversation()
	r := a.Runner(conv)
	defer func() { _ = r.Close() }()

	events, _ := r.Send(context.Background(), "go")
	for range events {
	}
	if err := r.Steer(context.Background(), "leaked"); err == nil {
		t.Fatalf("Steer after turn end should have returned ErrSteerNoActiveTurn")
	}
	events2, _ := r.Send(context.Background(), "again")
	for range events2 {
	}
	turn2 := prov.Received()[1]
	for _, m := range turn2.Messages {
		if m.Role == provider.RoleUser && m.Content == "leaked" {
			t.Fatalf("rejected steer leaked into turn-2 prompt: %+v", turn2.Messages)
		}
	}
}

// TestRunner_Steer_AutoContinuesAfterNoToolStop verifies that a Steer
// accepted while the LLM was streaming is honored even if the model finishes
// with FinishStop (no tool calls): the loop drains the queue and runs a new
// LLM call instead of orphaning the message.
func TestRunner_Steer_AutoContinuesAfterNoToolStop(t *testing.T) {
	prov := providertest.New().
		QueueStream(
			provider.StreamChunk{ContentDelta: "first"},
			provider.StreamChunk{FinishReason: provider.FinishStop},
		).
		QueueStream(
			provider.StreamChunk{ContentDelta: "second"},
			provider.StreamChunk{FinishReason: provider.FinishStop},
		)
	a := agent.New(prov)
	conv := agent.NewConversation()
	r := a.Runner(conv)
	defer func() { _ = r.Close() }()

	events, _ := r.Send(context.Background(), "go")
	steered := false
	for ev := range events {
		if ev.Kind == agent.EventTextDelta && !steered {
			steered = true
			if err := r.Steer(context.Background(), "be brief"); err != nil {
				t.Fatalf("Steer rejected mid-stream: %v", err)
			}
		}
	}
	// Provider received two requests in a single Send (auto-continuation).
	reqs := prov.Received()
	if len(reqs) != 2 {
		t.Fatalf("provider got %d requests, want 2 (auto-continuation)", len(reqs))
	}
	turn2 := reqs[1]
	foundSteer := false
	for _, m := range turn2.Messages {
		if m.Role == provider.RoleUser && m.Content == "be brief" {
			foundSteer = true
		}
	}
	if !foundSteer {
		t.Fatalf("steer 'be brief' missing from turn-2 prompt: %+v", turn2.Messages)
	}
}

// TestRunner_Retry_ResumesAfterTransientError verifies that a transient
// chunk.Err triggers EventRetry + sleep + a fresh ChatStream that includes
// the just-streamed text as historical context, so the model can continue
// where it left off.
func TestRunner_Retry_ResumesAfterTransientError(t *testing.T) {
	transient := &provider.ProviderError{StatusCode: 503, Err: errors.New("upstream busy")}
	prov := providertest.New().
		QueueStream(
			provider.StreamChunk{ContentDelta: "Once "},
			provider.StreamChunk{ContentDelta: "upon a"},
			provider.StreamChunk{Err: transient},
		).
		QueueStream(
			provider.StreamChunk{ContentDelta: " time."},
			provider.StreamChunk{FinishReason: provider.FinishStop},
		)
	a := agent.New(prov, agent.Retry(agent.RetryPolicy{
		MaxAttempts:  3,
		InitialDelay: time.Millisecond,
		MaxDelay:     time.Millisecond,
	}))
	conv := agent.NewConversation()
	r := a.Runner(conv)
	defer func() { _ = r.Close() }()

	events, _ := r.Send(context.Background(), "tell story")
	var (
		retryEvents []*agent.RetryEvent
		sawError    bool
		text        strings.Builder
	)
	for ev := range events {
		switch ev.Kind {
		case agent.EventTextDelta:
			text.WriteString(ev.Delta)
		case agent.EventRetry:
			retryEvents = append(retryEvents, ev.Retry)
		case agent.EventError:
			sawError = true
		}
	}
	if sawError {
		t.Fatalf("expected retry to recover, got fatal EventError")
	}
	if len(retryEvents) != 1 {
		t.Fatalf("expected 1 EventRetry, got %d", len(retryEvents))
	}
	if retryEvents[0].Attempt != 1 {
		t.Fatalf("Retry.Attempt = %d, want 1", retryEvents[0].Attempt)
	}
	if !errors.Is(retryEvents[0].Cause, transient.Err) {
		t.Fatalf("Retry.Cause = %v, want wrap of %v", retryEvents[0].Cause, transient.Err)
	}
	// Both attempts' text combine into a single user-visible answer because
	// the second prompt to the LLM included the errored partial.
	turn2 := prov.Received()[1]
	foundPartial := false
	for _, m := range turn2.Messages {
		if m.Role == provider.RoleAssistant && strings.Contains(m.Content, "Once upon a") {
			foundPartial = true
		}
	}
	if !foundPartial {
		t.Fatalf("turn-2 prompt missing the errored partial: %+v", turn2.Messages)
	}
}

// TestRunner_Retry_ExhaustedFallsBackToError verifies that once attempts are
// exhausted, the failure surfaces as EventError + EventTurnEnd(StopError).
func TestRunner_Retry_ExhaustedFallsBackToError(t *testing.T) {
	transient := &provider.ProviderError{StatusCode: 503, Err: errors.New("upstream busy")}
	prov := providertest.New().
		QueueStream(provider.StreamChunk{Err: transient}).
		QueueStream(provider.StreamChunk{Err: transient})
	a := agent.New(prov, agent.Retry(agent.RetryPolicy{
		MaxAttempts:  2,
		InitialDelay: time.Millisecond,
	}))
	conv := agent.NewConversation()
	r := a.Runner(conv)
	defer func() { _ = r.Close() }()

	events, _ := r.Send(context.Background(), "go")
	var retryCount int
	var fatal error
	var stop agent.StopReason
	for ev := range events {
		switch ev.Kind {
		case agent.EventRetry:
			retryCount++
		case agent.EventError:
			fatal = ev.Error
		case agent.EventTurnEnd:
			stop = ev.StopReason
		}
	}
	if retryCount != 1 {
		t.Fatalf("retry count = %d, want 1 (one retry between two attempts)", retryCount)
	}
	if fatal == nil {
		t.Fatalf("expected fatal EventError after exhaust")
	}
	if stop != agent.StopError {
		t.Fatalf("stop reason = %v, want StopError", stop)
	}
	// Errored partial must carry both PartialReason and PartialDetail so
	// downstream Stores/UIs can render the failure without dropping context.
	last, _ := conv.MessageAt(conv.Len() - 1)
	if last.PartialReason != agent.PartialErrored {
		t.Fatalf("PartialReason = %q, want PartialErrored", last.PartialReason)
	}
	if last.PartialDetail == "" {
		t.Fatalf("PartialDetail should carry the cause string, got empty")
	}
	if !strings.Contains(last.PartialDetail, "upstream busy") {
		t.Fatalf("PartialDetail = %q, want it to contain the cause", last.PartialDetail)
	}
}

// TestRunner_HookError_ObservableViaEventError covers the three hook stages
// surfacing failures as EventError rather than swallowing them silently.
func TestRunner_HookError_ObservableViaEventError(t *testing.T) {
	t.Run("user_prompt_submit", func(t *testing.T) {
		prov := providertest.New()
		boom := errors.New("ups deny")
		a := agent.New(prov, agent.UserPromptSubmit(func(_ context.Context, _ *agent.UserPromptInput) (*agent.UserPromptOutput, error) {
			return nil, boom
		}))
		r := a.Runner(agent.NewConversation())
		defer func() { _ = r.Close() }()
		events, _ := r.Send(context.Background(), "hi")
		var sawErr error
		for ev := range events {
			if ev.Kind == agent.EventError {
				sawErr = ev.Error
			}
		}
		var he *agent.HookError
		if !errors.As(sawErr, &he) || he.Stage != agent.HookStageUserPromptSubmit || !errors.Is(he.Cause, boom) {
			t.Fatalf("UserPromptSubmit hook err not surfaced: %v", sawErr)
		}
	})

	t.Run("turn_end", func(t *testing.T) {
		prov := providertest.New().QueueStream(
			provider.StreamChunk{ContentDelta: "x"},
			provider.StreamChunk{FinishReason: provider.FinishStop},
		)
		boom := errors.New("turn-end ups")
		a := agent.New(prov, agent.TurnEnd(func(_ context.Context, _ *agent.TurnEndInput) (*agent.TurnEndOutput, error) {
			return nil, boom
		}))
		r := a.Runner(agent.NewConversation())
		defer func() { _ = r.Close() }()
		events, _ := r.Send(context.Background(), "hi")
		var sawErr error
		for ev := range events {
			if ev.Kind == agent.EventError {
				sawErr = ev.Error
			}
		}
		var he *agent.HookError
		if !errors.As(sawErr, &he) || he.Stage != agent.HookStageTurnEnd || !errors.Is(he.Cause, boom) {
			t.Fatalf("TurnEnd hook err not surfaced: %v", sawErr)
		}
	})
}

// TestBuildRequest_SkipsUnfinalizedToolUse verifies that ToolUseBlocks not
// in ToolUseReady state (mid-stream cancel/error left them unfinalized) are
// dropped from the LLM request — they would otherwise become malformed
// tool_calls without paired tool_results.
func TestBuildRequest_SkipsUnfinalizedToolUse(t *testing.T) {
	msgs := []agent.Message{
		{Role: agent.RoleUser, Content: []agent.ContentBlock{agent.TextBlock{Text: "hi"}}},
		{Role: agent.RoleAssistant, Content: []agent.ContentBlock{
			agent.TextBlock{Text: "I will call a tool"},
			agent.ToolUseBlock{ID: "tu_x", Name: "Echo", RawArgs: `{"text":"hel`, State: agent.ToolUseStreaming},
		}, PartialReason: agent.PartialCancelled},
	}
	req := agent.BuildRequest(agent.RequestSpec{Messages: msgs})
	if len(req.Messages) != 2 {
		t.Fatalf("messages = %d, want 2", len(req.Messages))
	}
	if got := req.Messages[1].Content; got != "I will call a tool" {
		t.Fatalf("text content = %q", got)
	}
	if len(req.Messages[1].ToolCalls) != 0 {
		t.Fatalf("unfinalized tool_use should be dropped, got %d ToolCalls", len(req.Messages[1].ToolCalls))
	}
}
