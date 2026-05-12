package websearch_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	agent "github.com/cago-frame/agents/agent"
	"github.com/cago-frame/agents/tool"
	"github.com/cago-frame/agents/tool/websearch"
)

func callTool(t *testing.T, tl tool.Tool, args map[string]any) (*agent.ToolResultBlock, error) {
	t.Helper()
	return tl.Call(context.Background(), args)
}

func mustCall(t *testing.T, tl tool.Tool, args map[string]any) string {
	t.Helper()
	out, err := callTool(t, tl, args)
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	if out.IsError {
		t.Fatalf("tool error: %s", out.Content[0].(agent.TextBlock).Text)
	}
	return out.Content[0].(agent.TextBlock).Text
}

func mustCallError(t *testing.T, tl tool.Tool, args map[string]any) string {
	t.Helper()
	out, err := callTool(t, tl, args)
	if err != nil {
		t.Fatalf("unexpected Go error: %v", err)
	}
	if !out.IsError {
		t.Fatalf("expected tool error, got success: %s", out.Content[0].(agent.TextBlock).Text)
	}
	return out.Content[0].(agent.TextBlock).Text
}

func TestNoProviderConfigured(t *testing.T) {
	msg := mustCallError(t, websearch.New(), map[string]any{"query": "x"})
	if !strings.Contains(msg, "no provider configured") {
		t.Fatalf("got %q", msg)
	}
}

func TestEmptyQuery(t *testing.T) {
	mp := &websearch.MockProvider{}
	msg := mustCallError(t, websearch.New(websearch.WithProvider(mp)), map[string]any{"query": "  "})
	if !strings.Contains(msg, "query must not be empty") {
		t.Fatalf("got %q", msg)
	}
}

func TestProviderError(t *testing.T) {
	mp := &websearch.MockProvider{Err: errors.New("boom")}
	_, err := callTool(t, websearch.New(websearch.WithProvider(mp)), map[string]any{"query": "x"})
	if err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("got err=%v", err)
	}
}

func TestFormatResults(t *testing.T) {
	mp := &websearch.MockProvider{Results: []websearch.Result{
		{URL: "https://a.example.com/1", Title: "First", Snippet: "snip1"},
		{URL: "https://b.example.com/2", Title: "Second", Snippet: "snip2", PageAge: "3d"},
	}}
	out := mustCall(t, websearch.New(websearch.WithProvider(mp)), map[string]any{"query": "x"})
	if !strings.Contains(out, "1. [First](https://a.example.com/1) — snip1") {
		t.Fatalf("missing first row: %q", out)
	}
	if !strings.Contains(out, "2. [Second](https://b.example.com/2) — snip2 (page age: 3d)") {
		t.Fatalf("missing second row with page age: %q", out)
	}
}

func TestEmptyResults(t *testing.T) {
	mp := &websearch.MockProvider{Results: nil}
	out := mustCall(t, websearch.New(websearch.WithProvider(mp)), map[string]any{"query": "x"})
	if out != "[no results]" {
		t.Fatalf("got %q", out)
	}
}

func TestMaxResultsClamp(t *testing.T) {
	mp := &websearch.MockProvider{Results: []websearch.Result{
		{URL: "https://a.com/1", Title: "1"},
		{URL: "https://a.com/2", Title: "2"},
		{URL: "https://a.com/3", Title: "3"},
	}}
	tl := websearch.New(websearch.WithProvider(mp), websearch.MaxResults(2))
	out := mustCall(t, tl, map[string]any{"query": "x", "max_results": 100})

	// max_results clamp 到 2 — provider 收到 2,返回 2 条;输出只 2 行
	if strings.Count(out, "\n") != 1 {
		t.Fatalf("expected 2 lines (1 newline), got %q", out)
	}
}

func TestDefaultMaxResults(t *testing.T) {
	mp := &websearch.MockProvider{Results: []websearch.Result{
		{URL: "https://a.com/1", Title: "1"},
		{URL: "https://a.com/2", Title: "2"},
	}}
	tl := websearch.New(websearch.WithProvider(mp), websearch.DefaultMaxResults(2))
	out := mustCall(t, tl, map[string]any{"query": "x"})
	if !strings.Contains(out, "1. [1]") || !strings.Contains(out, "2. [2]") {
		t.Fatalf("got %q", out)
	}
}

func TestBlockedDomain(t *testing.T) {
	mp := &websearch.MockProvider{Results: []websearch.Result{
		{URL: "https://bad.example.com/x", Title: "B"},
		{URL: "https://good.com/x", Title: "G"},
	}}
	tl := websearch.New(websearch.WithProvider(mp), websearch.BlockedDomains("example.com"))
	out := mustCall(t, tl, map[string]any{"query": "x"})
	if strings.Contains(out, "[B]") {
		t.Fatalf("blocked domain leaked: %q", out)
	}
	if !strings.Contains(out, "[G]") {
		t.Fatalf("good result missing: %q", out)
	}
}

func TestAllowedDomain(t *testing.T) {
	mp := &websearch.MockProvider{Results: []websearch.Result{
		{URL: "https://example.com/x", Title: "ex"},
		{URL: "https://other.com/x", Title: "ot"},
	}}
	tl := websearch.New(websearch.WithProvider(mp), websearch.AllowedDomains("example.com"))
	out := mustCall(t, tl, map[string]any{"query": "x"})
	if strings.Contains(out, "[ot]") {
		t.Fatalf("non-allowed leaked: %q", out)
	}
	if !strings.Contains(out, "[ex]") {
		t.Fatalf("allowed missing: %q", out)
	}
}

func TestSchemaShape(t *testing.T) {
	tl := websearch.New()
	if tl.Name() != websearch.Name {
		t.Fatalf("name = %q", tl.Name())
	}
	sc := tl.Schema()
	if sc.Type != "object" {
		t.Fatalf("schema type = %q", sc.Type)
	}
	if _, ok := sc.Properties["query"]; !ok {
		t.Fatal("missing query in schema properties")
	}
	if _, ok := sc.Properties["max_results"]; !ok {
		t.Fatal("missing max_results in schema properties")
	}
}

func TestSerialOption(t *testing.T) {
	tl := websearch.New(websearch.Serial())
	st, ok := tl.(agent.SerialTool)
	if !ok {
		t.Fatal("tool does not implement SerialTool")
	}
	if !st.Serial() {
		t.Fatal("Serial() should be true")
	}
}
