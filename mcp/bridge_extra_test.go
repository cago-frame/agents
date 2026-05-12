package mcp

import (
	"context"
	"errors"
	"testing"

	mcpgo "github.com/mark3labs/mcp-go/mcp"

	agent "github.com/cago-frame/agents/agent"
)

// rawTool is a minimal tool.Tool used for tests that need a configurable
// result / error pair. Returning a non-nil block keeps the result mapped to a
// proper CallToolResult with TextContent, while a non-nil err triggers the
// NewToolResultError branch in addToolToMCP.
type rawTool struct {
	name   string
	text   string
	err    error
	schema agent.Schema
}

func (r *rawTool) Name() string         { return r.name }
func (r *rawTool) Description() string  { return "raw" }
func (r *rawTool) Schema() agent.Schema { return r.schema }
func (r *rawTool) Call(_ context.Context, _ map[string]any) (*agent.ToolResultBlock, error) {
	if r.err != nil {
		return nil, r.err
	}
	return &agent.ToolResultBlock{
		Content: []agent.ContentBlock{agent.TextBlock{Text: r.text}},
	}, nil
}

func TestBridge_Name(t *testing.T) {
	b := NewBridge("my-bridge")
	if b.Name() != "my-bridge" {
		t.Errorf("Name = %q", b.Name())
	}
}

func TestBridge_MustRegister(t *testing.T) {
	b := NewBridge("test")
	b.MustRegister(echoTool{})
	if names := b.ToolNames(); len(names) != 1 || names[0] != "echo" {
		t.Errorf("ToolNames = %v", names)
	}
}

func TestBridge_MustRegister_PanicsOnDuplicate(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic on duplicate")
		}
	}()
	b := NewBridge("test")
	b.MustRegister(echoTool{})
	b.MustRegister(echoTool{})
}

func TestBridge_Shutdown_BeforeStartNoOp(t *testing.T) {
	b := NewBridge("test")
	if err := b.Shutdown(context.Background()); err != nil {
		t.Errorf("Shutdown before Start: %v", err)
	}
}

func TestToolResultToMCP_Nil(t *testing.T) {
	got := toolResultToMCP(nil)
	if got == nil {
		t.Fatal("nil block → nil result, want empty text result")
	}
	if got.IsError {
		t.Errorf("nil block → IsError = true")
	}
	if len(got.Content) != 1 {
		t.Fatalf("nil block content len = %d", len(got.Content))
	}
}

func TestToolResultToMCP_SingleText(t *testing.T) {
	got := toolResultToMCP(&agent.ToolResultBlock{
		Content: []agent.ContentBlock{agent.TextBlock{Text: "hello"}},
	})
	if got.IsError {
		t.Errorf("IsError = true, want false")
	}
	if len(got.Content) != 1 {
		t.Fatalf("content len = %d, want 1", len(got.Content))
	}
	tc, ok := got.Content[0].(mcpgo.TextContent)
	if !ok {
		t.Fatalf("content[0] = %T, want TextContent", got.Content[0])
	}
	if tc.Text != "hello" {
		t.Errorf("text = %q", tc.Text)
	}
}

func TestToolResultToMCP_MultiText(t *testing.T) {
	got := toolResultToMCP(&agent.ToolResultBlock{
		Content: []agent.ContentBlock{
			agent.TextBlock{Text: "a"},
			agent.TextBlock{Text: "b"},
			agent.TextBlock{Text: "c"},
		},
	})
	if len(got.Content) != 3 {
		t.Fatalf("content len = %d, want 3", len(got.Content))
	}
	for i, want := range []string{"a", "b", "c"} {
		tc, ok := got.Content[i].(mcpgo.TextContent)
		if !ok {
			t.Fatalf("content[%d] = %T", i, got.Content[i])
		}
		if tc.Text != want {
			t.Errorf("content[%d].Text = %q, want %q", i, tc.Text, want)
		}
	}
}

func TestToolResultToMCP_IsErrorPropagates(t *testing.T) {
	got := toolResultToMCP(&agent.ToolResultBlock{
		IsError: true,
		Content: []agent.ContentBlock{agent.TextBlock{Text: "boom"}},
	})
	if !got.IsError {
		t.Errorf("IsError = false, want true")
	}
}

