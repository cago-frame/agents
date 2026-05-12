# Cago Agents

> Go 写的 AI Agent 框架。核心 `agent` 包提供 in-process 的 `Agent` + `Conversation` + `Runner`：`agent.New(prov, opts...)` 构造，`agent.NewConversation()` 承载多轮历史，`a.Runner(conv).Send(ctx, "...")` 返回 `iter.Seq[Event]` 给 caller 消费。`cliagent/claudecode` 与 `cliagent/codex` 是独立的 CLI subprocess facade（**两者均仅 headless**），分别包本地 `claude` CLI 和 `codex app-server`，导出**自己的** `Event` / `HookInput` 等 native 类型，不与 `agent/` 共享 runtime 类型，只在 `tool.Tool` / `agent.Schema` / `agent.ToolResultBlock` 这几个数据接口上对齐。

基于 [Cago](https://github.com/cago-frame/cago)。仍在迭代，API 可能变动；当前 spec：[`docs/superpowers/specs/2026-05-10-agent-rebuild-phase6-design.md`](docs/superpowers/specs/2026-05-10-agent-rebuild-phase6-design.md)。

## 模块

- **`agent/`** — 核心 in-process Agent。`agent.New(prov, opts...)` / `agent.NewConversation()` / `Runner.Send`/`Resend`/`Wait`/`Steer`/`Close`；4 stage Hook（`PreToolUse` / `PostToolUse` / `UserPromptSubmit` / `TurnEnd`）+ `OnEvent(filter, fn)` 旁路观测；`EventRetry` 在 transient ChatStream 失败时区分 `EventError` 致命终止。**Content block 用 audience 投影**：每个 block 实现 `ContentBlock`（`Type()` + `Audience()`），两个对偶投影 `BuildRequest`（LLM）/ `RenderForDisplay`（UI）各按自己的位过滤。下挂 `agent/blocks`（规范源 + 类型注册表）/ `agent/compactor`（摘要压缩，输出 `SummaryBlock`）/ `agent/approve`（工具审批）/ `agent/observe/{log,otel,metric,audit}`（可观测性）。
- **`cliagent/claudecode/`** — 把本地 `claude` CLI 包成 headless facade（stream-json over stdin/stdout，跨 turn 复用同一进程）。`claudecode.Tools(...)` 经 MCP bridge 暴露给 CLI；`claudecode.PreToolUse(...)` / `PostToolUse(...)` 等 hook helper 编成 Claude Code settings hooks。
- **`cliagent/codex/`** — 把 `codex app-server` 包成 headless facade（line-delimited JSON-RPC：`thread/start` / `thread/resume` / `turn/start` / `turn/steer` / `turn/interrupt`）。tool-like item 名字规范化（`commandExecution → command_execution`，`mcpToolCall → mcp.<server>.<tool>` 等）；`subagent.*` 是 collab 控制面调用，**不是**子 agent 内部事件流。
- **`cliagent/internal/runtime/`** — 两个 CLI facade 共用的 runtime 原语（Stream + Session + HookChain + Observer + Store），internal-only，不导出。
- **`provider/`** — 模型 Provider 抽象（`Name` / `ChatCompletion` / `ChatStream`）。实现：`provider/openai`、`provider/anthropics`；测试替身：`provider/providertest`。
- **`mcp/`** — Loopback streamable-HTTP MCP Bridge：`Bridge.Register(tool.Tool)` + `Bridge.Start(ctx)`，给两个 CLI facade 用。
- **`tool/`** — 自带的编码工具集子包（`read` / `write` / `edit` / `bash` / `grep` / `find` / `ls` / `todo` / `websearch` / `webfetch`）+ `tool/state.ReadTracker`（read-before-edit / stale 检测）。每个子包都暴露 `New(opts...) tool.Tool`；ready-made 组合（`Tools` / `ReadOnly` / `NewSession`）住在 `app/coding`。
- **`tool/subagent/`** — `subagent.NewTool(name, desc, []Entry{...})` 把子 `*agent.Agent` 包成普通 `agent.Tool`。子事件**不**冒泡到父 stream（要观测自己挂 `agent.OnEvent` 在 child agent 上）。
- **`app/coding/`** — 开箱即用 Coding Agent 系统：`coding.New(ctx, prov, cwd, opts...)` 一行拉起带工具集 / `dispatch_subagent` / 项目上下文（`CLAUDE.md` / `AGENTS.md`） / skills / slash commands / 自动压缩的父 agent。`coding.Explore` / `coding.Plan` / `coding.GeneralPurpose` 是 ready-made `subagent.Entry` 工厂。
- **`rag/embedding/`** — Embedding Provider 抽象 + OpenAI 实现。

## 安装

```bash
go get github.com/cago-frame/agents
```

Go 1.26+。用 `cliagent/claudecode` 还需要本机装好并 `claude login`；用 `cliagent/codex` 还需要本机装好并登录 `codex` CLI。

## Quickstart（in-process agent）

```go
a := agent.New(prov,
    agent.System("You are concise."),
    agent.Tools(myTool),
    agent.PreToolUse(agent.OnlyTool("Bash"), guardTool),
)

conv := agent.NewConversation()
runner := a.Runner(conv)
defer runner.Close()

events, err := runner.Send(ctx, "请帮我做 X")
if err != nil { return err }
for ev := range events {
    if ev.Kind == agent.EventTextDelta {
        fmt.Print(ev.Delta)
    }
}
```

需要同步糖：`runner.Wait(ctx, "...")` 等价 `Send + drain`。需要在 turn 进行中追加 user 内容：`runner.Steer(ctx, text, agent.WithSteerDisplay("@原始展示"))`。要让 UI 看到原始 `@srv1 status` 但模型只看到 mention 展开后的版本：`runner.Send(ctx, expandedBody, agent.WithSendDisplay("@srv1 status"))` —— `WithSendDisplay` / `WithSteerDisplay` 注入的 `DisplayTextBlock`（`Audience = ToUI`）进 conv 给 hook / UI 看，但 `BuildRequest` 因为它不含 `ToLLM` 自然过滤掉。UI 侧别直接遍历 `Message.Content`，调 `agent.RenderForDisplay(msg)` 拿 `DisplayMessage`，partial 状态 / ToolUse 流式 / DisplayText 优先级 / ToolUse+Result 配对都在里面做掉了。

## Quickstart（Claude Code 本地 CLI）

```go
r := claudecode.New(claudecode.Cwd(repo), claudecode.Tools(myTool))
defer r.Close(ctx)

sess := r.Session()
defer sess.Close(ctx)

stream, err := sess.Stream(ctx, "请帮我做 X")
if err != nil { return err }
for stream.Next() {
    if ev := stream.Event(); ev.Kind == claudecode.EventTextDelta {
        fmt.Print(ev.Text)
    }
}
res, _ := stream.Result()
```

`cliagent/claudecode` 与 `cliagent/codex` 接口同形（`New` / `Session` / `Stream`），但导出**自己的** `Event` 类型。Codex 走 JSON-RPC，工具名做规范化（详见 `cliagent/codex/doc.go`）。

## 示例

完整可跑代码见 `example/`：

| 目录 | 说明 |
|---|---|
| `example/agent-basic` | `agent.New(prov, agent.Tools(...), agent.PreToolUse(...))` + 多轮 `runner.Send(ctx, "...")` + `runner.Resend(ctx)`；含 `Decision=Deny` hook 演示，默认 `providertest`。 |
| `example/agent-partial-resume` | 全面演示 partial-cancel / partial-error / Steer / Retry / token limit / ctx deadline / hook error 的 conv + event 行为。 |
| `example/agent-subagent` | `subagent.NewTool` 把子 agent 包成普通 tool；父 stream 只看到 `EventPreToolUse` / `EventPostToolUse`，子流自己挂 `OnEvent` 旁路打印。 |
| `example/coding` | `app/coding.New(...)` 端到端 demo：项目上下文加载、skills、slash commands、`/compact`，默认 `providertest`，可切真实 OpenAI。 |
| `example/claudecode-basic` | `claudecode.New(...)` 驱真实 `claude` CLI；演示 `claudecode.Tools(...)` 经 MCP bridge 暴露 + `claudecode.PostToolUse("Bash", ...)` 注入 `AdditionalContext`。 |
| `example/claudecode-substream` | 消费 Claude Code 内置 `Task` / `Agent` 工具的子 agent 流式输出（`EventTextDelta` / `EventThinkingDelta` 实时打印 + `EventPostToolUse` sub-result 兜底）。 |
| `example/claudecode-multiturn` | 同 Session 跑两轮，看跨轮复用同一 `claude` 进程的延迟下降。 |
| `example/codex-basic` | `codex.New(...)` 驱真实 Codex app-server；演示 `codex.Tools(...)` 经 MCP bridge + 同 Session 的 `thread/resume`。 |

## Codex 事件映射

`cliagent/codex` 把 app-server notification 翻译成统一 `codex.Event`：文本增量走 `EventTextDelta`，reasoning 增量走 `EventThinkingDelta`，工具类 item 的 `item/started` / `item/completed` 分别走 `EventPreToolUse` / `EventPostToolUse`，`turn/completed` 最终产生 stream 级 `EventDone`。

工具名规范化：`commandExecution` → `command_execution`，`fileChange` → `file_change`，`mcpToolCall` → `mcp.<server>.<tool>`，`dynamicToolCall` → `<namespace>.<tool>`，`collabAgentToolCall` → `subagent.<tool>`。

注意：Codex 的 `subagent.*` 是协作控制面调用（如 `subagent.spawnAgent` / `subagent.wait` / `subagent.sendInput`），不是子 agent 内部事件流，也不是 `EventSubagentStop`。Codex 会把子 agent 折叠成单条 `collabAgentToolCall`，不冒泡嵌套子工具；分组用 input 中的 `receiverThreadIds` / `agentsStates`，不要用 `ToolEvent.ParentID`。

## 开发

```bash
make test       # go test -v ./...
make lint       # golangci-lint run
make lint-fix   # golangci-lint run --fix
make cover      # coverage.out + func summary
```
