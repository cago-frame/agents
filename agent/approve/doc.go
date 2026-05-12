// Package approve provides a synchronous approval gate for tool use.
//
// An Approver is a process-wide queue of Pending decisions. Hook(approver)
// returns an agent.ToolMiddleware that, on each invocation, enqueues a
// Pending and blocks until Approve(id) or Deny(id, reason) is called by an
// out-of-band actor (typically a UI consuming Pending() iter.Seq).
//
// Routing is left to the caller via agent.Use(matcher, approve.Hook(a)).
// There is intentionally no built-in WithMatcher: the agent.Use registration
// already supports a regex matcher.
//
// There is no framework-level timeout; callers control via context.
package approve
