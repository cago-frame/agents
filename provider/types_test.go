package provider

import (
	"encoding/json"
	"errors"
	"testing"
)

func TestProviderError_UnwrapAndRetryAfter(t *testing.T) {
	inner := errors.New("rate limited")
	pe := &ProviderError{Err: inner, RetryAfter: "5", StatusCode: 429}
	if !errors.Is(pe, inner) {
		t.Fatal("ProviderError must wrap inner via errors.Is")
	}
	var got *ProviderError
	if !errors.As(pe, &got) {
		t.Fatal("ProviderError must satisfy errors.As")
	}
	if got.RetryAfter != "5" || got.StatusCode != 429 {
		t.Fatalf("fields lost: %+v", got)
	}
	if pe.Error() != "rate limited" {
		t.Fatalf("Error() = %q, want %q", pe.Error(), "rate limited")
	}
}

func TestMessage_MultiContent_Roundtrip(t *testing.T) {
	m := Message{
		Role: RoleUser,
		MultiContent: []MessagePart{
			{Type: MessagePartText, Text: "look at this:"},
			{Type: MessagePartImage, Image: &MessageImage{
				URL: "https://example.com/cat.png",
			}},
			{Type: MessagePartImage, Image: &MessageImage{
				MediaType: "image/png",
				Inline:    []byte{0x89, 0x50, 0x4e, 0x47},
			}},
		},
	}
	raw, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var back Message
	if err := json.Unmarshal(raw, &back); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(back.MultiContent) != 3 {
		t.Fatalf("MultiContent len = %d, want 3", len(back.MultiContent))
	}
	if back.MultiContent[0].Text != "look at this:" {
		t.Fatalf("part 0 text = %q", back.MultiContent[0].Text)
	}
	if back.MultiContent[1].Image == nil || back.MultiContent[1].Image.URL != "https://example.com/cat.png" {
		t.Fatalf("part 1 image = %+v", back.MultiContent[1].Image)
	}
	if back.MultiContent[2].Image == nil || back.MultiContent[2].Image.MediaType != "image/png" {
		t.Fatalf("part 2 image = %+v", back.MultiContent[2].Image)
	}
}
