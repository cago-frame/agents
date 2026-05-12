---
name: cago-agents
description: Cago Agents Go framework skill. Use when working with github.com/cago-frame/agents, agent.New(prov, opts...), agent.NewConversation(), Runner.Send/Resend/Wait/Steer, hooks (PreToolUse / PostToolUse / UserPromptSubmit / TurnEnd), OnEvent observers, audience-driven ContentBlocks (DisplayTextBlock / SummaryBlock / RefBlock / NoticeBlock) via agent/blocks, BuildRequest / RenderForDisplay projection pair, MCP loopback bridge tools, cliagent/claudecode headless wrapper, cliagent/codex app-server wrapper, app/coding turnkey coding agent, or tool/subagent. Do not use for unrelated Go LLM code that does not import cago-frame/agents.
---

# Cago Agents

Go AI agent framework under module `github.com/cago-frame/agents`. Phase 6 shape (current):

- `agent.New(prov, opts...)` constructs an in-process `*agent.Agent` driving a `provider.Provider` chat-stream loop.
- `agent.NewConversation()` is the multi-turn history container; the same `Conversation` may be threaded through many `Send` calls.
- `a.Runner(conv)` returns a `*Runner` — it owns one in-flight turn at a time. `Send` returns an `iter.Seq[Event]` that the caller drains.
- 4-stage hooks are for control: `PreToolUse` / `PostToolUse` / `UserPromptSubmit` / `TurnEnd`. Read-only observation goes through `agent.OnEvent(filter, fn)` (NOT through hooks).
- `agent.Tools(...)` registers `tool.Tool` values. CLI facades (`cliagent/claudecode`, `cliagent/codex`) expose those tools to the underlying CLI through the loopback streamable-HTTP MCP bridge in `mcp/`.

There is no longer a `Backend` interface, no `agent.NewWithBackend`, no `agent.Session`/`agent.Stream` (those were removed in Phase 6). Anything you find online that talks about `agent.NewWithBackend(...)`, `*agent.Stream.Next()`, `*agent.Session`, or `agent.Observe(...)` is historical; ignore it.

## Module Map

| Import path | Role |
|---|---|
| `agent` | `Agent` + `Conversation` + `Runner`, `Event`, `Hook`, `Tool`, `ContentBlock` (type-aliased from `agent/blocks`), `BuildRequest`, `RenderForDisplay`, `DisplayMessage`. |
| `agent/blocks` | Canonical `ContentBlock` interface (`Type()` + `Audience()`), every typed variant (`TextBlock` / `ImageBlock` / `ToolUseBlock` / `ToolResultBlock` / `ThinkingBlock` / `DisplayTextBlock` / `RefBlock` / `NoticeBlock` / `SummaryBlock`), `AudienceMask` (`ToUI` / `ToLLM`), and the registry (`Register`, `RegisterFactory`, `Encode`, `Decode`, `EncodeAll`, `DecodeAll`, `StoredBlock`). |
| `agent/compactor` | `Strategy` interface + `Noop` and `LLMSummarize`; `compactor.WithStrategy(s)` registers a TurnEnd hook that emits `EventCompacted`. |
| `agent/approve` | Tool-call approval queue: `approve.New()`, `Approver.Pending() iter.Seq[Pending]`, `approve.Hook(*Approver) PreToolUseHook`. |
| `agent/observe/log` | `log.Plugin(opts...) agent.Option`: cago-zap binding for lifecycle events. `agent/observe/{otel,metric,audit}` are skeletons (interfaces + no-op). |
| `provider` | `Provider` (`Name` / `ChatCompletion` / `ChatStream`) abstraction. Implementations: `provider/openai`, `provider/anthropics`. Test double: `provider/providertest`. |
| `cliagent/claudecode` | Headless wrapper around the local `claude` CLI. `claudecode.New(opts...) *Runner`, `Runner.Session`, `Session.Stream` — all native types in this package, NOT `agent.*`. |
| `cliagent/codex` | Headless wrapper around `codex app-server`. Same shape as claudecode; native types in this package. |
| `cliagent/internal/runtime` | Runtime primitives (Stream, Session, HookChain, Observer fan-out, Store) shared between the two CLI facades — NOT exported. |
| `mcp` | Loopback streamable-HTTP MCP bridge; `Bridge.Register(tool.Tool)` + `Bridge.Start(ctx)`. Used by both CLI facades. |
| `tool` | Built-in coding tool subpackages (`read` / `write` / `edit` / `bash` / `grep` / `find` / `ls` / `todo` / `websearch` / `webfetch`) + `tool/state.ReadTracker` for read-before-edit invariants. |
| `tool/subagent` | `subagent.NewTool(name, desc, []Entry)` — wraps child agents behind one ordinary `agent.Tool`. |
| `app/coding` | Turnkey coding agent system: `coding.New(ctx, prov, cwd, opts...)` returns `*System`. |

