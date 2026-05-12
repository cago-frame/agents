// Package audit is a SKELETON for an audit-log observability plugin.
//
// As of Phase 2c, only the API shape is reserved. A real implementation
// accepts a Sink (user-provided) and records, at minimum, every
// PreToolUse / PostToolUse with the input / output payload, redacted
// per the caller's policy.
//
// Open design questions:
//
//   - Audit record schema (JSON shape, versioned?)
//   - Redaction strategy (caller-provided / regex / structured)
//   - Sink batching vs synchronous write
//   - Retention / rotation (caller's concern via Sink impl)
package audit
