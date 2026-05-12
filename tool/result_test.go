package tool

import (
	"testing"

	agent "github.com/cago-frame/agents/agent"
)

func TestTextResult(t *testing.T) {
	got := TextResult("hello")
	if got.IsError {
		t.Fatalf("expected IsError=false")
	}
	if len(got.Content) != 1 {
		t.Fatalf("expected 1 block, got %d", len(got.Content))
	}
	tb, ok := got.Content[0].(agent.TextBlock)
	if !ok || tb.Text != "hello" {
		t.Fatalf("expected TextBlock{hello}, got %#v", got.Content[0])
	}
}

func TestErrorResult(t *testing.T) {
	got := ErrorResult("boom")
	if !got.IsError {
		t.Fatal("expected IsError=true")
	}
	if got.Content[0].(agent.TextBlock).Text != "boom" {
		t.Fatalf("got %#v", got.Content[0])
	}
}

func TestMultimodalResult(t *testing.T) {
	got := MultimodalResult(
		agent.TextBlock{Text: "see image:"},
		agent.ImageBlock{MediaType: "image/png"},
	)
	if got.IsError {
		t.Fatal("expected IsError=false")
	}
	if len(got.Content) != 2 {
		t.Fatalf("expected 2 blocks, got %d", len(got.Content))
	}
}