## Construction Pattern

```go
a := agent.New(prov,
    agent.System("You are concise."),
    agent.Model("gpt-5.5"),
    agent.Tools(myTool),
    agent.PreToolUse(agent.OnlyTool("Bash"), guardTool),
    agent.OnEvent(agent.AnyEvent(), func(ctx context.Context, ev agent.Event) {
        // read-only telemetry; never use this for control
    }),
)
```

Drive a turn:

```go
conv := agent.NewConversation()
runner := a.Runner(conv)
defer runner.Close()

events, err := runner.Send(ctx, "do the task")
if err != nil {
    return err
}
for ev := range events {
    switch ev.Kind {
    case agent.EventTextDelta:
        fmt.Print(ev.Delta)
    case agent.EventPreToolUse:
        log.Printf("tool start: %s", ev.Tool.Name)
    case agent.EventPostToolUse:
        log.Printf("tool done: %s", ev.Tool.Name)
    case agent.EventTurnEnd:
        log.Printf("stop=%s", ev.Stop)
    case agent.EventDone:
        // last event of the iter
    }
}
```

Sync sugar when you don't want to consume the iter yourself:

```go
err := runner.Wait(ctx, "do the task")  // Send + drain
```

`runner.Resend(ctx)` re-emits the last user message (e.g. after a transient provider error). `runner.Steer(ctx, text, opts...)` injects guidance into the **active** turn; if no turn is in flight it returns `agent.ErrSteerNoActiveTurn`.

## Event Model

`agent.Event` is an immutable value type; observers receive a copy. Kinds:

- `EventTextDelta`, `EventThinkingDelta`, `EventToolDelta`
- `EventMessageEnd`
- `EventPreToolUse`, `EventPostToolUse`
- `EventTurnEnd` (carries `Stop StopReason`)
- `EventCompacted` (emitted after TurnEnd by compactor TurnEnd hooks)
- `EventError` (fatal), `EventCancelled` (ctx cancel)
- `EventRetry` (transient ChatStream failure that the framework will retry; carries `Event.Retry *RetryEvent` so UI can distinguish from `EventError`)
- `EventDone` (always the final event of the iter; emitted exactly once per turn)

`StopReason` values: `StopEndTurn` / `StopMaxSteps` / `StopHook` / `StopError` / `StopCancelled`.

Filter helpers for `agent.OnEvent(filter, fn)`: `agent.AnyEvent()`, `agent.OnlyKinds(EventPreToolUse, EventPostToolUse)`, `agent.OnlyTool("Bash")`.

## Hooks vs Observers

Hooks make decisions; `OnEvent` observes. Hooks come in 4 stages:

```go
agent.PreToolUse(agent.OnlyTool("Bash"), func(ctx context.Context, in *agent.PreToolUseInput) (*agent.PreToolUseOutput, error) {
    if shouldDeny(in.ToolUse.Input) {
        return &agent.PreToolUseOutput{Decision: agent.DecisionDeny, DenyReason: "policy"}, nil
    }
    return nil, nil
})
```

Stage rules:

- `PreToolUseOutput.Decision = DecisionDeny` returns the deny text to the model **as a tool result** — the turn continues; the model can react.
- `UserPromptOutput.Decision = DecisionDeny` aborts the turn immediately with `StopReason = StopHook`.
- `TurnEndOutput.EmittedEvents` lets a TurnEnd hook insert extra events into the iter after `EventTurnEnd` (this is how `agent/compactor` emits `EventCompacted`).

`OnEvent` observers see a **copy** of every event matching the filter; they cannot mutate event flow or block backend progress. Use them for logging / metrics / persistence side-effects.

## Content Blocks and Audience-Based Projections

