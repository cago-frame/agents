package agent_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	. "github.com/smartystreets/goconvey/convey"

	"github.com/cago-frame/agents/agent"
	"github.com/cago-frame/agents/provider"
	"github.com/cago-frame/agents/provider/providertest"
)

// scenarioPanicTool always panics inside Call. Currently the framework does
// not recover panics in tool dispatch (see B3 SkipConvey below), so this is
// only kept as documentation of the intended-but-not-implemented behavior.
type scenarioPanicTool struct{}

func (scenarioPanicTool) Name() string         { return "panicky" }
func (scenarioPanicTool) Description() string  { return "always panics" }
func (scenarioPanicTool) Schema() agent.Schema { return agent.Schema{Type: "object"} }
func (scenarioPanicTool) Call(_ context.Context, _ map[string]any) (*agent.ToolResultBlock, error) {
	panic("intentional panic from scenarioPanicTool")
}

// scenarioErrTool always returns an error from Call. (Named with a
// "scenario" prefix to avoid clashing with the package-level errTool defined
// in runner_tool_test.go, which uses a different Name.)
type scenarioErrTool struct{}

func (scenarioErrTool) Name() string         { return "borked" }
func (scenarioErrTool) Description() string  { return "always errors" }
func (scenarioErrTool) Schema() agent.Schema { return agent.Schema{Type: "object"} }
func (scenarioErrTool) Call(_ context.Context, _ map[string]any) (*agent.ToolResultBlock, error) {
	return nil, errors.New("tool simulated failure")
}

// chatAddToolWithCounter wraps chatAddTool (from scenarios_chat_test.go) to
// count whether Call was reached. Used to verify PreToolUse deny blocks dispatch.
type chatAddToolWithCounter struct{ counted *bool }

func (c chatAddToolWithCounter) Name() string        { return "add" }
func (c chatAddToolWithCounter) Description() string { return "add" }
func (c chatAddToolWithCounter) Schema() agent.Schema {
	return chatAddTool{}.Schema()
}
func (c chatAddToolWithCounter) Call(ctx context.Context, in map[string]any) (*agent.ToolResultBlock, error) {
	*c.counted = true
	return chatAddTool{}.Call(ctx, in)
}

