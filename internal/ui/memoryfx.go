package ui

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// memoryWriteTools are the knowledge-graph tools that add something new,
// as opposed to read_graph/search_nodes/open_nodes which just query it.
var memoryWriteTools = map[string]bool{
	"create_entities":  true,
	"create_relations": true,
	"add_observations": true,
}

// IsMemoryWriteTool reports whether name is a knowledge-graph tool that
// adds something new to memory (as opposed to reading it).
func IsMemoryWriteTool(name string) bool {
	return memoryWriteTools[name]
}

// CelebrateMemoryHit plays a short terminal animation and prints a banner
// when the model successfully writes to the memory knowledge graph, so a
// newly learned fact about the user doesn't just silently scroll by in the
// tool-call noise.
func CelebrateMemoryHit(toolName, argsJSON string) {
	facts := extractMemoryFacts(toolName, argsJSON)
	if len(facts) == 0 {
		return
	}

	spin := []string{"🧠", "💭", "✨", "💾", "🌟", "💭"}
	colors := []int{201, 165, 129, 93, 57, 213}
	for i := 0; i < len(spin)*3; i++ {
		frame := spin[i%len(spin)]
		color := colors[i%len(colors)]
		fmt.Printf("\r\033[38;5;%dm  %s  saving to memory...\033[0m", color, frame)
		time.Sleep(60 * time.Millisecond)
	}

	fmt.Print("\r\033[K")

	border := "\033[38;5;213m" + strings.Repeat("─", 46) + "\033[0m"
	fmt.Println(border)
	fmt.Println("\033[1m\033[38;5;213m   🧠 ✨ MEMORY UPDATED ✨ 🧠\033[0m")
	for _, f := range facts {
		fmt.Printf("   \033[38;5;120m➜\033[0m %s\n", f)
	}
	fmt.Println(border)
}

// extractMemoryFacts turns a memory-write tool call's raw JSON arguments
// into short human-readable lines, so the celebration banner shows what was
// actually learned instead of just "something happened".
func extractMemoryFacts(toolName, argsJSON string) []string {
	var facts []string

	switch toolName {
	case "add_observations":
		var args struct {
			Observations []struct {
				EntityName string   `json:"entityName"`
				Contents   []string `json:"contents"`
			} `json:"observations"`
		}
		if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
			return nil
		}
		for _, o := range args.Observations {
			for _, c := range o.Contents {
				facts = append(facts, fmt.Sprintf("%s — %s", o.EntityName, c))
			}
		}

	case "create_entities":
		var args struct {
			Entities []struct {
				Name         string   `json:"name"`
				EntityType   string   `json:"entityType"`
				Observations []string `json:"observations"`
			} `json:"entities"`
		}
		if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
			return nil
		}
		for _, e := range args.Entities {
			facts = append(facts, fmt.Sprintf("new entity: %s (%s)", e.Name, e.EntityType))
			for _, o := range e.Observations {
				facts = append(facts, fmt.Sprintf("%s — %s", e.Name, o))
			}
		}

	case "create_relations":
		var args struct {
			Relations []struct {
				From         string `json:"from"`
				To           string `json:"to"`
				RelationType string `json:"relationType"`
			} `json:"relations"`
		}
		if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
			return nil
		}
		for _, r := range args.Relations {
			facts = append(facts, fmt.Sprintf("%s --%s--> %s", r.From, r.RelationType, r.To))
		}
	}

	return facts
}