Every `ContentBlock` declares both `Type()` (the on-wire JSON discriminator used by the `agent/blocks` registry) and `Audience()` (an `AudienceMask` bitset over `ToUI` / `ToLLM`). Two pure projection functions consume the canonical `Message`:

| Projection | Function | Filter |
|---|---|---|
| LLM request | `agent.BuildRequest(spec)` | keeps `Audience.Has(ToLLM)` |
| UI render | `agent.RenderForDisplay(msg)` → `DisplayMessage` | keeps `Audience.Has(ToUI)`, fuses ToolUse+ToolResult, hides Thinking by default |

Block types and their default audiences:

| Block | Audience | Used for |
|---|---|---|
| `TextBlock` | `ToAll` | plain text |
| `ImageBlock` | `ToAll` | image content |
| `ToolUseBlock` | `ToAll` | assistant tool invocation; `State` field is `ToolUseReady` / `ToolUseStreaming` / `ToolUseMalformed` |
| `ToolResultBlock` | `ToAll` | tool reply (nested `Content` round-trips through the registry) |
| `ThinkingBlock` | `ToLLM` | model chain-of-thought; hidden from UI by default |
| `DisplayTextBlock` | `ToUI` | UI-visible text that the LLM does NOT see (e.g. raw `@mention` form) |
| `RefBlock` | `ToUI` | asset/file/tool reference pill |
| `NoticeBlock` | `ToUI` | UI system notice |
| `SummaryBlock` | `ToAll` | compactor output; UI can render with a "summarized" badge |

`WithSendDisplay` / `WithSteerDisplay` produce `DisplayTextBlock`, not the removed `MetadataBlock`:

```go
runner.Send(ctx, "hello server srv1, what's your status",
    agent.WithSendDisplay("@srv1 status"))   // UI shows the @-form; the LLM gets the expanded body.

runner.Steer(ctx, "expanded steer body",
    agent.WithSteerDisplay("@raw display")) // last call wins for Steer
```

UIs should consume `RenderForDisplay`, not raw `Message.Content`:

```go
for _, m := range conv.Messages() {
    dm := agent.RenderForDisplay(m)
    for _, seg := range dm.Segments {
        switch s := seg.(type) {
        case agent.DisplayText:    // SourceLLM=true means the model also saw this string
        case agent.DisplayToolCall: // already fused with its ToolResult; Status enum is Pending/Ready/Success/Error
        case agent.DisplaySummary:  // compactor output; UI should distinguish from normal text
        }
    }
}
```

