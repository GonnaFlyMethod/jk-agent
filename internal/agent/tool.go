package agent

import (
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/openai/openai-go"
	"github.com/openai/openai-go/shared"
)

type Tool struct {
	Name        string
	Description string
	Parameters  shared.FunctionParameters
	Execute     func(args map[string]any) (string, error)
}

type ToolRegistry struct {
	tools map[string]*Tool
}

func NewToolRegistry() *ToolRegistry {
	return &ToolRegistry{tools: make(map[string]*Tool)}
}

func (r *ToolRegistry) Register(t *Tool) {
	r.tools[t.Name] = t
}

func (r *ToolRegistry) ToOpenAITools() []openai.ChatCompletionToolParam {
	out := make([]openai.ChatCompletionToolParam, 0, len(r.tools))
	for _, t := range r.tools {
		out = append(out, openai.ChatCompletionToolParam{
			Function: shared.FunctionDefinitionParam{
				Name:        t.Name,
				Description: openai.String(t.Description),
				Parameters:  t.Parameters,
			},
		})
	}
	return out
}

func (r *ToolRegistry) Execute(name, argsJSON string) (string, error) {
	t, ok := r.tools[name]
	if !ok {
		return "", fmt.Errorf("unknown tool: %s", name)
	}

	var args map[string]any
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return "", fmt.Errorf("invalid args for %s: %w", name, err)
	}

	log.Printf("tool call: %s(%s)", name, argsJSON)
	start := time.Now()

	result, err := t.Execute(args)

	if err != nil {
		log.Printf("tool error: %s (%s): %v", name, time.Since(start), err)
		return result, err
	}

	log.Printf("tool result: %s (%s): %s", name, time.Since(start), truncate(result, 500))
	return result, nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + fmt.Sprintf("... (%d more bytes)", len(s)-n)
}
