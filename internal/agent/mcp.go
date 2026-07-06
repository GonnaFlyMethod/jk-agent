package agent

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// MCPServerConfig describes an MCP server the orchestrator can talk to.
// Adding a new utility (a DAW, another browser, whatever) means adding a
// config here, not writing a new Tool by hand.
type MCPServerConfig struct {
	Name    string
	Command string
	Args    []string
	Env     map[string]string // extra env vars for the server process, e.g. a storage path
}

// MCPManager discovers and calls tools exposed by configured MCP servers.
// A server is only spawned if its Command is actually installed, and the
// heavy action it wraps (e.g. launching Firefox) stays lazy inside that
// server's own tool handlers - we never launch it ourselves.
type MCPManager struct {
	mu         sync.Mutex
	client     *mcp.Client
	configs    []MCPServerConfig
	sessions   map[string]*mcp.ClientSession // server name -> session
	owner      map[string]string             // tool name -> server name
	discovered []*Tool                       // tools from in-process servers
}

func NewMCPManager() *MCPManager {
	return &MCPManager{
		client:   mcp.NewClient(&mcp.Implementation{Name: "JK-orchestrator", Version: "0.1.0"}, nil),
		sessions: make(map[string]*mcp.ClientSession),
		owner:    make(map[string]string),
	}
}

func (m *MCPManager) RegisterServer(cfg MCPServerConfig) {
	m.configs = append(m.configs, cfg)
}

// DiscoverTools connects to every registered server whose Command resolves
// on PATH, lists its tools, and returns them wrapped as Tools ready for a
// ToolRegistry. Servers whose command isn't installed are skipped.
func (m *MCPManager) DiscoverTools(ctx context.Context) ([]*Tool, error) {
	// In-process tools are already wrapped at registration time.
	tools := append([]*Tool{}, m.discovered...)

	for _, cfg := range m.configs {
		if _, err := exec.LookPath(cfg.Command); err != nil {
			fmt.Printf("mcp: skipping %s, %q not found on PATH\n", cfg.Name, cfg.Command)
			continue
		}

		session, err := m.connect(ctx, cfg)
		if err != nil {
			return nil, fmt.Errorf("mcp: connect %s: %w", cfg.Name, err)
		}

		result, err := session.ListTools(ctx, nil)
		if err != nil {
			return nil, fmt.Errorf("mcp: list tools %s: %w", cfg.Name, err)
		}

		for _, mt := range result.Tools {
			m.owner[mt.Name] = cfg.Name
			tools = append(tools, m.wrapTool(mt))
		}
	}

	return tools, nil
}

func (m *MCPManager) connect(ctx context.Context, cfg MCPServerConfig) (*mcp.ClientSession, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if session, ok := m.sessions[cfg.Name]; ok {
		return session, nil
	}

	cmd := exec.Command(cfg.Command, cfg.Args...)
	if len(cfg.Env) > 0 {
		cmd.Env = os.Environ()
		for k, v := range cfg.Env {
			cmd.Env = append(cmd.Env, k+"="+v)
		}
	}

	transport := &mcp.CommandTransport{Command: cmd}
	session, err := m.client.Connect(ctx, transport, nil)
	if err != nil {
		return nil, err
	}

	m.sessions[cfg.Name] = session
	return session, nil
}

func (m *MCPManager) wrapTool(mt *mcp.Tool) *Tool {
	params, ok := mt.InputSchema.(map[string]any)
	if !ok {
		params = map[string]any{"type": "object"}
	}

	return &Tool{
		Name:        mt.Name,
		Description: mt.Description,
		Parameters:  params,
		Execute: func(args map[string]any) (string, error) {
			return m.call(mt.Name, args)
		},
	}
}

func (m *MCPManager) call(name string, args map[string]any) (string, error) {
	m.mu.Lock()
	serverName, ok := m.owner[name]
	session := m.sessions[serverName]
	m.mu.Unlock()

	if !ok || session == nil {
		return "", fmt.Errorf("mcp: no server owns tool %s", name)
	}

	res, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      name,
		Arguments: args,
	})
	if err != nil {
		return "", fmt.Errorf("mcp: call %s: %w", name, err)
	}

	var sb strings.Builder
	for _, c := range res.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			sb.WriteString(tc.Text)
		}
	}

	if res.IsError {
		return "", fmt.Errorf("mcp: tool %s reported an error: %s", name, sb.String())
	}

	return sb.String(), nil
}

// RegisterInProcess starts an already-constructed MCP server in a goroutine
// using an in-memory transport and connects the manager's client to it.
// Use this instead of RegisterServer when the server code lives in-process.
func (m *MCPManager) RegisterInProcess(ctx context.Context, name string, server *mcp.Server) error {
	serverTransport, clientTransport := mcp.NewInMemoryTransports()

	go func() {
		if err := server.Run(ctx, serverTransport); err != nil {
			fmt.Printf("mcp: in-process server %s exited: %v\n", name, err)
		}
	}()

	session, err := m.client.Connect(ctx, clientTransport, nil)
	if err != nil {
		return fmt.Errorf("mcp: connect in-process %s: %w", name, err)
	}

	m.mu.Lock()
	m.sessions[name] = session
	m.mu.Unlock()

	result, err := session.ListTools(ctx, nil)
	if err != nil {
		return fmt.Errorf("mcp: list tools %s: %w", name, err)
	}

	m.mu.Lock()
	for _, mt := range result.Tools {
		m.owner[mt.Name] = name
		m.discovered = append(m.discovered, m.wrapTool(mt))
	}
	m.mu.Unlock()

	return nil
}

// Close terminates every MCP server session that was actually started.
func (m *MCPManager) Close() {
	m.mu.Lock()
	defer m.mu.Unlock()

	for name, session := range m.sessions {
		if err := session.Close(); err != nil {
			fmt.Printf("mcp: close %s: %v\n", name, err)
		}
	}
}
