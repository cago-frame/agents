// Package otel is a SKELETON for an OpenTelemetry observability plugin.
//
// As of Phase 2c, only the API shape is reserved. Plugin() returns a no-op
// agent.Option. A real implementation should:
//
//   - Accept a trace.Tracer (and optionally a meter)
//   - Open a Span at SessionStart / Send (matching the chosen agent semconv)
//   - Add Span events for PreToolUse / PostToolUse / Error / Canceled
//   - Close the Span on TurnEnd / Done
//
// Open design questions to resolve before real implementation:
//
//   - Span hierarchy: one Span per turn vs one Span per Send
//   - Tool calls: child Spans vs Span events
//   - Multi-step turns: how to convey step boundaries
//   - Error attributes: align with otel-genai semconv when stable
package otel
