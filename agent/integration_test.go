package agent_test

import (
	"context"
	"encoding/json"
	"sync"
	"testing"

	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"

	agent "github.com/cago-frame/agents/agent"
	"github.com/cago-frame/agents/agent/approve"
	"github.com/cago-frame/agents/agent/compactor"
	logplugin "github.com/cago-frame/agents/agent/observe/log"
	"github.com/cago-frame/agents/provider"
	"github.com/cago-frame/agents/provider/providertest"
)

// End-to-end: text -> tool -> text, with Watch + OnEvent both observed.
func TestIntegration_FullTurnWithTool(t *testing.T) {
	args, _ := json.Marshal(map[string]any{"text": "data"})
	prov := providertest.New().
		QueueStream(
			provider.StreamChunk{ContentDelta: "I'll fetch."},
			provider.StreamChunk{ToolCallDelta: &provider.ToolCallDelta{Index: 0, ID: "tu1", Name: "Echo", ArgsDelta: string(args)}},
			provider.StreamChunk{FinishReason: provider.FinishToolCalls},
		).
		QueueStream(
			provider.StreamChunk{ContentDelta: "Got it: "},
			provider.StreamChunk{ContentDelta: "data"},
			provider.StreamChunk{FinishReason: provider.FinishStop},
		)

	var onEvts []agent.EventKind

	conv := agent.NewConversation()

	// watchChanges is collected under watchMu by the watcher goroutine.
	// watchReadyC is closed once the watcher has collected at least minChanges items.
	const minChanges = 4
	var (
		watchMu      sync.Mutex
		watchChanges []agent.Change
	)
	watchReadyC := make(chan struct{})
	var watchReadyOnce sync.Once

	// Register the watch subscription synchronously on the main goroutine so
	// broadcasts from r.Wait below cannot race with goroutine scheduling and
	// be missed before the sub registers.
	watchSeq := conv.Watch()
	go func() {
		for ch := range watchSeq {
			watchMu.Lock()
			watchChanges = append(watchChanges, ch)
			n := len(watchChanges)
			watchMu.Unlock()
			if n >= minChanges {
				watchReadyOnce.Do(func() { close(watchReadyC) })
			}
		}
	}()

	a := agent.New(prov,
		agent.Tools(echoTool{}),
		// OnEvent callbacks run synchronously in the loop goroutine before the
		// loop emits events into the ring buffer; by the time r.Wait returns
		// all callbacks have completed, so no mutex is needed here.
		agent.OnEvent(agent.AnyEvent, func(_ context.Context, ev agent.Event) {
			onEvts = append(onEvts, ev.Kind)
		}),
	)

	r := a.Runner(conv)
	defer func() { _ = r.Close() }()

	if err := r.Wait(context.Background(), "fetch please"); err != nil {
		t.Fatalf("wait: %v", err)
	}

	// Wait for the watcher goroutine to collect at least minChanges items.
	// All broadcasts happened before r.Wait returned, so the channel is already
	// buffered with all items; the goroutine just needs to be scheduled.
	<-watchReadyC

	// Conv should have: user, assistant(text+toolUse), tool(result), assistant(text)
	if got := conv.Len(); got != 4 {
		t.Fatalf("conv len = %d, want 4", got)
	}
	last, _ := conv.MessageAt(3)
	if textOfBlocks(last.Content) != "Got it: data" {
		t.Fatalf("final text = %q", textOfBlocks(last.Content))
	}

	// Watch saw at least 4 changes: Appended(user), Appended/Finalized(partial
	// assistant), Appended(tool result), Appended/Finalized(partial assistant).
	// Total is typically 6 (2 Finalized + 4 Appended).
	watchMu.Lock()
	nChanges := len(watchChanges)
	watchMu.Unlock()
	if nChanges < minChanges {
		t.Fatalf("expected at least %d watch changes, got %d", minChanges, nChanges)
	}

	// OnEvent saw at least one PreToolUse + PostToolUse + TurnEnd + Done.
	// (onEvts is safe to read without a mutex here because all OnEvent callbacks
	// run synchronously within the loop goroutine before r.Wait returns.)
	seen := map[agent.EventKind]bool{}
	for _, k := range onEvts {
		seen[k] = true
	}
	for _, k := range []agent.EventKind{agent.EventPreToolUse, agent.EventPostToolUse, agent.EventTurnEnd, agent.EventDone} {
		if !seen[k] {
			t.Fatalf("missing event kind %v", k)
		}
	}
}

