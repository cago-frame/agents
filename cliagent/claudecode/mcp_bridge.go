package claudecode

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/cago-frame/agents/mcp"
	"github.com/cago-frame/agents/tool"
)

// mcpBridge wraps mcp.Bridge with lazy-start semantics for backend.
// One mcpBridge instance per backend instance — Streams share the bridge.
type mcpBridge struct {
	mu        sync.Mutex
	startOnce sync.Once
	bridge    *mcp.Bridge
	ep        mcp.BridgeEndpoint  // populated after first start()
	startErr  error               // cached error from first start() attempt
	tools     map[string]struct{} // names registered, for ToolSource classification
}

func newMCPBridge() *mcpBridge {
	return &mcpBridge{
		bridge: mcp.NewBridge("cago-bridge"),
		tools:  map[string]struct{}{},
	}
}

// ensureRegistered registers each tool that hasn't been registered yet.
// Idempotent on duplicate names.
func (m *mcpBridge) ensureRegistered(ts []tool.Tool) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, t := range ts {
		if _, ok := m.tools[t.Name()]; ok {
			continue
		}
		if err := m.bridge.Register(t); err != nil {
			return fmt.Errorf("%w: %v", ErrBridgeStart, err)
		}
		m.tools[t.Name()] = struct{}{}
	}
	return nil
}

// start lazy-starts the bridge HTTP server. Returns the cached endpoint after
// first call (idempotent). Uses sync.Once to eliminate the TOCTOU window that
// would allow two concurrent callers to both reach mcp.Bridge.Start.
//
// Note: sync.Once does not propagate ctx cancellation — a canceled first call
// permanently caches the error. Acceptable because bridge start is a fast
// loopback bind with no remote I/O.
func (m *mcpBridge) start(ctx context.Context) (mcp.BridgeEndpoint, error) {
	m.startOnce.Do(func() {
		ep, err := m.bridge.Start(ctx)
		m.mu.Lock()
		m.ep = ep
		if err != nil {
			m.startErr = fmt.Errorf("%w: %v", ErrBridgeStart, err)
		}
		m.mu.Unlock()
	})
	m.mu.Lock()
	ep, err := m.ep, m.startErr
	m.mu.Unlock()
	return ep, err
}

func (m *mcpBridge) shutdown(ctx context.Context) error {
	m.mu.Lock()
	bridge := m.bridge
	m.mu.Unlock()
	if bridge == nil {
		return nil
	}
	return bridge.Shutdown(ctx)
}

// configJSON serializes the endpoint as the JSON expected by `claude --mcp-config`.
// Caller must have called start() first (m.ep populated). Panics if start()
// has not yet succeeded — this guards a contract that must hold at call sites
// and makes violations loud rather than silently emitting an empty --mcp-config.
func (m *mcpBridge) configJSON() string {
	m.mu.Lock()
	ep := m.ep
	m.mu.Unlock()
	if ep.URL == "" {
		panic("mcpBridge: configJSON called before start() succeeded")
	}
	cfg := map[string]any{
		"mcpServers": map[string]any{
			ep.ServerName: map[string]any{
				"type": "http",
				"url":  ep.URL,
				"headers": map[string]string{
					"Authorization": "Bearer " + ep.Token,
				},
			},
		},
	}
	data, _ := json.Marshal(cfg)
	return string(data)
}

// endpoint returns the cached endpoint after start() has succeeded.
func (m *mcpBridge) endpoint() mcp.BridgeEndpoint {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.ep
}

// toolNames returns a stable copy of registered tool names (set semantics → unordered).
func (m *mcpBridge) toolNames() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]string, 0, len(m.tools))
	for n := range m.tools {
		out = append(out, n)
	}
	return out
}
