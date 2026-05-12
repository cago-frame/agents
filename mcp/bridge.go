package mcp

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"

	agent "github.com/cago-frame/agents/agent"
	"github.com/cago-frame/agents/tool"
)

// ErrToolAlreadyRegistered Bridge.Register 同名重复时返回，包装在 fmt.Errorf("%w") 里。
// 用 errors.Is 判断。
var ErrToolAlreadyRegistered = errors.New("mcp: tool already registered")

// Bridge 把一组 tool.Tool 暴露为 MCP server (loopback HTTP + bearer)。
// 任意 MCP 消费者可连接；claudecode 是首个使用方。
type Bridge struct {
	name  string
	mu    sync.Mutex
	tools map[string]tool.Tool
	order []string
	// runtime state (populated by Start):
	addr      string
	token     string
	httpSrv   *http.Server
	mcpServer *server.MCPServer
}

// NewBridge 构造。name 作为 MCP server 名字。
func NewBridge(name string) *Bridge {
	return &Bridge{name: name, tools: map[string]tool.Tool{}}
}

// Register 加入一个 tool.Tool。同名重复返回 ErrToolAlreadyRegistered
// 包装的错误（用 errors.Is 匹配）。
func (b *Bridge) Register(t tool.Tool) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	name := t.Name()
	if _, exists := b.tools[name]; exists {
		return fmt.Errorf("bridge: %w: %q", ErrToolAlreadyRegistered, name)
	}
	b.tools[name] = t
	b.order = append(b.order, name)
	if b.mcpServer != nil {
		b.addToolToMCP(t)
	}
	return nil
}

// MustRegister 注册一个 tool.Tool，重复则 panic。便于装配阶段的链式 / 静态注册。
func (b *Bridge) MustRegister(t tool.Tool) {
	if err := b.Register(t); err != nil {
		panic(err)
	}
}

// ToolNames 返回注册的 tool 名列表（注册顺序）。
func (b *Bridge) ToolNames() []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]string, len(b.order))
	copy(out, b.order)
	return out
}

// Name 返回 MCP server 名。
func (b *Bridge) Name() string { return b.name }

// BridgeEndpoint 一次 Bridge Start 后的连接描述。
type BridgeEndpoint struct {
	ServerName string
	URL        string
	Token      string
}

// Start 启动 HTTP 监听（loopback 随机端口）。幂等：二次调用返回同一 Endpoint。
func (b *Bridge) Start(ctx context.Context) (BridgeEndpoint, error) {
	tracer := otel.GetTracerProvider().Tracer("github.com/cago-frame/agents/mcp")
	_, span := tracer.Start(ctx, "mcp.Bridge.Start")
	defer span.End()

	b.mu.Lock()
	if b.addr != "" {
		ep := BridgeEndpoint{ServerName: b.name, URL: b.urlLocked(), Token: b.token}
		span.SetAttributes(
			attribute.String("bridge.url", ep.URL),
			attribute.Int("bridge.tool_count", len(b.order)),
		)
		b.mu.Unlock()
		return ep, nil
	}
	token, err := randomToken()
	if err != nil {
		b.mu.Unlock()
		return BridgeEndpoint{}, err
	}
	b.token = token

	if b.mcpServer == nil {
		b.mcpServer = server.NewMCPServer(b.name, "0.1.0",
			server.WithToolCapabilities(false),
			server.WithRecovery(),
		)
		for _, name := range b.order {
			b.addToolToMCP(b.tools[name])
		}
	}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		b.mu.Unlock()
		return BridgeEndpoint{}, err
	}
	b.addr = ln.Addr().String()

	mux := http.NewServeMux()
	mux.Handle("/mcp", b.authMiddleware(b.buildHandler().ServeHTTP))

	srv := &http.Server{Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	b.httpSrv = srv
	go func() { _ = srv.Serve(ln) }()

	ep := BridgeEndpoint{ServerName: b.name, URL: b.urlLocked(), Token: token}
	span.SetAttributes(
		attribute.String("bridge.url", ep.URL),
		attribute.Int("bridge.tool_count", len(b.order)),
	)
	b.mu.Unlock()
	return ep, nil
}

// Shutdown 优雅关闭（给 3s graceful deadline）。
func (b *Bridge) Shutdown(ctx context.Context) error {
	b.mu.Lock()
	srv := b.httpSrv
	b.httpSrv = nil
	b.addr = ""
	b.token = ""
	b.mu.Unlock()
	if srv == nil {
		return nil
	}
	return srv.Shutdown(ctx)
}

// urlLocked 调用者须持有 b.mu。
func (b *Bridge) urlLocked() string {
	return "http://" + b.addr + "/mcp"
}

func (b *Bridge) authMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		b.mu.Lock()
		expected := b.token
		b.mu.Unlock()
		hdr := r.Header.Get("Authorization")
		if expected == "" || hdr != "Bearer "+expected {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next(w, r)
	}
}

// buildHandler 构建 mcp-go streamable HTTP handler。
func (b *Bridge) buildHandler() *server.StreamableHTTPServer {
	return server.NewStreamableHTTPServer(b.mcpServer)
}

// addToolToMCP 把 tool.Tool 注册进 mcpServer。调用者须持有 b.mu 或在初始化阶段。
func (b *Bridge) addToolToMCP(t tool.Tool) {
	schemaBytes, err := json.Marshal(t.Schema())
	if err != nil {
		// agent.Schema 仅含 JSON-marshalable 字段；理论不会失败。退化为最小 schema。
		schemaBytes = []byte(`{"type":"object"}`)
	}
	mcpTool := mcp.NewToolWithRawSchema(t.Name(), t.Description(), schemaBytes)
	handler := func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args := req.GetArguments()
		result, callErr := t.Call(ctx, args)
		if callErr != nil {
			return mcp.NewToolResultError(callErr.Error()), nil
		}
		return toolResultToMCP(result), nil
	}
	b.mcpServer.AddTool(mcpTool, handler)
}

// toolResultToMCP 把 *agent.ToolResultBlock 转成 *mcp.CallToolResult。
// nil block 退化为空文本结果（保持与历史 encodeToolResult(nil) 行为一致）。
func toolResultToMCP(block *agent.ToolResultBlock) *mcp.CallToolResult {
	if block == nil {
		return mcp.NewToolResultText("")
	}
	contents := make([]mcp.Content, 0, len(block.Content))
	for _, raw := range block.Content {
		switch v := raw.(type) {
		case agent.TextBlock:
			contents = append(contents, mcp.NewTextContent(v.Text))
		case agent.ImageBlock:
			switch {
			case len(v.Source.Inline) > 0:
				data := base64.StdEncoding.EncodeToString(v.Source.Inline)
				contents = append(contents, mcp.NewImageContent(data, v.MediaType))
			case v.Source.URL != "":
				// mcp-go 的 ImageContent 走 base64 内嵌，不直接支持 URL；
				// 退化为 TextContent 描述。
				contents = append(contents, mcp.NewTextContent("image: "+v.Source.URL))
			default:
				// 既没 Inline 也没 URL，跳过。
			}
		default:
			// ToolUseBlock / ToolResultBlock / ThinkingBlock 在 tool 返回值里
			// 不应出现；保守处理为类型名 TextContent，便于排查。
			contents = append(contents, mcp.NewTextContent("["+raw.Type()+"]"))
		}
	}
	return &mcp.CallToolResult{
		Content: contents,
		IsError: block.IsError,
	}
}

func randomToken() (string, error) {
	var buf [32]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf[:]), nil
}
