package agent

import "testing"

func TestWithSendDisplay_AppendsDisplayTextBlock(t *testing.T) {
	blocks, err := buildUserContent("hello", []SendOption{
		WithSendDisplay("hello @srv1"),
	})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(blocks) != 2 {
		t.Fatalf("got %d blocks, want 2", len(blocks))
	}
	if blocks[0].(TextBlock).Text != "hello" {
		t.Fatalf("blocks[0] mismatch: %#v", blocks[0])
	}
	dt, ok := blocks[1].(DisplayTextBlock)
	if !ok || dt.Text != "hello @srv1" {
		t.Fatalf("blocks[1] = %#v, want DisplayTextBlock{Text: hello @srv1}", blocks[1])
	}
	if dt.Audience().Has(AudienceLLM) {
		t.Fatal("DisplayTextBlock from WithSendDisplay must not target the LLM")
	}
}