// TestE2E_Phase2a_ParallelStreamingMultimodal exercises 2a's three additions
// in one turn:
//   - Two parallel non-serial tool calls (echoTool x 2) with stable Pre/Post ordering
//   - One streaming tool yielding multiple deltas
//   - An ImageBlock seeded into the conversation flowing into provider.MultiContent
func TestE2E_Phase2a_ParallelStreamingMultimodal(t *testing.T) {
	t.Parallel()

	args1, _ := json.Marshal(map[string]any{"text": "first"})
	args2, _ := json.Marshal(map[string]any{"text": "second"})
	args3, _ := json.Marshal(map[string]any{})

	prov := providertest.New().
		// Round 1: two parallel echoes + one streaming echo
		QueueStream(
			provider.StreamChunk{ToolCallDelta: &provider.ToolCallDelta{
				Index: 0, ID: "tu_1", Name: "Echo", ArgsDelta: string(args1),
			}},
			provider.StreamChunk{ToolCallDelta: &provider.ToolCallDelta{
				Index: 1, ID: "tu_2", Name: "Echo", ArgsDelta: string(args2),
			}},
			provider.StreamChunk{ToolCallDelta: &provider.ToolCallDelta{
				Index: 2, ID: "tu_3", Name: "StreamEcho", ArgsDelta: string(args3),
			}},
			provider.StreamChunk{FinishReason: provider.FinishToolCalls},
		).
		// Round 2: assistant final answer
		QueueStream(
			provider.StreamChunk{ContentDelta: "all done"},
			provider.StreamChunk{FinishReason: provider.FinishStop},
		)

	stream := &streamingEchoTool{name: "StreamEcho", parts: []string{"a", "b"}}
	a := agent.New(prov, agent.Tools(echoTool{}, stream))
	conv := agent.NewConversation()
	// Seed conv with an ImageBlock-bearing user message to verify multimodal
	// plumbing through Send.
	conv.Append(agent.Message{Role: agent.RoleUser, Content: []agent.ContentBlock{
		agent.TextBlock{Text: "look at this:"},
		agent.ImageBlock{MediaType: "image/png", Source: agent.BlobSource{URL: "https://example.com/x.png"}},
	}})
	r := a.Runner(conv)
	defer func() { _ = r.Close() }()

	events, err := r.Send(context.Background(), "go")
	if err != nil {
		t.Fatalf("send: %v", err)
	}

	var preIDs, postIDs []string
	// EventToolDelta now fires both during args streaming (one per ToolCallDelta
	// chunk that has a non-empty ArgsDelta) and during streaming-tool dispatch
	// (one per Yield). Track the two phases separately: before a tool's
	// EventPreToolUse fires, deltas are args-streaming; after, they're
	// dispatch-streaming.
	preFired := map[string]bool{}
	argDeltas := map[string]int{}
	dispatchDeltas := map[string]int{}
	var sawTurnEnd bool
	for ev := range events {
		switch ev.Kind {
		case agent.EventPreToolUse:
			preIDs = append(preIDs, ev.Tool.ToolUseID)
			preFired[ev.Tool.ToolUseID] = true
		case agent.EventPostToolUse:
			postIDs = append(postIDs, ev.Tool.ToolUseID)
		case agent.EventToolDelta:
			if preFired[ev.Tool.ToolUseID] {
				dispatchDeltas[ev.Tool.ToolUseID]++
			} else {
				argDeltas[ev.Tool.ToolUseID]++
			}
		case agent.EventTurnEnd:
			sawTurnEnd = true
		}
	}

	wantOrder := []string{"tu_1", "tu_2", "tu_3"}
	if !equalStrings(preIDs, wantOrder) {
		t.Fatalf("Pre order = %v, want %v", preIDs, wantOrder)
	}
	if !equalStrings(postIDs, wantOrder) {
		t.Fatalf("Post order = %v, want %v", postIDs, wantOrder)
	}
	// Each tool received one args-streaming chunk during ChatStream.
	for _, id := range wantOrder {
		if argDeltas[id] != 1 {
			t.Fatalf("args-stream delta count for %s = %d, want 1", id, argDeltas[id])
		}
	}
	// Only tu_3 (StreamEcho) yields dispatch deltas, and it yields two.
	if dispatchDeltas["tu_3"] != 2 {
		t.Fatalf("dispatch delta count for tu_3 = %d, want 2", dispatchDeltas["tu_3"])
	}
	for _, id := range []string{"tu_1", "tu_2"} {
		if dispatchDeltas[id] != 0 {
			t.Fatalf("non-streaming tool %s emitted %d dispatch deltas, want 0", id, dispatchDeltas[id])
		}
	}
	if !sawTurnEnd {
		t.Fatal("EventTurnEnd not seen")
	}

	// Inspect that the ImageBlock survived into the provider request via
	// MultiContent. providertest.Mock exposes Received() for replay.
	received := prov.Received()
	if len(received) == 0 {
		t.Fatal("provider received no requests")
	}
	last := received[len(received)-1]
	if len(last.Messages) == 0 {
		t.Fatal("provider request had no messages")
	}
	first := last.Messages[0]
	if len(first.MultiContent) < 2 {
		t.Fatalf("first message MultiContent len = %d, want >= 2", len(first.MultiContent))
	}
	hasImage := false
	for _, p := range first.MultiContent {
		if p.Type == provider.MessagePartImage {
			hasImage = true
		}
	}
	if !hasImage {
		t.Fatal("ImageBlock did not propagate into provider.MultiContent")
	}
}

