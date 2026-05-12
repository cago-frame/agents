package claudecode

import (
	"encoding/json"
	"testing"
)

func TestStringifyToolResult(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want string
	}{
		{"empty", ``, ""},
		{"plain string", `"hello"`, "hello"},
		{"text block array", `[{"type":"text","text":"abc"},{"type":"text","text":"def"}]`, "abcdef"},
		{"unknown shape falls back to raw", `{"foo":"bar"}`, `{"foo":"bar"}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := stringifyToolResult(json.RawMessage(tt.raw))
			if got != tt.want {
				t.Errorf("stringifyToolResult(%q) = %q, want %q", tt.raw, got, tt.want)
			}
		})
	}
}
