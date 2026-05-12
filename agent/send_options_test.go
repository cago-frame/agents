package agent_test

import (
	"context"
	"errors"
	"testing"

	agent "github.com/cago-frame/agents/agent"
	"github.com/cago-frame/agents/provider/providertest"
)

func TestSend_TextOnly_Setup(t *testing.T) {
	a := agent.New(providertest.New())
	conv := agent.NewConversation()
	r := a.Runner(conv)
	defer func() { _ = r.Close() }()

	// No queued provider response. Send should return setup-success but
	// first event will be EventError (provider has no queued entry).
	_, err := r.Send(context.Background(), "hi")
	if err != nil {
		t.Fatalf("Send setup should succeed: %v", err)
	}
}

func TestSend_AfterClose_Fails(t *testing.T) {
	a := agent.New(providertest.New())
	conv := agent.NewConversation()
	r := a.Runner(conv)
	_ = r.Close()

	_, err := r.Send(context.Background(), "hi")
	if !errors.Is(err, agent.ErrRunnerClosed) {
		t.Fatalf("want ErrRunnerClosed, got %v", err)
	}
}

func TestSend_TextWithBlocks_Conflict(t *testing.T) {
	a := agent.New(providertest.New())
	conv := agent.NewConversation()
	r := a.Runner(conv)
	defer func() { _ = r.Close() }()

	_, err := r.Send(context.Background(), "hi", agent.WithBlocks(agent.TextBlock{Text: "x"}))
	if !errors.Is(err, agent.ErrIncompatibleOption) {
		t.Fatalf("want ErrIncompatibleOption, got %v", err)
	}
}

func TestSendOption_WithImage(t *testing.T) {
	a := agent.New(providertest.New())
	conv := agent.NewConversation()
	r := a.Runner(conv)
	defer func() { _ = r.Close() }()

	img := agent.ImageBlock{MediaType: "image/png"}
	_, err := r.Send(context.Background(), "look", agent.WithImage(img))
	if err != nil {
		t.Fatalf("Send setup should succeed: %v", err)
	}
}
