// Package agent is the core in-process AI agent framework: Agent +
// Conversation + Runner driving a provider.Provider chat-stream loop
// with tool-call dispatch, 4-stage hooks (PreToolUse / PostToolUse /
// UserPromptSubmit / TurnEnd), and OnEvent observers.
//
// Construct with [New], spin up a conversation with [NewConversation],
// then drive turns via Runner.Send returning iter.Seq[Event]. Runner
// also exposes Resend (re-emit the last user message), Wait (Send +
// drain sync sugar), and Steer (inject guidance into the active turn).
//
// # Content Blocks and Audience-Based Projections
//
// A [Message] holds a slice of [ContentBlock] values. Every block
// declares both its on-wire Type() (the JSON discriminator used by the
// [github.com/cago-frame/agents/agent/blocks] registry) and its
// Audience() (the bitset of consumers — UI / LLM — that
// include it).
//
// Audience replaces the older "MetadataBlock side channel" mechanism:
// two built-in projection functions filter the canonical Message into the
// view each consumer needs, all driven by the same audience bits:
//
//   - [BuildRequest] keeps only blocks whose Audience has [AudienceLLM]
//     (i.e. [github.com/cago-frame/agents/agent/blocks.ToLLM]).
//   - [RenderForDisplay] keeps only blocks whose Audience has
//     [AudienceUI]; it also fuses ToolUseBlock with its paired
//     ToolResultBlock, hides ThinkingBlock by default, and promotes
//     DisplayTextBlock over plain TextBlock for the user-visible form.
//
// The [SendOption] [WithSendDisplay] and [SteerOption] [WithSteerDisplay]
// attach a [DisplayTextBlock] — its Audience excludes
// [AudienceLLM], so BuildRequest never forwards it, while UI sees it via
// RenderForDisplay. Use these when the chat surface should render the user's
// raw input (e.g. "@srv1 status") while the model gets the expanded body.
//
// # Streaming and Partial State
//
// A [Message]'s [PartialState] (formerly the PartialReason string)
// records whether the assistant turn ended prematurely
// (PartialStreaming, PartialCancelled, PartialErrored, PartialTokenLimit,
// PartialTimeout). [ToolUseBlock.State] discriminates streaming vs
// ready vs malformed for tool calls; BuildRequest drops anything other
// than [ToolUseReady] (replaying mid-stream args would break the
// request), while RenderForDisplay surfaces the raw buffer so the UI
// can render a live "calling X" indicator.
//
// # Events
//
// Event kinds include EventTextDelta, EventThinkingDelta, EventMessageEnd,
// EventPreToolUse, EventPostToolUse, EventToolDelta, EventTurnEnd,
// EventCompacted, EventError, EventCancelled, EventRetry, and the
// terminal EventDone. See [EventKind] for the full list.
package agent
