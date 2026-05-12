package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"

	mcpclient "github.com/mark3labs/mcp-go/client"
	mcptransport "github.com/mark3labs/mcp-go/client/transport"
	mcpgo "github.com/mark3labs/mcp-go/mcp"

	agent "github.com/cago-frame/agents/agent"
)

type echoTool struct{}

func (echoTool) Name() string        { return "echo" }
func (echoTool) Description() string { return "echo back" }
func (echoTool) Schema() agent.Schema {
	return agent.Schema{Type: "object"}
}
func (echoTool) Call(_ context.Context, in map[string]any) (*agent.ToolResultBlock, error) {
	b, _ := json.Marshal(in)
	return &agent.ToolResultBlock{
		Content: []agent.ContentBlock{agent.TextBlock{Text: string(b)}},
	}, nil
}

func TestBridge_Register_StoresTools(t *testing.T) {
	b := NewBridge("test")
	if err := b.Register(echoTool{}); err != nil {
		t.Fatalf("register: %v", err)
	}
	names := b.ToolNames()
	if len(names) != 1 || names[0] != "echo" {
		t.Fatalf("names: %v", names)
	}
}

func TestBridge_Register_RejectsDuplicates(t *testing.T) {
	b := NewBridge("test")
	_ = b.Register(echoTool{})
	err := b.Register(echoTool{})
	if err == nil {
		t.Fatalf("want duplicate error")
	}
	if !errors.Is(err, ErrToolAlreadyRegistered) {
		t.Fatalf("want errors.Is(err, ErrToolAlreadyRegistered); got %v", err)
	}
}

func TestBridge_Start_ReturnsEndpoint(t *testing.T) {
	b := NewBridge("test")
	_ = b.Register(echoTool{})
	ep, err := b.Start(context.Background())
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	defer func() { _ = b.Shutdown(context.Background()) }()
	if !strings.HasPrefix(ep.URL, "http://127.0.0.1:") {
		t.Fatalf("url: %s", ep.URL)
	}
	if len(ep.Token) < 16 {
		t.Fatalf("token too short: %q", ep.Token)
	}
	if ep.ServerName != "test" {
		t.Fatalf("server name: %s", ep.ServerName)
	}
}

func TestBridge_Start_UnauthorizedWithoutToken(t *testing.T) {
	b := NewBridge("test")
	_ = b.Register(echoTool{})
	ep, _ := b.Start(context.Background())
	defer func() { _ = b.Shutdown(context.Background()) }()

	resp, err := http.Post(ep.URL, "application/json", strings.NewReader(`{}`))
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("want 401, got %d", resp.StatusCode)
	}
}

func TestBridge_Start_Idempotent(t *testing.T) {
	b := NewBridge("test")
	ep1, _ := b.Start(context.Background())
	ep2, err := b.Start(context.Background())
	if err != nil {
		t.Fatalf("second start: %v", err)
	}
	if ep1.URL != ep2.URL || ep1.Token != ep2.Token {
		t.Fatalf("start not idempotent: %+v vs %+v", ep1, ep2)
	}
	_ = b.Shutdown(context.Background())
}

func TestBridge_RoundTrip_EchoTool(t *testing.T) {
	b := NewBridge("test")
	_ = b.Register(echoTool{})
	ep, err := b.Start(context.Background())
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	defer func() { _ = b.Shutdown(context.Background()) }()

	cli, err := mcpclient.NewStreamableHttpClient(ep.URL,
		mcptransport.WithHTTPHeaders(map[string]string{"Authorization": "Bearer " + ep.Token}),
	)
	if err != nil {
		t.Fatalf("client: %v", err)
	}
	if err := cli.Start(context.Background()); err != nil {
		t.Fatalf("client start: %v", err)
	}
	if _, err := cli.Initialize(context.Background(), mcpgo.InitializeRequest{}); err != nil {
		t.Fatalf("initialize: %v", err)
	}
	res, err := cli.CallTool(context.Background(), mcpgo.CallToolRequest{
		Params: mcpgo.CallToolParams{
			Name:      "echo",
			Arguments: map[string]any{"msg": "hi"},
		},
	})
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	if len(res.Content) == 0 {
		t.Fatalf("no content")
	}
	tc, ok := res.Content[0].(mcpgo.TextContent)
	if !ok {
		t.Fatalf("content[0] type = %T, want TextContent", res.Content[0])
	}
	if !strings.Contains(tc.Text, `"msg":"hi"`) {
		t.Fatalf("text = %q, want JSON containing msg:hi", tc.Text)
	}
}
