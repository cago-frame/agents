# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

Cago Agents 是 Go 1.26 写的 AI Agent 框架（`github.com/cago-frame/agents`）。两条独立 facade，不共享 runtime 类型，仅在 `tool.Tool` / `agent.Schema` / `agent.ToolResultBlock` 这些数据接口上对齐：

- **`agent/`** — in-process LLM loop。`agent.New(prov, opts...)` + `agent.NewConversation()` + `a.Runner(conv).Send(ctx, ...)` 返回 `iter.Seq[Event]`。
- **`cliagent/{claudecode,codex}/`** — CLI subprocess 包装，**仅 headless**。包本地 `claude` CLI / `codex app-server`，导出**自己的** native 类型（`Event` / `Session` / `Stream` / `HookInput`），共用 `cliagent/internal/runtime/`（internal-only，不导出）。

`mcp/` 是 loopback bridge，把 Go 的 `tool.Tool` 暴露成 streamable-HTTP MCP server 给两个 CLI facade 用。`app/coding/` 基于 `agent` + `tool/*` + `tool/subagent` 组装出开箱即用的 Coding Agent。

当前设计 spec：`docs/superpowers/specs/2026-05-10-agent-rebuild-phase6-design.md`（Phase 6 总体形态）+ `docs/superpowers/specs/2026-05-11-content-blocks-audience-redesign.md`（content block 三向投影 + audience 重构，**取代** Phase 2c 的 MetadataBlock 侧通道叙述）。其它 design docs 标 (Historical)，仅作归档。

## Commands

```bash
make test          # go test -v ./...
make lint          # golangci-lint run（自动安装）
make lint-fix      # golangci-lint run --fix
make cover         # coverage.out + func summary

go test ./agent -run TestRunner_ToolCall -v   # 单测
go test -race ./...                           # race detector
```

CI 跑 `golangci-lint` + `make test`。`example/` 下的 demo 不在 CI 里，可手动跑。

## Development Principles

- **TDD first** — 写失败测试再写实现；测试与生产代码同包 `_test.go`。
- **窄接口** — 跨包依赖永远依赖接口（`Provider` / `Tool` / compactor `Strategy` 等），不要依赖具体类型。
- `agent/` 与 `cliagent/{claudecode,codex}/` 是独立 facade，不共享 runtime；不要把 `agent.Runner` / `agent.Event` 与 `claudecode.Stream` / `codex.Event` 互相套用。

## Architecture Highlights

### `agent/` — 核心

- 4 stage Hook：`PreToolUse` / `PostToolUse` / `UserPromptSubmit` / `TurnEnd`。`PreToolUseOutput.Decision = DecisionDeny` 反馈 deny 给模型；`UserPromptOutput.Decision = DecisionDeny` 立刻结束 turn（`StopReason = StopHook`）；`TurnEndOutput.EmittedEvents` 在 `EventTurnEnd` 之后追加 event（如 `EventCompacted`）。
- `agent.OnEvent(filter, fn)` 是**纯旁路**观测入口；不要试图在 hook 里改 event。
- `Runner.Send` / `Resend` / `Wait` / `Steer` / `Close`。`Steer(ctx, text, opts...)` 把侧通道引导塞进**当前** active turn（无 active turn 时报 `ErrSteerNoActiveTurn`）。`Wait` 是 `Send + drain` 的同步糖。
- **Content blocks 走 audience 投影**：每个 block 实现 `ContentBlock` 接口（`Type()` + `Audience()`，规范源在 `agent/blocks/`），声明自己进哪几路投影——UI / LLM。两个投影都是纯函数：`BuildRequest` 过滤 `AudienceLLM`、`RenderForDisplay` 过滤 `AudienceUI`。要让 UI 看到原文、模型看展开版：`runner.Send(ctx, expandedBody, agent.WithSendDisplay("@srv1 status"))` —— 注入的是 `DisplayTextBlock`（`Audience = ToUI`），不是过去的 `MetadataBlock`。`Steer` 同理走 `WithSteerDisplay`。
- **agent 不拥有 Store**：宿主应用自行决定持久化模型；需要跨进程 JSON 往返时使用 `agent/blocks` 的 `Encode` / `Decode` / `EncodeAll` / `DecodeAll` 保留 block 类型判别符，避免 `map[string]any` 漂移。
- `EventRetry` 是 ChatStream 短暂失败重试时发的（带 `Event.Retry *RetryEvent`），与 `EventError` 致命终止区分，UI 渲染要分开。
- `BuildRequest(RequestSpec)` 是从 conv 快照 + Agent 配置生成 chat request 的纯函数；`RenderForDisplay(msg)` 是它的 UI 对偶（返回 `DisplayMessage`，已处理 partial 状态 / ToolUse 流式 / DisplayText 优先级 / ToolUse+Result 配对）。要复现某一方视角的视图走对应投影，不要自己拼。
- **PartialReason 是 `PartialState` 类型化字符串枚举**（`PartialStreaming` / `PartialCancelled` / `PartialErrored` / `PartialTokenLimit` / `PartialTimeout`），不要再写成裸 `string`。`ToolUseBlock.State` 字段（`ToolUseReady` / `ToolUseStreaming` / `ToolUseMalformed`）取代过去靠 `Input == nil` 的隐式判别。

子包（窄接口可插拔，core 不强依赖）：`agent/compactor`（摘要压缩 strategy）/ `agent/approve`（PreToolUse 阻塞审批）/ `agent/observe/{log,otel,metric,audit}`（log 已实现，其它是 skeleton）。

### `cliagent/claudecode/` & `cliagent/codex/` — CLI facade（headless-only）

