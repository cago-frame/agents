// Package metric is a SKELETON for a metrics observability plugin.
//
// As of Phase 2c, only the API shape is reserved. A real implementation
// should accept a prometheus.Registerer (or OTel meter) and emit at minimum:
//
//   - turns_total{stop_reason, model}
//   - tool_calls_total{tool_name, decision}
//   - turn_duration_seconds histogram
//   - tool_call_duration_seconds histogram
//   - prompt_tokens_total / completion_tokens_total counters
//
// Open design questions:
//
//   - Choice of OTel meter vs prometheus.Registerer (or both)
//   - Cardinality control for tool_name (allowlist? hash?)
//   - Model attribute extraction (per-turn config)
package metric