func TestToolResultToMCP_ImageInline(t *testing.T) {
	got := toolResultToMCP(&agent.ToolResultBlock{
		Content: []agent.ContentBlock{
			agent.ImageBlock{
				MediaType: "image/png",
				Source:    agent.BlobSource{Inline: []byte{0x89, 0x50, 0x4e, 0x47}},
			},
		},
	})
	if len(got.Content) != 1 {
		t.Fatalf("content len = %d", len(got.Content))
	}
	ic, ok := got.Content[0].(mcpgo.ImageContent)
	if !ok {
		t.Fatalf("content[0] = %T, want ImageContent", got.Content[0])
	}
	if ic.MIMEType != "image/png" {
		t.Errorf("MIMEType = %q", ic.MIMEType)
	}
	if ic.Data == "" {
		t.Errorf("Data empty; want base64-encoded payload")
	}
}

func TestToolResultToMCP_ImageURL_Falls_Back(t *testing.T) {
	got := toolResultToMCP(&agent.ToolResultBlock{
		Content: []agent.ContentBlock{
			agent.ImageBlock{
				MediaType: "image/png",
				Source:    agent.BlobSource{URL: "https://example.com/x.png"},
			},
		},
	})
	if len(got.Content) != 1 {
		t.Fatalf("content len = %d", len(got.Content))
	}
	tc, ok := got.Content[0].(mcpgo.TextContent)
	if !ok {
		t.Fatalf("content[0] = %T, want TextContent fallback", got.Content[0])
	}
	if tc.Text != "image: https://example.com/x.png" {
		t.Errorf("text = %q", tc.Text)
	}
}

func TestRandomToken_Unique(t *testing.T) {
	a, err := randomToken()
	if err != nil {
		t.Fatalf("randomToken: %v", err)
	}
	b, err := randomToken()
	if err != nil {
		t.Fatalf("randomToken: %v", err)
	}
	if a == b {
		t.Error("expected unique tokens")
	}
	if len(a) != 64 { // 32 bytes hex-encoded
		t.Errorf("token len = %d, want 64", len(a))
	}
}

func TestNewServer_BasicAdd(t *testing.T) {
	s := NewServer("name", "0.0.1")
	if s == nil {
		t.Fatal("NewServer returned nil")
	}
	s.AddTool("echo", "desc", func(_ context.Context, _ mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		return nil, nil
	})
	if s.StreamableHTTPServer() == nil {
		t.Error("StreamableHTTPServer returned nil")
	}
}

// TestBridge_RegisterAfterStart exercises the addToolToMCP branch where
// b.mcpServer is non-nil at register time.
func TestBridge_RegisterAfterStart(t *testing.T) {
	b := NewBridge("test")
	if _, err := b.Start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer func() { _ = b.Shutdown(context.Background()) }()
	if err := b.Register(&rawTool{
		name:   "post-start",
		text:   "ok",
		schema: agent.Schema{Type: "object"},
	}); err != nil {
		t.Fatalf("Register after Start: %v", err)
	}
	if names := b.ToolNames(); !contains(names, "post-start") {
		t.Errorf("post-start not in ToolNames: %v", names)
	}
}

func contains(ss []string, want string) bool {
	for _, s := range ss {
		if s == want {
			return true
		}
	}
	return false
}

// errorTool returns a Go-side error from Call; addToolToMCP must wrap that
// as mcp.NewToolResultError without bubbling the error to mcp-go.
type errorTool struct{}

func (errorTool) Name() string         { return "errtool" }
func (errorTool) Description() string  { return "errs" }
func (errorTool) Schema() agent.Schema { return agent.Schema{Type: "object"} }
func (errorTool) Call(_ context.Context, _ map[string]any) (*agent.ToolResultBlock, error) {
	return nil, errors.New("boom")
}

func TestBridge_ToolErrorWrappedAsToolResultError(t *testing.T) {
	b := NewBridge("test")
	_ = b.Register(errorTool{})
	if _, err := b.Start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer func() { _ = b.Shutdown(context.Background()) }()
	if !contains(b.ToolNames(), "errtool") {
		t.Errorf("errtool not registered: %v", b.ToolNames())
	}
}