- 公共入口 `New(opts...) *Runner`，**不 import `agent/`**。Runner / Session / Stream / HookInput 都是包内 native 类型。
- `Tools(...)` 接 `tool.Tool`，经 `mcp.Bridge` 暴露给 CLI；hook helper 编成 settings JSON hooks（claudecode）或在 app-server 可控点派发（codex）。
- 同一 Session 跨 turn 复用进程（避免冷启动 / MCP 重连 / prompt cache 重建），**不**跨 Session 复用。**总是** `defer sess.Close(ctx) + defer r.Close(ctx)`，不然进程泄漏。
- `claudecode` 每轮只发**最新一条** user message + `--resume <ThreadID>`；不要把全 history 灌过去（CLI 自己的 transcript 是真理源）。
- `codex` 走 line-delimited JSON-RPC（`thread/start` / `thread/resume` / `turn/start` / `turn/steer` / `turn/interrupt`）。**不要**重新引入 `codex exec --json` fallback——它是 one-shot，没法承载 steer / interrupt / approval。
- Codex 工具名规范化：`commandExecution → command_execution`，`fileChange → file_change`，`mcpToolCall → mcp.<server>.<tool>`，`dynamicToolCall → <namespace>.<tool>`，`collabAgentToolCall → subagent.<tool>`。**`subagent.*` 是 Codex collab 控制面调用，不是子 agent 内部事件流**；分组用 input 里的 `receiverThreadIds` / `agentsStates`，不要用 `ToolEvent.ParentID`。

### `provider/` — 模型 Provider 抽象

`Name` / `ChatCompletion` / `ChatStream`。已有 `openai/`（go-openai）+ `anthropics/`（anthropic-sdk-go）完整实现，覆盖 streaming / tool use / 多模态 / `ThinkingBlock` 往返（Anthropic 的 `Signature` 必须保留）。`providertest/` 是 queueable mock，测试和 example 普遍用。

### `tool/` — 编码工具子包

每个 tool 一个子包（`read` / `write` / `edit` / `bash` / `grep` / `find` / `ls` / `task` / `websearch` / `webfetch`），都暴露 `New(opts ...Option) agent.Tool`（`task` 是个 CRUD 工具组，导出 `NewCreate` / `NewList` / `NewGet` / `NewUpdate` / `NewDelete` + `NewSuite` 一次性返回 5 个）。`tool/state.ReadTracker` 跨 tool 共享：read 完成时记 `(path, mtime, size)`，edit / write 动手前 `Check()`，不命中报 `state.ErrNotRead`，stale 报 `state.ErrStale`。

`bash.New(bash.Jobs(jm))` + input `run_in_background=true` → 把命令丢 `*bash.JobManager`，立即返 `shell_id`；配套 `bash.NewOutput(jm)` / `bash.NewKill(jm)` 暴露 `bash_output` / `kill_shell`；`JobManager.StopAll()` 批量收摊。

### `tool/subagent/` — 子 agent 当普通 tool

`subagent.NewTool(name, desc, []Entry)` 返回**普通 `agent.Tool`**：内部对选中的 child agent 跑一轮 `runner.Wait`，把最终 assistant 文本当返回值。**子事件不冒泡到父 stream**，父侧只看到标准 `EventPreToolUse` / `EventPostToolUse`。要观测子流，自己在创建 child `agent.New(...)` 时挂 `agent.OnEvent`。

### `app/coding/` — 开箱即用

```go
sys, _ := coding.New(ctx, prov, cwd, opts...)
defer sys.Close(ctx)
parent := sys.Agent()
```

三层 API：完整系统 `coding.New` / 子 agent 工厂 `coding.Explore` / `coding.Plan` / `coding.GeneralPurpose` / 工具集 helper `coding.Tools(cwd)` / `coding.ReadOnly(cwd)` / `coding.NewSession(cwd)`。

特性：`~/.claude/CLAUDE.md` + 仓库链 `CLAUDE.md` / `AGENTS.md` 自动注入父 + GP system prompt（`WithoutContextFiles()` 关）；Skills 扫 `~/.claude/skills/` + `.claude/skills/` 的 `SKILL.md` 注入（`WithoutSkills()` 关）；Slash commands（`sys.SlashRegistry().Resolve(line)`，模板支持 `$1` / `$@` / `$ARGUMENTS` / `${@:N:L}`）；自动压缩需 `WithCompactionThreshold(N)`，靠上轮 assistant `Usage.PromptTokens` 判断，摘要替换历史并在 TurnEnd hook 里发 `EventCompacted`。

公开 prompt 常量：`coding.SystemPrompt` / `ExplorePrompt` / `PlanPrompt` / `GeneralPurposePrompt` / `SystemIntro`。

### `mcp/` & `rag/embedding/`

`Bridge.Register(tool.Tool) + Bridge.Start(ctx)` 起 streamable-HTTP MCP server（`127.0.0.1:<random>` + 随机 bearer），duplicate-register 错误 wrap `mcp.ErrToolAlreadyRegistered`。`rag/embedding/` 是 `Embedder` 接口 + OpenAI 实现；其它 RAG 部件（chunking / vectorstore）out-of-scope。

## Things to keep in mind

- `agent.Tool.Serial()==true` 在一轮内任一 tool 命中即让**整轮**串行。
- `claudecode.Session` / `Stream` 是 cliagent 包内 native 类型（住在 `cliagent/internal/runtime/`），不是已删除的 `agent.Session` / `agent.Stream`，不要混用。
- `golangci-lint` 在 `.golangci.yml` 里精挑过 `gosec` 排除项；不要在源码加 `//nolint` 抑制已经全局豁免的规则。
