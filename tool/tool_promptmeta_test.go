package tool

import (
	"reflect"
	"testing"
)

func TestRawTool_PromptMeta_DefaultEmpty(t *testing.T) {
	r := &RawTool{NameStr: "x", DescStr: "desc"}
	if got := r.PromptSnippet(); got != "" {
		t.Fatalf("PromptSnippet default: want empty, got %q", got)
	}
	if got := r.PromptGuidelines(); got != nil {
		t.Fatalf("PromptGuidelines default: want nil, got %v", got)
	}
}

func TestRawTool_PromptMeta_FieldsExposed(t *testing.T) {
	r := &RawTool{
		NameStr:             "read",
		PromptSnippetStr:    "Read file contents",
		PromptGuidelinesArr: []string{"Read before edit", "No cat/sed"},
	}
	if got := r.PromptSnippet(); got != "Read file contents" {
		t.Fatalf("PromptSnippet: %q", got)
	}
	if got := r.PromptGuidelines(); !reflect.DeepEqual(got, []string{"Read before edit", "No cat/sed"}) {
		t.Fatalf("PromptGuidelines: %v", got)
	}
}

func TestPromptMetaOption_WithPromptMeta(t *testing.T) {
	r := &RawTool{NameStr: "x"}
	WithPromptMeta("snippet", []string{"g1"})(r)
	if r.PromptSnippetStr != "snippet" {
		t.Fatalf("snippet not set: %q", r.PromptSnippetStr)
	}
	if !reflect.DeepEqual(r.PromptGuidelinesArr, []string{"g1"}) {
		t.Fatalf("guidelines not set: %v", r.PromptGuidelinesArr)
	}
}
