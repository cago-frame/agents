package coding_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	. "github.com/smartystreets/goconvey/convey"

	"github.com/cago-frame/agents/agent"
	"github.com/cago-frame/agents/app/coding"
	"github.com/cago-frame/agents/provider"
	"github.com/cago-frame/agents/provider/providertest"
)

func mustWriteFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// findSystemPromptInRequest returns the concatenated text of any Messages
// in req that carry RoleSystem. provider.CompletionRequest has no top-level
// system field — agents put the system prompt as a message at the front.
func findSystemPromptInRequest(req *provider.CompletionRequest) string {
	if req == nil {
		return ""
	}
	var b strings.Builder
	for _, m := range req.Messages {
		if m.Role == provider.RoleSystem {
			if b.Len() > 0 {
				b.WriteString("\n")
			}
			b.WriteString(m.Content)
		}
	}
	return b.String()
}

func TestScenario_CodingAssembly(t *testing.T) {
	Convey("Feature: app/coding 装配 + /help + 自动压缩", t, func() {

		Convey("Scenario: 默认装配 + /help builtin 可解析", func() {
			mock := providertest.New()
			sys, err := coding.New(t.Context(), mock, t.TempDir(),
				coding.WithoutContextFiles(), coding.WithoutSkills())
			So(err, ShouldBeNil)
			t.Cleanup(func() { _ = sys.Close(context.Background()) })

			reg := sys.SlashRegistry()
			So(reg, ShouldNotBeNil)
			res, err := reg.Resolve("/help")
			So(err, ShouldBeNil)
			So(res.IsBuiltin, ShouldBeTrue)
		})

		Convey("Scenario: CLAUDE.md 自动注入 + WithoutContextFiles 关掉", func() {
			cwd := t.TempDir()
			mustWriteFile(t, filepath.Join(cwd, "CLAUDE.md"), "PROJ_RULES_FOO")

			// 默认注入
			mock := providertest.New()
			mock.QueueStream(
				provider.StreamChunk{ContentDelta: "ack"},
				provider.StreamChunk{FinishReason: provider.FinishStop},
			)
			sys, err := coding.New(t.Context(), mock, cwd,
				coding.WithoutCompaction(), coding.WithoutSkills(), coding.WithoutSlashCommands())
			So(err, ShouldBeNil)
			t.Cleanup(func() { _ = sys.Close(context.Background()) })

			r := sys.Agent().Runner(agent.NewConversation())
			t.Cleanup(func() { _ = r.Close() })
			events, err := r.Send(context.Background(), "ping")
			So(err, ShouldBeNil)
			for range events {
			}

			received := mock.Received()
			So(len(received), ShouldEqual, 1)
			sysPrompt := findSystemPromptInRequest(received[0])
			So(sysPrompt, ShouldContainSubstring, "PROJ_RULES_FOO")

			// WithoutContextFiles 关掉
			mock2 := providertest.New()
			mock2.QueueStream(
				provider.StreamChunk{ContentDelta: "ack"},
				provider.StreamChunk{FinishReason: provider.FinishStop},
			)
			sys2, err := coding.New(t.Context(), mock2, cwd,
				coding.WithoutContextFiles(),
				coding.WithoutCompaction(), coding.WithoutSkills(), coding.WithoutSlashCommands(),
			)
			So(err, ShouldBeNil)
			t.Cleanup(func() { _ = sys2.Close(context.Background()) })

			r2 := sys2.Agent().Runner(agent.NewConversation())
			t.Cleanup(func() { _ = r2.Close() })
			events2, err := r2.Send(context.Background(), "ping")
			So(err, ShouldBeNil)
			for range events2 {
			}
			received2 := mock2.Received()
			So(len(received2), ShouldEqual, 1)
			sysPrompt2 := findSystemPromptInRequest(received2[0])
			So(sysPrompt2, ShouldNotContainSubstring, "PROJ_RULES_FOO")
		})

		Convey("Scenario: WithCompactionThreshold 触发 → EventCompacted 在 TurnEnd 后", func() {
			mock := providertest.New().
				QueueStream(
					provider.StreamChunk{ContentDelta: "first"},
					provider.StreamChunk{Usage: &provider.Usage{PromptTokens: 200}, FinishReason: provider.FinishStop},
				).
				Queue(&provider.CompletionResponse{
					Role:         provider.RoleAssistant,
					Content:      "compacted summary",
					FinishReason: provider.FinishStop,
				})

			sys, err := coding.New(t.Context(), mock, t.TempDir(),
				coding.WithoutContextFiles(), coding.WithoutSkills(), coding.WithoutSlashCommands(),
				coding.WithCompactionThreshold(100),
			)
			So(err, ShouldBeNil)
			t.Cleanup(func() { _ = sys.Close(context.Background()) })

			conv := agent.NewConversation()
			// 预填 6 条以过 keepRecent 闸门
			for i := 0; i < 3; i++ {
				conv.Append(agent.Message{
					Role:    agent.RoleUser,
					Content: []agent.ContentBlock{agent.TextBlock{Text: "old user msg"}},
				})
				conv.Append(agent.Message{
					Role:    agent.RoleAssistant,
					Content: []agent.ContentBlock{agent.TextBlock{Text: "old assistant msg"}},
				})
			}

			r := sys.Agent().Runner(conv)
			t.Cleanup(func() { _ = r.Close() })
			events, err := r.Send(context.Background(), "ping")
			So(err, ShouldBeNil)

			var sawCompacted bool
			for ev := range events {
				if ev.Kind == agent.EventCompacted {
					sawCompacted = true
				}
			}
			So(sawCompacted, ShouldBeTrue)
		})

		Convey("Scenario: WithSystemTemplate 替换 intro 但保留 tools/guidelines 段", func() {
			mock := providertest.New()
			tmpl := "CUSTOM_INTRO_FOR_TEST\n\n## Available tools\n{{.ToolsList}}\n## Guidelines\n{{.GuidelinesList}}\nCurrent date: {{.Date}}\nCurrent working directory: {{.Cwd}}"
			sys, err := coding.New(t.Context(), mock, t.TempDir(),
				coding.WithoutContextFiles(), coding.WithoutSkills(), coding.WithoutSlashCommands(),
				coding.WithSystemTemplate(tmpl),
			)
			So(err, ShouldBeNil)
			t.Cleanup(func() { _ = sys.Close(context.Background()) })

			r := sys.Agent().Runner(agent.NewConversation())
			t.Cleanup(func() { _ = r.Close() })
			events, err := r.Send(context.Background(), "ping")
			So(err, ShouldBeNil)
			for range events {
			}
			received := mock.Received()
			So(len(received), ShouldEqual, 1)
			sysPrompt := findSystemPromptInRequest(received[0])
			So(sysPrompt, ShouldContainSubstring, "CUSTOM_INTRO_FOR_TEST")
			So(sysPrompt, ShouldNotContainSubstring, coding.SystemIntro)
			So(sysPrompt, ShouldContainSubstring, "- read:")
			So(sysPrompt, ShouldContainSubstring, "Prefer using tools over speculating")
		})

		Convey("Scenario: 模板解析失败时 coding.New 报错", func() {
			mock := providertest.New()
			_, err := coding.New(t.Context(), mock, t.TempDir(),
				coding.WithoutContextFiles(), coding.WithoutSkills(), coding.WithoutSlashCommands(),
				coding.WithSystemTemplate("{{"),
			)
			So(err, ShouldNotBeNil)
		})
	})
}
