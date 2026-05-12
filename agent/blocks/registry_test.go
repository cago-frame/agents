package blocks_test

import (
	"encoding/json"
	"testing"

	"github.com/cago-frame/agents/agent/blocks"
)

func TestEncodeDecode_TextBlock_RoundTrip(t *testing.T) {
	in := blocks.TextBlock{Text: "hello"}
	sb, err := blocks.Encode(in)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	if sb.Type != "text" {
		t.Fatalf("Type = %q, want text", sb.Type)
	}
	out, err := blocks.Decode(sb)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	tb, ok := out.(blocks.TextBlock)
	if !ok {
		t.Fatalf("Decode returned %T, want TextBlock", out)
	}
	if tb.Text != in.Text {
		t.Fatalf("round-trip text = %q, want %q", tb.Text, in.Text)
	}
}

func TestEncodeDecode_DisplayTextBlock_RoundTrip(t *testing.T) {
	in := blocks.DisplayTextBlock{Text: "@srv1 status"}
	sb, _ := blocks.Encode(in)
	out, err := blocks.Decode(sb)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	dt, ok := out.(blocks.DisplayTextBlock)
	if !ok {
		t.Fatalf("Decode returned %T, want DisplayTextBlock", out)
	}
	if dt.Text != in.Text {
		t.Fatalf("text mismatch: %q vs %q", dt.Text, in.Text)
	}
}

func TestEncodeDecode_ToolResult_NestedBlocksKeepTypes(t *testing.T) {
	in := blocks.ToolResultBlock{
		ToolUseID: "tu_1",
		Content: []blocks.ContentBlock{
			blocks.TextBlock{Text: "ok"},
			blocks.RefBlock{Kind: "file", ID: "main.go", Label: "main.go"},
		},
	}
	sb, err := blocks.Encode(in)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	out, err := blocks.Decode(sb)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	tr, ok := out.(blocks.ToolResultBlock)
	if !ok {
		t.Fatalf("Decode returned %T, want ToolResultBlock", out)
	}
	if len(tr.Content) != 2 {
		t.Fatalf("nested content len = %d, want 2", len(tr.Content))
	}
	if _, ok := tr.Content[0].(blocks.TextBlock); !ok {
		t.Fatalf("nested[0] = %T, want TextBlock", tr.Content[0])
	}
	if _, ok := tr.Content[1].(blocks.RefBlock); !ok {
		t.Fatalf("nested[1] = %T, want RefBlock — type info was lost", tr.Content[1])
	}
}

func TestEncodeDecodeAll_PreservesAudience(t *testing.T) {
	// Sanity that the projection layer can still filter by audience after a
	// typed-block JSON round-trip — the decoded slice has the same type
	// identities, so the Audience() methods keep working.
	in := []blocks.ContentBlock{
		blocks.TextBlock{Text: "hello"},
		blocks.DisplayTextBlock{Text: "@you"},
		blocks.ThinkingBlock{Text: "..."},
	}
	sbs, err := blocks.EncodeAll(in)
	if err != nil {
		t.Fatalf("EncodeAll: %v", err)
	}
	out, err := blocks.DecodeAll(sbs)
	if err != nil {
		t.Fatalf("DecodeAll: %v", err)
	}
	want := []blocks.AudienceMask{blocks.ToAll, blocks.ToUI, blocks.ToLLM}
	for i, b := range out {
		if b.Audience() != want[i] {
			t.Errorf("out[%d] (%s) audience=%b, want=%b", i, b.Type(), b.Audience(), want[i])
		}
	}
}

func TestDecode_UnknownType_Errors(t *testing.T) {
	_, err := blocks.Decode(blocks.StoredBlock{Type: "no-such-type", Data: []byte(`{}`)})
	if err == nil {
		t.Fatal("expected error for unknown type")
	}
}

func TestRegister_DuplicateTypePanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("expected panic on duplicate registration")
		}
	}()
	blocks.Register("text", func(_ []byte) (blocks.ContentBlock, error) {
		return blocks.TextBlock{}, nil
	})
}

func TestJSONShape_HasTypeDiscriminator(t *testing.T) {
	// Smoke test that the on-wire JSON carries a type field. This is what
	// makes JSON rehydration type-safe — without it, a persisted Content
	// slice would lose its variants on reload.
	sb, _ := blocks.Encode(blocks.TextBlock{Text: "x"})
	raw, _ := json.Marshal(sb)
	got := string(raw)
	if want := `"type":"text"`; !contains(got, want) {
		t.Fatalf("encoded JSON %s missing %s", got, want)
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