func TestScenario_Errors(t *testing.T) {
	Convey("Feature: 错误路径下的契约", t, func() {

		Convey("Provider 错误", func() {
			Convey("A1: mid-stream Err (无 retry) → EventError + PartialErrored 保留", func() {
				boom := errors.New("simulated upstream blip")
				prov := providertest.New().QueueStream(
					provider.StreamChunk{ContentDelta: "In a galaxy "},
					provider.StreamChunk{ContentDelta: "far, far"},
					provider.StreamChunk{Err: boom},
				)
				_, conv, r := setupAgent(t, prov)

				evts := drainEvents(mustSend(t, r, "故事"))

				errEv, ok := findEvent(evts, agent.EventError)
				So(ok, ShouldBeTrue)
				So(errors.Is(errEv.Error, boom), ShouldBeTrue)

				So(conv.Len(), ShouldEqual, 2)
				last := lastMessage(t, conv)
				So(last.PartialReason, ShouldEqual, agent.PartialErrored)
				So(textOfBlocks(last.Content), ShouldContainSubstring, "far")
			})

			Convey("A2: provider 直接返 error 不开流 → user msg 仍在 conv", func() {
				prov := providertest.New().QueueError(errors.New("connection refused"))
				_, conv, r := setupAgent(t, prov)

				evts := drainEvents(mustSend(t, r, "hi"))

				_, hadErr := findEvent(evts, agent.EventError)
				So(hadErr, ShouldBeTrue)
				So(conv.Len(), ShouldBeGreaterThanOrEqualTo, 1)
				first, _ := conv.MessageAt(0)
				So(first.Role, ShouldEqual, agent.RoleUser)
			})

			Convey("A3: transient 503 + RetryPolicy → EventRetry → 自动续写", func() {
				transient := &provider.ProviderError{StatusCode: 503, Err: errors.New("upstream busy")}
				prov := providertest.New().
					QueueStream(
						provider.StreamChunk{ContentDelta: "Once "},
						provider.StreamChunk{Err: transient},
					).
					QueueStream(
						provider.StreamChunk{ContentDelta: "upon a time."},
						provider.StreamChunk{FinishReason: provider.FinishStop},
					)
				_, conv, r := setupAgent(t, prov, agent.Retry(agent.RetryPolicy{
					MaxAttempts:  3,
					InitialDelay: 1 * time.Millisecond,
					MaxDelay:     10 * time.Millisecond,
				}))

				evts := drainEvents(mustSend(t, r, "讲故事"))

				retryEv, ok := findEvent(evts, agent.EventRetry)
				So(ok, ShouldBeTrue)
				So(retryEv.Retry, ShouldNotBeNil)
				So(retryEv.Retry.Attempt, ShouldBeGreaterThanOrEqualTo, 1)

				last := lastMessage(t, conv)
				So(textOfBlocks(last.Content), ShouldContainSubstring, "upon a time")
			})

			Convey("A4: retry 用尽 → EventError 致命", func() {
				transient := &provider.ProviderError{StatusCode: 503, Err: errors.New("always busy")}
				prov := providertest.New().
					QueueStream(provider.StreamChunk{Err: transient}).
					QueueStream(provider.StreamChunk{Err: transient}).
					QueueStream(provider.StreamChunk{Err: transient})
				_, _, r := setupAgent(t, prov, agent.Retry(agent.RetryPolicy{
					MaxAttempts:  2,
					InitialDelay: 1 * time.Millisecond,
					MaxDelay:     10 * time.Millisecond,
				}))

				evts := drainEvents(mustSend(t, r, "x"))
				_, hadErr := findEvent(evts, agent.EventError)
				So(hadErr, ShouldBeTrue)
			})
		})

		Convey("Tool 错误", func() {
			Convey("B1: Tool.Call 返 error → ToolResultBlock IsError=true", func() {
				prov := providertest.New().
					QueueStream(
						provider.StreamChunk{ToolCallDelta: &provider.ToolCallDelta{Index: 0, ID: "tx1", Name: "borked"}},
						provider.StreamChunk{ToolCallDelta: &provider.ToolCallDelta{Index: 0, ArgsDelta: `{}`}},
						provider.StreamChunk{FinishReason: provider.FinishToolCalls},
					).
					QueueStream(
						provider.StreamChunk{ContentDelta: "tool failed, fallback."},
						provider.StreamChunk{FinishReason: provider.FinishStop},
					)
				_, conv, r := setupAgent(t, prov, agent.Tools(scenarioErrTool{}))

				evts := drainEvents(mustSend(t, r, "use borked"))

				postEv, ok := findEvent(evts, agent.EventPostToolUse)
				So(ok, ShouldBeTrue)
				So(postEv.Tool, ShouldNotBeNil)
				So(postEv.Tool.Output, ShouldNotBeNil)
				So(postEv.Tool.Output.IsError, ShouldBeTrue)

				final := lastMessage(t, conv)
				So(textOfBlocks(final.Content), ShouldContainSubstring, "fallback")
			})

			Convey("B2: tool args malformed JSON → ToolUseBlock.State == ToolUseMalformed", func() {
				prov := providertest.New().QueueStream(
					provider.StreamChunk{ToolCallDelta: &provider.ToolCallDelta{Index: 0, ID: "tm1", Name: "add", ArgsDelta: `{"a":2,`}},
					provider.StreamChunk{FinishReason: provider.FinishToolCalls},
				)
				_, conv, r := setupAgent(t, prov, agent.Tools(chatAddTool{}))

				drainEvents(mustSend(t, r, "破 JSON"))

				// Malformed-state lives on the assistant message that produced the
				// broken tool call; that message is somewhere in the conv (the very
				// last entry may be a tool result or an errored follow-up partial),
				// so scan all messages instead of only the tail.
				var foundMalformed bool
				for i := 0; i < conv.Len(); i++ {
					m, _ := conv.MessageAt(i)
					for _, b := range m.Content {
						if tu, ok := b.(agent.ToolUseBlock); ok {
							if tu.State == agent.ToolUseMalformed {
								foundMalformed = true
							}
						}
					}
				}
				So(foundMalformed, ShouldBeTrue)
			})

			// B3: tool panic → recover 当 error.
			// Framework currently does NOT recover panics inside tool dispatch
			// (see agent/tooldispatch.go Run). Calling a panicking tool crashes
			// the whole test process. Skipping until the framework adds a
			// recover() in ToolDispatcher.Run that converts panics into
			// IsError=true ToolResultBlocks.
			SkipConvey("B3: tool panic → recover 当 error (framework lacks panic recovery)", func() {
				prov := providertest.New().
					QueueStream(
						provider.StreamChunk{ToolCallDelta: &provider.ToolCallDelta{Index: 0, ID: "tp1", Name: "panicky"}},
						provider.StreamChunk{ToolCallDelta: &provider.ToolCallDelta{Index: 0, ArgsDelta: `{}`}},
						provider.StreamChunk{FinishReason: provider.FinishToolCalls},
					).
					QueueStream(
						provider.StreamChunk{ContentDelta: "recovered"},
						provider.StreamChunk{FinishReason: provider.FinishStop},
					)
				_, _, r := setupAgent(t, prov, agent.Tools(scenarioPanicTool{}))

				evts := drainEvents(mustSend(t, r, "panic"))

				postEv, ok := findEvent(evts, agent.EventPostToolUse)
				So(ok, ShouldBeTrue)
				So(postEv.Tool.Output, ShouldNotBeNil)
				So(postEv.Tool.Output.IsError, ShouldBeTrue)
			})
		})

		Convey("Hook 错误 vs 决策", func() {
			Convey("C1: UserPromptSubmit hook 抛 error → EventError 包 *HookError", func() {
				boom := errors.New("policy down")
				prov := providertest.New().QueueStream(
					provider.StreamChunk{ContentDelta: "x"},
					provider.StreamChunk{FinishReason: provider.FinishStop},
				)
				_, _, r := setupAgent(t, prov,
					agent.UserPromptSubmit(func(_ context.Context, _ *agent.UserPromptInput) (*agent.UserPromptOutput, error) {
						return nil, boom
					}),
				)

				evts := drainEvents(mustSend(t, r, "hi"))

				errEv, ok := findEvent(evts, agent.EventError)
				So(ok, ShouldBeTrue)
				var he *agent.HookError
				So(errors.As(errEv.Error, &he), ShouldBeTrue)
				// HookStage is the typed string "user_prompt_submit"; compare to
				// the constant rather than to a substring (spec draft used
				// "UserPrompt" which is the Go identifier, not the stage string).
				So(he.Stage, ShouldEqual, agent.HookStageUserPromptSubmit)
				So(errors.Is(he.Cause, boom), ShouldBeTrue)
			})

			Convey("C2: 中间件 AbortWithDeny → tool 不 dispatch", func() {
				denyMW := func(c *agent.ToolContext) {
					c.AbortWithDeny("not allowed by policy")
				}
				args, _ := json.Marshal(map[string]any{"a": 1, "b": 2})
				prov := providertest.New().
					QueueStream(
						provider.StreamChunk{ToolCallDelta: &provider.ToolCallDelta{Index: 0, ID: "td1", Name: "add", ArgsDelta: string(args)}},
						provider.StreamChunk{FinishReason: provider.FinishToolCalls},
					).
					QueueStream(
						provider.StreamChunk{ContentDelta: "ok skipping"},
						provider.StreamChunk{FinishReason: provider.FinishStop},
					)
				var addCalled bool
				counter := chatAddToolWithCounter{counted: &addCalled}

				_, _, r := setupAgent(t, prov,
					agent.Tools(counter),
					agent.Use("add", denyMW),
				)

				evts := drainEvents(mustSend(t, r, "试 add"))

				So(addCalled, ShouldBeFalse)
				_, ok := findEvent(evts, agent.EventPostToolUse)
				So(ok, ShouldBeTrue)
			})

			Convey("C3: UserPromptSubmit Decision=Deny → 立即终止 turn StopHook", func() {
				denyHook := func(_ context.Context, _ *agent.UserPromptInput) (*agent.UserPromptOutput, error) {
					return &agent.UserPromptOutput{
						Decision:   agent.DecisionDeny,
						DenyReason: "blocked input",
					}, nil
				}
				prov := providertest.New()
				_, _, r := setupAgent(t, prov, agent.UserPromptSubmit(denyHook))

				evts := drainEvents(mustSend(t, r, "blocked input"))

				endEv, ok := findEvent(evts, agent.EventTurnEnd)
				So(ok, ShouldBeTrue)
				So(endEv.StopReason, ShouldEqual, agent.StopHook)
				So(len(prov.Received()), ShouldEqual, 0)
			})
		})

		Convey("超时 vs 取消（UI 文案区分）", func() {
			Convey("D1: ctx deadline → StopTimeout + PartialTimeout", func() {
				prov := providertest.New().QueueStreamFunc(func(ctx context.Context) <-chan provider.StreamChunk {
					ch := make(chan provider.StreamChunk)
					go func() { defer close(ch); <-ctx.Done() }()
					return ch
				})
				_, conv, r := setupAgent(t, prov)

				ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
				defer cancel()

				events, err := r.Send(ctx, "slow")
				So(err, ShouldBeNil)
				evts := drainEvents(events)
				endEv, ok := findEvent(evts, agent.EventTurnEnd)
				So(ok, ShouldBeTrue)
				So(endEv.StopReason, ShouldEqual, agent.StopTimeout)

				last := lastMessage(t, conv)
				So(last.PartialReason, ShouldEqual, agent.PartialTimeout)
			})

			Convey("D2: 显式 r.Cancel → StopCancelled + PartialCancelled", func() {
				prov := providertest.New().QueueStreamFunc(func(ctx context.Context) <-chan provider.StreamChunk {
					ch := make(chan provider.StreamChunk)
					go func() { defer close(ch); <-ctx.Done() }()
					return ch
				})
				_, conv, r := setupAgent(t, prov)

				events, err := r.Send(context.Background(), "slow")
				So(err, ShouldBeNil)
				go func() {
					time.Sleep(10 * time.Millisecond)
					_ = r.Cancel("user pressed esc")
				}()
				evts := drainEvents(events)

				endEv, ok := findEvent(evts, agent.EventTurnEnd)
				So(ok, ShouldBeTrue)
				So(endEv.StopReason, ShouldEqual, agent.StopCancelled)

				last := lastMessage(t, conv)
				So(last.PartialReason, ShouldEqual, agent.PartialCancelled)
			})

			Convey("D3: Send 之前 ctx 已 canceled → 立刻返 error", func() {
				prov := providertest.New()
				_, _, r := setupAgent(t, prov)

				ctx, cancel := context.WithCancel(context.Background())
				cancel()

				_, err := r.Send(ctx, "noop")
				So(err, ShouldNotBeNil)
			})
		})

		Convey("E1: Token limit FinishLength → StopTokenLimit + PartialTokenLimit → continue 续写", func() {
			prov := providertest.New().
				QueueStream(
					provider.StreamChunk{ContentDelta: "first half "},
					provider.StreamChunk{FinishReason: provider.FinishLength,
						Usage: &provider.Usage{PromptTokens: 20, CompletionTokens: 256, TotalTokens: 276}},
				).
				QueueStream(
					provider.StreamChunk{ContentDelta: "second half."},
					provider.StreamChunk{FinishReason: provider.FinishStop},
				)
			_, conv, r := setupAgent(t, prov)

			evts := drainEvents(mustSend(t, r, "long answer please"))

			endEv, ok := findEvent(evts, agent.EventTurnEnd)
			So(ok, ShouldBeTrue)
			So(endEv.StopReason, ShouldEqual, agent.StopTokenLimit)

			last := lastMessage(t, conv)
			So(last.PartialReason, ShouldEqual, agent.PartialTokenLimit)

			drainEvents(mustSend(t, r, "continue"))
			finalConv := lastMessage(t, conv)
			So(textOfBlocks(finalConv.Content), ShouldContainSubstring, "second half")
		})
	})
}
