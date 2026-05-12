// Package claudecode wraps the local Claude Code CLI (`claude`) behind a
// self-contained, headless-only facade. Public entry points are
// claudecode.New / claudecode.Runner / claudecode.Session / claudecode.Stream
// and the claudecode.Event / claudecode.HookInput / claudecode.HookOutput
// value types. This package does NOT import the agent package's Event/Hook
// types — only the data interfaces (tool.Tool, agent.Schema,
// agent.ToolResultBlock).
//
// The runner spawns `claude -p --input-format stream-json --output-format
// stream-json --verbose ...` once per Session and reuses the process across
// turns to avoid cold start, MCP reconnect, and prompt-cache rebuild costs.
// Each call to Session.Stream writes a stream-json `user` frame to stdin
// and yields the resulting events. The session_id from the first
// `system.init` frame is the cross-turn resume identifier, exposed via
// Stream.SessionID() / Stream.State().ThreadID.
//
// claudecode.Tools(...) takes tool.Tool values and exposes them to the CLI
// through the loopback streamable-HTTP MCP bridge in package mcp; hook
// helpers (PreToolUse / PostToolUse / UserPromptSubmit / Stop / SubagentStop
// / SessionStart / SessionEnd / Notification) install Claude Code settings
// hooks via the internal clihook UDS layer and surface their callbacks as
// `func(ctx, claudecode.HookInput) (*claudecode.HookOutput, error)`.
//
// Steer / FollowUp inject additional stream-json `user` frames at safe
// points within an active turn (Steer) or as separately scheduled turns
// owned by the Session (FollowUp). Stream.Close does not drop a queued
// FollowUp; only Session.Close / Runner.Close do.
package claudecode
