package codex

import (
	"context"
	"fmt"
	"sync"

	"github.com/cago-frame/agents/mcp"
	"github.com/cago-frame/agents/tool"
)

const mcpBridgeTokenEnv = "CAGO_CODEX_MCP_CAGO_BRIDGE_" + "TOKEN"

type mcpBridge struct {
	mu        sync.Mutex
	startOnce sync.Once
	bridge    *mcp.Bridge
	ep        mcp.BridgeEndpoint
	startErr  error
	tools     map[string]struct{}
	tokenEnv  string
}

func newMCPBridge() *mcpBridge {
	return &mcpBridge{
		bridge:   mcp.NewBridge(mcpBridgeServerName),
		tools:    map[string]struct{}{},
		tokenEnv: mcpBridgeTokenEnv,
	}
}

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

func (m *mcpBridge) applyToSpec(spec *runSpec) {
	m.mu.Lock()
	ep := m.ep
	tokenEnv := m.tokenEnv
	m.mu.Unlock()
	if ep.URL == "" {
		panic("mcpBridge: applyToSpec called before start() succeeded")
	}
	spec.mcpServerName = ep.ServerName
	spec.mcpURL = ep.URL
	spec.mcpTokenEnv = tokenEnv
	if spec.env == nil {
		spec.env = map[string]string{}
	}
	spec.env[tokenEnv] = ep.Token
}