For cross-message pairing (assistant's `ToolUseBlock` in one message, `ToolResultBlock` in the next `Role=Tool` message), use `agent.RenderConversationForDisplay(msgs)`.

## CLI Facades (`cliagent/claudecode`, `cliagent/codex`)

Both are **headless-only** subprocess wrappers and do NOT import `agent/`. They expose their own native types (`claudecode.Event` / `codex.Event`, etc.) — these have similar shape to `agent.Event` but are independent enums and may diverge.

```go
r := claudecode.New(claudecode.Cwd(repo), claudecode.Tools(myTool),
    claudecode.PostToolUse("Bash", func(ctx context.Context, in claudecode.HookInput) (*claudecode.HookOutput, error) {
        return &claudecode.HookOutput{AdditionalContext: "extra context"}, nil
    }),
)
defer r.Close(ctx)

sess := r.Session()
defer sess.Close(ctx)

stream, err := sess.Stream(ctx, "do the task")
// ... consume via stream.Next() / stream.Event() / stream.Result()
```

claudecode launches `claude -p --input-format stream-json --output-format stream-json --verbose ...` once per Session and reuses the process across turns. Cross-turn resume identifier = `Stream.SessionID()` (= `claude` thread id from the first `system.init` frame).

codex uses line-delimited JSON-RPC against `codex app-server --listen stdio://` (`thread/start`, `thread/resume`, `turn/start`, `turn/steer`, `turn/interrupt`). codex never had `codex exec --json` fallback in this codebase.

### Codex Tool-Name Normalization

| Codex item type | `Event.Tool.Name` |
|---|---|
| `commandExecution` | `command_execution` |
| `fileChange` | `file_change` |
| `mcpToolCall` | `mcp.<server>.<tool>` (or `<tool>` when server empty) |
| `dynamicToolCall` | `<namespace>.<tool>` (or `<tool>` when namespace empty) |
| `collabAgentToolCall` | `subagent.<tool>` |

Codex `subagent.*` are collab control-plane calls (`subagent.spawnAgent`, `subagent.wait`, `subagent.sendInput`). They surface as ordinary pre/post tool events — they are NOT child-agent internal event streams and NOT `EventSubagentStop`. Codex folds child-agent work into a single `collabAgentToolCall`; nested child tools are not surfaced and `ToolEvent.ParentID` is empty for codex. Group by `receiverThreadIds` / `agentsStates` from the input metadata.

## Coding Tools (`tool/` + `app/coding`)

`tool/` ships file/shell tool subpackages each exposing `New(opts...) agent.Tool`. Ready-made bundles live in `app/coding`:

```go
import "github.com/cago-frame/agents/app/coding"

tools := coding.Tools(cwd)        // 7 stateless tools: read+write+edit+bash+grep+find+ls
tools := coding.ReadOnly(cwd)     // 4 read-only tools: read+grep+find+ls

sess  := coding.NewSession(cwd)   // shared *state.ReadTracker + bash JobManager + task.Store
tools := sess.All()               // 14 tools (7 stateless + bash_output + kill_shell + task_create/list/get/update/delete)
```

`coding.NewSession(cwd)` enforces Claude Code's read-before-edit invariant: `edit` / `write` against an existing file fails with `state.ErrNotRead` until `read` ran on that path; if mtime/size changed externally the next `edit` / `write` returns `state.ErrStale`. New-file `write` is exempt.

`bash` background mode: `bash.New(bash.Jobs(jm))` makes `run_in_background=true` calls return a `shell_id`; pair with `bash.NewOutput(jm)` (`bash_output`) and `bash.NewKill(jm)` (`kill_shell`). `coding.NewSession` wires all three to a fresh `*JobManager` automatically; `coding.System.Close` calls `JobManager.StopAll()`.

## Coding Agent System (`app/coding`)

```go
sys, err := coding.New(ctx, prov, cwd,
    coding.WithModel("gpt-5.5"),
    coding.WithSearch(searchProv),
    coding.WithCompactionThreshold(120_000),
)
defer sys.Close(ctx)

parent := sys.Agent()             // *agent.Agent fully wired
conv := agent.NewConversation()
r := parent.Runner(conv)
defer r.Close()
events, _ := r.Send(ctx, "...")
```

What `coding.New` wires up:

- Parent toolset = `Session.All()` + `subagent` + optional web tools.
- `subagent` ships with `Explore` (read-only code search), `Plan` (architecture plans), `GeneralPurpose` (parent toolset minus dispatch / todo). Disable GP via `WithoutGeneralPurpose()`; replace or add Entries via `WithExtraSubagents(...)` (extra Entry with same Type **replaces** the default and Closes the replaced one).
- Project context: `~/.claude/CLAUDE.md` plus every `CLAUDE.md` / `AGENTS.md` along cwd → repo-root chain (`WithoutContextFiles()` to disable).
- Skills: `~/.claude/skills/` and `.claude/skills/` along the chain — scanned for `SKILL.md` with frontmatter `name` / `description` / `disable-model-invocation`, injected as XML blocks (`WithoutSkills()` to disable).
- Dynamic system prompt: `coding.BuildSystemPrompt(opts)` assembles tools + project context + skills + each tool's `PromptSnippet()` / `PromptGuidelines()`.
- Slash commands: `~/.claude/commands/` + `.claude/commands/` chain plus builtin `/compact` and `/help`. Resolve via `sys.SlashRegistry().Resolve(line)`. Templates support `$1` / `$@` / `$ARGUMENTS` / `${@:N}` / `${@:N:L}`.
- Compaction: manual `sys.Compact(ctx, conv, ...)` or `/compact [hint]`. Auto-trigger requires `WithCompactionThreshold(N)` and watches the previous turn's assistant `Usage.PromptTokens`. Summarizer LLM defaults to the parent provider; override with `WithCompactor(CompactSpec{...})`. Compaction calls `conv.Truncate(0) + conv.Append(summary)` and (auto path only) emits `agent.EventCompacted` from a TurnEnd hook.

Public prompt constants for customization: `coding.SystemIntro`, `coding.SystemPrompt`, `coding.ExplorePrompt`, `coding.PlanPrompt`, `coding.GeneralPurposePrompt`.

Subagent factories are usable on their own:

```go
explore := coding.Explore(prov, cwd)        // subagent.Entry, read-only
plan    := coding.Plan(prov, cwd)
gp      := coding.GeneralPurpose(prov, cwd)
dispatch := subagent.NewTool("dispatch", "...", []subagent.Entry{explore, plan, gp})
```

## Sub-Agent Tool

`tool/subagent.NewTool(name, desc, []Entry{...})` returns an ordinary `agent.Tool`. Schema is `{title, type (enum), prompt}`; `Call` selects the matching child `*agent.Agent`, runs `runner.Wait(ctx, prompt)`, and returns the final assistant text as the tool result.

Semantics:

- Child events do NOT bubble to the parent stream.
- Parent stream sees only standard `EventPreToolUse` / `EventPostToolUse` for the subagent tool call.
- Child `Conversation`, history, and observers are independent.
- To observe child internals, register `agent.OnEvent(filter, fn)` on the child agent at `agent.New(...)` time.

## Persistence Boundary

The in-process `agent` package no longer owns a Store contract. Host
applications decide their own persistence schema and can rehydrate history
with `agent.LoadConversation(id, []agent.Message)`.

When a host needs JSON round-tripping for message content, use
`agent/blocks.Encode` / `Decode` / `EncodeAll` / `DecodeAll`. `StoredBlock`
keeps each block's type discriminator on the wire so typed `ContentBlock`
values rehydrate without drifting into `map[string]any`.

## Common Mistakes

| Mistake | Correct model |
|---|---|
| Calling `agent.NewWithBackend(...)` / `agent.NewBuiltinBackend(...)` | Removed in Phase 6. Use `agent.New(prov, opts...)`. |
| Using `*agent.Stream.Next()` / `*agent.Session` | Removed. Use `runner.Send(ctx, ...)` returning `iter.Seq[Event]`. |
| Calling `agent.Observe(...)` | Use `agent.OnEvent(filter, fn)`. |
| Mutating events from inside a hook | Hooks return Decision objects; events are emitted by the runner and observed via `OnEvent`, not edited. |
| Putting display text into `TextBlock` to "hide it from the LLM" | Use `agent.DisplayTextBlock` (or `WithSendDisplay` / `WithSteerDisplay`); its `Audience()` excludes `ToLLM` so `BuildRequest` strips it. |
| Using `MetadataBlock{Key, Value}` for side-channel data | Removed. Use a typed block instead — `DisplayTextBlock` for display text, `RefBlock` for refs, `NoticeBlock` for UI notices, `SummaryBlock` for summaries. Or register a new typed block via `blocks.RegisterFactory[T]()` if you need a domain-specific variant. |
| Walking raw `Message.Content` in UI code | Use `agent.RenderForDisplay(msg)` (or `RenderConversationForDisplay(msgs)`) — it handles partial state, ToolUse fusion, and DisplayText precedence in one place. |
| Treating `ToolUseBlock.Input == nil` as "still streaming" | Check `ToolUseBlock.State` explicitly (`ToolUseReady` / `ToolUseStreaming` / `ToolUseMalformed`). |
| Expecting `tool/subagent` child events on the parent stream | Child events do not bubble; observe the child directly. |
| Treating Codex `subagent.*` as nested child tool events | They are Codex collab control-plane tool calls. |
| Using `ToolEvent.ParentID` for codex grouping | Codex leaves it empty; use `receiverThreadIds` / `agentsStates` from the input metadata. |
| Re-introducing `codex exec --json` fallback | Removed intentionally; `exec --json` is one-shot and cannot carry steer / interrupt / approval requests. |
| Forgetting `defer runner.Close()` / `defer sess.Close(ctx)` / `defer r.Close(ctx)` for CLI facades | Releases CLI processes and MCP bridge resources. |

## Verification

```bash
go test ./agent
go test ./cliagent/claudecode
go test ./cliagent/codex
go test ./tool/subagent
go test ./tool/...
go test ./app/coding
```

Full gate when changing framework contracts:

```bash
make test
make lint
```

Current spec: `docs/superpowers/specs/2026-05-10-agent-rebuild-phase6-design.md`. Specs dated before 2026-05-10 are marked Historical and may describe removed shapes (`Backend`, `Session`/`Stream`, `Observe`, `EventSubagentStop` on the agent package).
