package agent

import (
	"errors"
	"strings"
	"testing"
)

func TestToolRegistry_ExecuteUnknownTool(t *testing.T) {
	r := NewToolRegistry()

	_, err := r.Execute("does_not_exist", "{}")
	if err == nil {
		t.Fatal("expected an error for an unregistered tool, got nil")
	}

	if !strings.Contains(err.Error(), "unknown tool") {
		t.Errorf("expected 'unknown tool' error, got: %v", err)
	}
}

func TestToolRegistry_ExecuteInvalidJSON(t *testing.T) {
	r := NewToolRegistry()

	r.Register(&Tool{
		Name: "echo",
		Execute: func(args map[string]any) (string, error) {
			t.Fatal("Execute should not run when argument JSON is invalid")
			return "", nil
		},
	})

	_, err := r.Execute("echo", "{not valid json")
	if err == nil {
		t.Fatal("expected an error for malformed argument JSON, got nil")
	}

	if !strings.Contains(err.Error(), "invalid args") {
		t.Errorf("expected 'invalid args' error, got: %v", err)
	}
}

func TestToolRegistry_ExecuteSuccess(t *testing.T) {
	r := NewToolRegistry()

	r.Register(&Tool{
		Name: "greet",
		Execute: func(args map[string]any) (string, error) {
			return "hello " + args["name"].(string), nil
		},
	})

	result, err := r.Execute("greet", `{"name":"world"}`)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result != "hello world" {
		t.Errorf("got %q, want %q", result, "hello world")
	}
}

func TestToolRegistry_ExecutePropagatesToolError(t *testing.T) {
	r := NewToolRegistry()

	wantErr := errors.New("boom")

	r.Register(&Tool{
		Name: "explode",
		Execute: func(args map[string]any) (string, error) {
			return "", wantErr
		},
	})

	_, err := r.Execute("explode", "{}")

	if !errors.Is(err, wantErr) {
		t.Errorf("got error %v, want %v", err, wantErr)
	}
}

func TestToolRegistry_ToOpenAITools(t *testing.T) {
	r := NewToolRegistry()
	r.Register(&Tool{Name: "a", Description: "tool a"})
	r.Register(&Tool{Name: "b", Description: "tool b"})

	tools := r.ToOpenAITools()
	if len(tools) != 2 {
		t.Fatalf("got %d tools, want 2", len(tools))
	}

	names := map[string]bool{}

	for _, tool := range tools {
		names[tool.Function.Name] = true
	}

	if !names["a"] || !names["b"] {
		t.Errorf("expected tools named 'a' and 'b', got %+v", names)
	}
}

func TestToOpenAITools_EmptyRegistry(t *testing.T) {
	r := NewToolRegistry()

	tools := r.ToOpenAITools()

	if len(tools) != 0 {
		t.Errorf("got %d tools, want 0", len(tools))
	}
}

func TestTruncate_ShorterThanLimit(t *testing.T) {
	got := truncate("hello", 10)
	if got != "hello" {
		t.Errorf("got %q, want %q", got, "hello")
	}
}

func TestTruncate_ExactlyAtLimit(t *testing.T) {
	got := truncate("hello", 5)
	if got != "hello" {
		t.Errorf("got %q, want %q, string at exactly the limit should be unchanged", got, "hello")
	}
}

func TestTruncate_LongerThanLimit(t *testing.T) {
	got := truncate("hello world", 5)
	want := "hello... (6 more bytes)"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}
