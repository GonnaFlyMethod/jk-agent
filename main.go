package main

import (
	"context"
	"log"
	"os"
	"path/filepath"
	"strings"

	"JK/internal/agent"
	"JK/internal/osmcp"
	"JK/internal/sysinfo"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func main() {
	logFile, err := os.OpenFile("orchestrator.log", os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		log.Fatal(err)
	}
	defer logFile.Close()

	log.SetOutput(logFile)
	log.SetFlags(log.Ldate | log.Ltime | log.Lmicroseconds)

	ctx := context.Background()

	mcpManager := agent.NewMCPManager()
	defer mcpManager.Close()

	// os-utils runs in-process — no subprocess or go run needed.
	osServer := mcp.NewServer(&mcp.Implementation{Name: "os-utils", Version: "0.1.0"}, nil)
	osmcp.RegisterTools(osServer)

	if err := mcpManager.RegisterInProcess(ctx, "os-utils", osServer); err != nil {
		log.Fatal(err)
	}

	// External MCP servers are spawned on demand if their command is on PATH.
	mcpManager.RegisterServer(agent.MCPServerConfig{
		Name:    "playwright",
		Command: "npx",
		Args:    []string{"-y", "@playwright/mcp@latest", "--browser", "firefox"},
	})

	mcpManager.RegisterServer(agent.MCPServerConfig{
		Name:    "filesystem",
		Command: "npx",
		Args:    []string{"-y", "@modelcontextprotocol/server-filesystem", os.Getenv("HOME")},
	})

	// Desktop control (screenshots with element detection, mouse, keyboard) —
	// fetched on demand via uvx straight from its git repo since it isn't
	// published on PyPI. This supersedes osmcp's own take_screenshot/press_key.
	mcpManager.RegisterServer(agent.MCPServerConfig{
		Name:    "desktop-control",
		Command: "uvx",
		Args:    []string{"--from", "git+https://github.com/charettep/ubuntu-desktop-control-mcp", "ubuntu-desktop-control"},
	})

	// MEMORY_FILE_PATH must be absolute — the server resolves a relative
	// path against its own npx install directory, not our cwd, which would
	// silently scatter the knowledge graph across ephemeral npx cache dirs.
	memoryPath, err := filepath.Abs("memory.jsonl")
	if err != nil {
		log.Fatal(err)
	}

	mcpManager.RegisterServer(agent.MCPServerConfig{
		Name:    "memory",
		Command: "npx",
		Args:    []string{"-y", "@modelcontextprotocol/server-memory"},
		Env:     map[string]string{"MEMORY_FILE_PATH": memoryPath},
	})

	registry := agent.NewToolRegistry()

	mcpTools, err := mcpManager.DiscoverTools(ctx)
	if err != nil {
		log.Fatal(err)
	}
	for _, t := range mcpTools {
		registry.Register(t)
	}

	vendor := agent.NewOpenAIVendor()
	orchestrator := agent.NewOrchestrator(vendor, registry, sysinfo.DetectEnvironment(), loadMemory(registry), loadInstructions())
	orchestrator.Start()
}

// loadInstructions reads JK.md (what to do) and PERSONALITY.md (how to talk)
// from the working directory, if present, and folds them into one system
// message. Either file missing is not fatal — the hardcoded system prompt
// still covers the core operating rules on its own.
func loadInstructions() string {
	var sb strings.Builder

	if data, err := os.ReadFile("JK.md"); err == nil {
		sb.WriteString(string(data))
	} else {
		log.Printf("JK.md not loaded: %v", err)
	}

	if data, err := os.ReadFile("PERSONALITY.md"); err == nil {
		if sb.Len() > 0 {
			sb.WriteString("\n\n")
		}
		sb.WriteString(string(data))
	} else {
		log.Printf("PERSONALITY.md not loaded: %v", err)
	}

	return sb.String()
}

// loadMemory reads the entire user knowledge graph once at startup, if the
// memory MCP server connected, so the model starts every session already
// knowing what it has learned about the user instead of having to remember
// to call read_graph itself.
func loadMemory(registry *agent.ToolRegistry) string {
	result, err := registry.Execute("read_graph", "{}")
	if err != nil {
		return "USER MEMORY: unavailable (" + err.Error() + ")"
	}
	return "USER MEMORY (knowledge graph read at startup — this is what you already know about the user):\n" + result
}
