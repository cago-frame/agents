// Package codex wraps a local Codex app-server (`codex app-server --listen stdio://`)
// behind a self-contained facade. Public entry points are codex.New /
// codex.Runner / codex.Session / codex.Stream and the codex.Event /
// codex.HookInput / codex.HookOutput value types. This package does NOT
// import the agent package's Event/Hook types — only the data interfaces
// (tool.Tool, agent.Schema, agent.ToolResultBlock).
//
// The runner drives Codex through line-delimited JSON-RPC (`thread/start`,
// `thread/resume`, `turn/start`, `turn/steer`, `turn/interrupt`) and
// translates app-server notifications into the codex.Event stream:
//
//   - thread/started -> codex.EventSessionStart
//   - item/agentMessage/delta -> codex.EventTextDelta
//   - item/reasoning/textDelta and item/reasoning/summaryTextDelta -> codex.EventThinkingDelta
//   - thread/tokenUsage/updated -> codex.EventUsage
//   - item/started for tool-like items -> codex.EventPreToolUse
//   - item/completed for tool-like items -> codex.EventPostToolUse
//   - turn/completed -> codex.EventStop, followed by the stream-level codex.EventDone
//
// Tool-like Codex items are exposed through Event.Tool.Name with stable names:
//
//   - commandExecution -> command_execution
//   - fileChange -> file_change
//   - mcpToolCall -> mcp.<server>.<tool>, or <tool> when server is empty
//   - dynamicToolCall -> <namespace>.<tool>, or <tool> when namespace is empty
//   - collabAgentToolCall -> subagent.<tool>
//
// Codex "subagent.*" tool names are collaboration control-plane calls such as
// subagent.spawnAgent, subagent.wait, and subagent.sendInput. They are ordinary
// pre/post tool events, not child-agent internal event streams and not
// codex.EventSubagentStop. Codex folds child-agent work into a single
// collabAgentToolCall, so ToolEvent.ParentID is empty for Codex; applications
// that group collab calls should use the collab metadata in ToolEvent.Input,
// especially receiverThreadIds and agentsStates.
package codex