// TestE2E_Phase2b_Compaction exercises compaction end-to-end:
//   - WithStrategy(LLMSummarize) fires after threshold, replaces history with summary,
//     and emits EventCompacted
//   - The summary message reaches the Conversation as a typed SummaryBlock
func TestE2E_Phase2b_Compaction(t *testing.T) {
	t.Parallel()

	prov := providertest.New().
		QueueStream(
			provider.StreamChunk{ContentDelta: "first"},
			provider.StreamChunk{Usage: &provider.Usage{PromptTokens: 200}, FinishReason: provider.FinishStop},
		).
		QueueCompletion(&provider.CompletionResponse{
			Content:      "summary",
			Role:         provider.RoleAssistant,
			FinishReason: provider.FinishStop,
		})

	a := agent.New(prov,
		compactor.WithStrategy(compactor.LLMSummarize(prov, compactor.TriggerOnTokens(100))),
	)
	conv := agent.NewConversation()

	r := a.Runner(conv)
	events, _ := r.Send(context.Background(), "ping")
	var sawCompacted bool
	for ev := range events {
		if ev.Kind == agent.EventCompacted {
			sawCompacted = true
		}
	}
	_ = r.Close()

	if !sawCompacted {
		t.Fatal("compaction did not fire")
	}

	msgs := conv.Messages()
	if len(msgs) == 0 {
		t.Fatal("conversation is empty after compaction")
	}
	last := msgs[len(msgs)-1]
	if len(last.Content) == 0 {
		t.Fatal("last message has no content after compaction")
	}
	if sb, ok := last.Content[0].(agent.SummaryBlock); !ok || sb.Text != "summary" {
		t.Fatalf("last content = %#v, want SummaryBlock(summary)", last.Content[0])
	}
}

// bashLikeTool is a tool fixture used by the Phase 2c E2E test.
type bashLikeTool struct{}

func (bashLikeTool) Name() string         { return "Bash" }
func (bashLikeTool) Description() string  { return "test fixture" }
func (bashLikeTool) Schema() agent.Schema { return agent.Schema{Type: "object"} }
func (bashLikeTool) Call(ctx context.Context, input map[string]any) (*agent.ToolResultBlock, error) {
	return &agent.ToolResultBlock{Content: []agent.ContentBlock{agent.TextBlock{Text: "ok"}}}, nil
}

// TestE2E_Phase2c_ApproveAndLog exercises approve.Hook + observe/log.Plugin
// in one turn: a tool call goes through the approve middleware, executes, and
// the log plugin captures pre/post/turn_end entries.
func TestE2E_Phase2c_ApproveAndLog(t *testing.T) {
	t.Parallel()
	core, recorded := observer.New(zap.InfoLevel)
	logger := zap.New(core)

	args, _ := json.Marshal(map[string]any{"cmd": "ls"})
	prov := providertest.New().
		QueueStream(
			provider.StreamChunk{ToolCallDelta: &provider.ToolCallDelta{
				Index: 0, ID: "tu_1", Name: "Bash", ArgsDelta: string(args),
			}},
			provider.StreamChunk{FinishReason: provider.FinishToolCalls},
		).
		QueueStream(
			provider.StreamChunk{ContentDelta: "ok"},
			provider.StreamChunk{FinishReason: provider.FinishStop},
		)

	approver := approve.New()
	defer func() { _ = approver.Close() }()
	go func() {
		for p := range approver.Pending() {
			_ = approver.Approve(p.ID)
		}
	}()

	a := agent.New(prov,
		agent.Tools(bashLikeTool{}),
		agent.Use("Bash", approve.Hook(approver)),
		logplugin.Plugin(logplugin.WithLogger(logger)),
	)
	conv := agent.NewConversation()
	r := a.Runner(conv)
	defer func() { _ = r.Close() }()

	events, _ := r.Send(context.Background(), "go")
	for range events {
	}

	wantMessages := map[string]int{"pre_tool_use": 0, "post_tool_use": 0, "turn_end": 0}
	for _, e := range recorded.All() {
		if _, ok := wantMessages[e.Message]; ok {
			wantMessages[e.Message]++
		}
	}
	for k, n := range wantMessages {
		if n == 0 {
			t.Errorf("expected log entry %q at least once", k)
		}
	}
}
