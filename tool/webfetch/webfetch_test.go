package webfetch_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	agent "github.com/cago-frame/agents/agent"
	"github.com/cago-frame/agents/provider"
	"github.com/cago-frame/agents/provider/providertest"
	"github.com/cago-frame/agents/tool"
	"github.com/cago-frame/agents/tool/webfetch"
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

func TestFetchHTMLConvertedToMarkdown(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(`<html><body><h1>Title</h1><p>Hello <a href="https://example.com">world</a></p></body></html>`))
	}))
	defer srv.Close()

	tl := webfetch.New()
	out := mustCall(t, tl, map[string]any{"url": srv.URL})

	if !strings.Contains(out, "# Title") {
		t.Fatalf("expected H1 markdown, got %q", out)
	}
	if !strings.Contains(out, "[world](https://example.com)") {
		t.Fatalf("expected markdown link, got %q", out)
	}
}

func TestFetchJSONIsPretty(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"a":1,"b":[2,3]}`))
	}))
	defer srv.Close()

	out := mustCall(t, webfetch.New(), map[string]any{"url": srv.URL})
	if !strings.Contains(out, "\"a\": 1") || !strings.Contains(out, "\"b\":") {
		t.Fatalf("expected indented json, got %q", out)
	}
}

func TestFetchPlainTextRaw(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte("hello plain"))
	}))
	defer srv.Close()

	out := mustCall(t, webfetch.New(), map[string]any{"url": srv.URL})
	if out != "hello plain" {
		t.Fatalf("got %q", out)
	}
}

func TestFetchXMLRaw(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/xml")
		_, _ = w.Write([]byte(`<root><a>1</a></root>`))
	}))
	defer srv.Close()

	out := mustCall(t, webfetch.New(), map[string]any{"url": srv.URL})
	if !strings.Contains(out, "<root>") {
		t.Fatalf("expected raw xml, got %q", out)
	}
}

func TestFetchBinaryRejected(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/pdf")
		_, _ = w.Write([]byte("%PDF-1.4..."))
	}))
	defer srv.Close()

	msg := mustCallError(t, webfetch.New(), map[string]any{"url": srv.URL})
	if !strings.Contains(msg, "unsupported content-type") {
		t.Fatalf("unexpected error message: %q", msg)
	}
}

func TestFetch404IsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	msg := mustCallError(t, webfetch.New(), map[string]any{"url": srv.URL})
	if !strings.Contains(msg, "404") {
		t.Fatalf("expected 404 in error message, got %q", msg)
	}
}

func TestFetchNoContentTypeSniffsHTML(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "")
		_, _ = w.Write([]byte("<html><body><h1>Sniffed</h1></body></html>"))
	}))
	defer srv.Close()

	out := mustCall(t, webfetch.New(), map[string]any{"url": srv.URL})
	if !strings.Contains(out, "# Sniffed") {
		t.Fatalf("expected H1 from sniffed html, got %q", out)
	}
}

func TestFetchMaxBytesExceeded(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte(strings.Repeat("a", 5000)))
	}))
	defer srv.Close()

	msg := mustCallError(t, webfetch.New(webfetch.MaxBytes(100)), map[string]any{"url": srv.URL})
	if !strings.Contains(msg, "MaxBytes") {
		t.Fatalf("expected MaxBytes in error message, got %q", msg)
	}
}

func TestFetchBlockedDomain(t *testing.T) {
	tl := webfetch.New(webfetch.BlockedDomains("example.com"))
	msg := mustCallError(t, tl, map[string]any{"url": "https://www.example.com/x"})
	if !strings.Contains(msg, "blocked") {
		t.Fatalf("expected blocked error, got %q", msg)
	}
}

func TestFetchAllowedDomainSubdomain(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()

	// allowlist 不命中 httptest 的 127.0.0.1,应该报错
	tl := webfetch.New(webfetch.AllowedDomains("example.com"))
	msg := mustCallError(t, tl, map[string]any{"url": srv.URL})
	if !strings.Contains(msg, "allowlist") {
		t.Fatalf("expected allowlist error, got %q", msg)
	}
}

func TestFetchEmptyURL(t *testing.T) {
	msg := mustCallError(t, webfetch.New(), map[string]any{"url": ""})
	if !strings.Contains(msg, "url must not be empty") {
		t.Fatalf("expected empty url error, got %q", msg)
	}
}

func TestFetchInvalidScheme(t *testing.T) {
	msg := mustCallError(t, webfetch.New(), map[string]any{"url": "ftp://example.com/x"})
	if !strings.Contains(msg, "unsupported scheme") {
		t.Fatalf("expected scheme error, got %q", msg)
	}
}

func TestFetchProviderExtraction(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte("<p>The answer is 42.</p>"))
	}))
	defer srv.Close()

	mock := providertest.New().Queue(&provider.CompletionResponse{
		Content:      "Answer: 42",
		FinishReason: provider.FinishStop,
	})
	tl := webfetch.New(webfetch.WithProvider(mock))

	out := mustCall(t, tl, map[string]any{"url": srv.URL, "prompt": "what is the answer?"})
	if out != "Answer: 42" {
		t.Fatalf("expected provider extraction, got %q", out)
	}
}

func TestFetchProviderIgnoredWithoutPrompt(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte("<h1>Hello</h1>"))
	}))
	defer srv.Close()

	mock := providertest.New().Queue(&provider.CompletionResponse{
		Content:      "should not be returned",
		FinishReason: provider.FinishStop,
	})
	tl := webfetch.New(webfetch.WithProvider(mock))

	out := mustCall(t, tl, map[string]any{"url": srv.URL})
	if !strings.Contains(out, "# Hello") {
		t.Fatalf("expected raw markdown when no prompt, got %q", out)
	}
}

func TestFetchCacheHit(t *testing.T) {
	hits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits++
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte("v1"))
	}))
	defer srv.Close()

	tl := webfetch.New(webfetch.Cache(1 * time.Minute))
	first := mustCall(t, tl, map[string]any{"url": srv.URL})
	second := mustCall(t, tl, map[string]any{"url": srv.URL})

	if first != "v1" || second != "v1" {
		t.Fatalf("got %q / %q", first, second)
	}
	if hits != 1 {
		t.Fatalf("expected cache hit (1 server hit), got %d hits", hits)
	}
}

func TestFetchCacheExpiry(t *testing.T) {
	hits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits++
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte("v"))
	}))
	defer srv.Close()

	tl := webfetch.New(webfetch.Cache(1 * time.Millisecond))
	mustCall(t, tl, map[string]any{"url": srv.URL})
	time.Sleep(20 * time.Millisecond)
	mustCall(t, tl, map[string]any{"url": srv.URL})

	if hits != 2 {
		t.Fatalf("expected 2 server hits after expiry, got %d", hits)
	}
}

func TestFetchSchemaFields(t *testing.T) {
	tl := webfetch.New()
	if tl.Name() != webfetch.Name {
		t.Fatalf("name = %q", tl.Name())
	}
	sc := tl.Schema()
	if sc.Type != "object" {
		t.Fatalf("schema type = %q", sc.Type)
	}
	if _, ok := sc.Properties["url"]; !ok {
		t.Fatal("schema missing url")
	}
	if _, ok := sc.Properties["prompt"]; !ok {
		t.Fatal("schema missing prompt")
	}
	if len(sc.Required) != 1 || sc.Required[0] != "url" {
		t.Fatalf("required = %v", sc.Required)
	}
}

func TestFetchHTMLLenientWithBrokenMarkup(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte("<div><p>broken<a href><span>nested"))
	}))
	defer srv.Close()

	out := mustCall(t, webfetch.New(), map[string]any{"url": srv.URL})
	if !strings.Contains(out, "broken") {
		t.Fatalf("expected text from broken html, got %q", out)
	}
}

func TestSerialOption(t *testing.T) {
	tl := webfetch.New(webfetch.Serial())
	st, ok := tl.(agent.SerialTool)
	if !ok {
		t.Fatal("tool does not implement SerialTool")
	}
	if !st.Serial() {
		t.Fatal("Serial() should be true")
	}
}
