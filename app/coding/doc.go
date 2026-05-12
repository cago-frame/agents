// Package coding provides an out-of-the-box Coding Agent system.
//
// Three layers of API:
//
//  1. Full system:
//
//     sys, _ := coding.New(prov, cwd, opts...)
//     defer sys.Close(ctx)
//     parent := sys.Agent()
//
//  2. Subagent factories: coding.Explore / coding.Plan / coding.GeneralPurpose
//
//  3. Tool helpers: coding.Tools / coding.ReadOnly / coding.NewSession
//
// Auto-loaded by default:
//   - Project context: ~/.claude/CLAUDE.md + every CLAUDE.md / AGENTS.md along the
//     repo-root → cwd chain.
//   - Skills: ~/.claude/skills/ + .claude/skills/ along the chain, injected as an
//     XML block into the system prompt.
//   - Slash commands: ~/.claude/commands/ + .claude/commands/ along the chain, plus
//     builtin /compact and /help.
//   - Dynamic system prompt: assembled from the current tool set + project context +
//     skills.
//
// Disable individual features: WithoutContextFiles / WithoutSkills / WithoutSlashCommands.
//
// Compaction: manual by default (sys.Compact / /compact); WithCompactionThreshold(N) enables
// auto-trigger based on the previous turn's provider.Usage.PromptTokens. Summarizer LLM
// reuses the parent provider by default; override with WithCompactor(CompactSpec{...}).
package coding
