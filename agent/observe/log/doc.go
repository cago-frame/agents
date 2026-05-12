// Package log provides a zap-based observability plugin for agent.
//
// Plugin() returns an agent.Option that registers OnEvent callbacks against
// every relevant EventKind. Default level: Info on PreToolUse / PostToolUse
// / MessageEnd / TurnEnd; Warn on Error and Canceled. TextDelta and
// ThinkingDelta are silent by default; WithDeltas() opts in (Debug level).
//
// The default logger is zap.NewNop(); pass WithLogger(l) to wire up cago's
// logger.Ctx(ctx) or zap.L().
package log
