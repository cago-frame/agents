// Package observe is the parent of agent observability plugin sub-packages.
//
// Each sub-package (log, otel, metric, audit) exports one or more
// constructors that return agent.Option, conventionally named Plugin().
// Plugins use only the public OnEvent / Hook / lifecycle Options of agent;
// they do not reach into core internals.
//
// As of Phase 2c, only observe/log is a real implementation. observe/otel,
// observe/metric, and observe/audit are skeletons reserving the API shape;
// each grows a real implementation in its own future phase.
package observe
