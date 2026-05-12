// Package compactor provides conversation-summarization strategies.
//
// A Strategy decides whether a Conversation should be compacted (typically
// after a turn) and produces a single summary Message that replaces history.
// The WithStrategy(s) Option registers a TurnEnd hook on an agent.Agent.
// When the strategy fires, the hook truncates the conversation to zero
// messages, appends the summary, and emits an EventCompacted via
// TurnEndOutput.EmittedEvents.
//
// LLMSummarize uses a provider.Provider to generate the summary; Noop is a
// no-op for tests.
package compactor
