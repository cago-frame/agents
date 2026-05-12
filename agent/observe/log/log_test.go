package log_test

import (
	"context"
	"testing"

	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"

	agent "github.com/cago-frame/agents/agent"
	logplugin "github.com/cago-frame/agents/agent/observe/log"
	"github.com/cago-frame/agents/provider"
	"github.com/cago-frame/agents/provider/providertest"
)

func TestLog_PluginRecordsLifecycleEvents(t *testing.T) {
	t.Parallel()
	core, recorded := observer.New(zap.InfoLevel)
	logger := zap.New(core)

	prov := providertest.New().QueueStream(
		provider.StreamChunk{ContentDelta: "answer"},
		provider.StreamChunk{FinishReason: provider.FinishStop},
	)

	a := agent.New(prov, logplugin.Plugin(logplugin.WithLogger(logger)))
	conv := agent.NewConversation()
	r := a.Runner(conv)
	defer func() { _ = r.Close() }()

	events, _ := r.Send(context.Background(), "ping")
	for range events {
	}

	wantPrefixes := map[string]bool{"turn_end": false, "message_end": false}
	for _, entry := range recorded.All() {
		for k := range wantPrefixes {
			if entry.Message == k {
				wantPrefixes[k] = true
			}
		}
	}
	for k, ok := range wantPrefixes {
		if !ok {
			t.Errorf("expected log entry %q not seen", k)
		}
	}
}

func TestLog_DeltasSilentByDefault(t *testing.T) {
	t.Parallel()
	core, recorded := observer.New(zap.InfoLevel)
	logger := zap.New(core)

	prov := providertest.New().QueueStream(
		provider.StreamChunk{ContentDelta: "abc"},
		provider.StreamChunk{ContentDelta: "def"},
		provider.StreamChunk{FinishReason: provider.FinishStop},
	)

	a := agent.New(prov, logplugin.Plugin(logplugin.WithLogger(logger)))
	conv := agent.NewConversation()
	r := a.Runner(conv)
	defer func() { _ = r.Close() }()
	events, _ := r.Send(context.Background(), "go")
	for range events {
	}

	for _, entry := range recorded.All() {
		if entry.Message == "text_delta" {
			t.Fatalf("text_delta logged at Info without WithDeltas: %+v", entry)
		}
	}
}

func TestLog_WithDeltasEnablesDeltaLog(t *testing.T) {
	t.Parallel()
	core, recorded := observer.New(zap.DebugLevel)
	logger := zap.New(core)

	prov := providertest.New().QueueStream(
		provider.StreamChunk{ContentDelta: "abc"},
		provider.StreamChunk{FinishReason: provider.FinishStop},
	)

	a := agent.New(prov, logplugin.Plugin(logplugin.WithLogger(logger), logplugin.WithDeltas()))
	conv := agent.NewConversation()
	r := a.Runner(conv)
	defer func() { _ = r.Close() }()
	events, _ := r.Send(context.Background(), "go")
	for range events {
	}

	var sawDelta bool
	for _, entry := range recorded.All() {
		if entry.Message == "text_delta" {
			sawDelta = true
			break
		}
	}
	if !sawDelta {
		t.Fatal("text_delta expected with WithDeltas")
	}
}
