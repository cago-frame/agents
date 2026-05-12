package mcp

import (
	"context"
	"net/http"

	trace2 "github.com/cago-frame/cago/pkg/opentelemetry/trace"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"go.opentelemetry.io/otel/trace"
)

// 基于github.com/mark3labs/mcp-go简单封装
// 接入 cago 框架相关的开发工具

type Server struct {
	tracer trace.Tracer
	server *server.MCPServer
}

func NewServer(name string, version string) *Server {
	s := &Server{
		server: server.NewMCPServer(
			name,
			version,
			server.WithToolCapabilities(false),
			server.WithRecovery(),
		),
	}
	if tp := trace2.Default(); tp != nil {
		s.tracer = tp.Tracer(
			"github.com/cago-frame/agents/mcp",
			trace.WithInstrumentationVersion(version),
		)
	}
	return s
}

type ToolHandlerFunc server.ToolHandlerFunc

func (s *Server) AddTool(name string, description string, handler ToolHandlerFunc, options ...mcp.ToolOption) *Server {
	toolOptions := make([]mcp.ToolOption, 0, 1+len(options))
	toolOptions = append(toolOptions, mcp.WithDescription(description))
	toolOptions = append(toolOptions, options...)

	tool := mcp.NewTool(name, toolOptions...)

	h := handler

	if s.tracer != nil {
		h = func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			ctx, span := s.tracer.Start(ctx, "CallTool "+name)
			defer span.End()
			return handler(ctx, request)
		}
	}

	s.server.AddTool(tool, server.ToolHandlerFunc(h))
	return s
}

func (s *Server) StreamableHTTPServer() http.HandlerFunc {
	return server.NewStreamableHTTPServer(s.server).ServeHTTP
}
