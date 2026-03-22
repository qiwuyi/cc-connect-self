package memory

import (
	"testing"
)

func TestFilterMessagesForMemory(t *testing.T) {
	tests := []struct {
		name     string
		input    []MemoryMessage
		expected []MemoryMessage
	}{
		{
			name:     "empty input",
			input:    []MemoryMessage{},
			expected: []MemoryMessage{},
		},
		{
			name:     "nil input",
			input:    nil,
			expected: nil,
		},
		{
			name: "keep user messages",
			input: []MemoryMessage{
				{Role: "user", Content: "Hello"},
				{Role: "assistant", Content: "Hi there"},
			},
			expected: []MemoryMessage{
				{Role: "user", Content: "Hello"},
				{Role: "assistant", Content: "Hi there"},
			},
		},
		{
			name: "filter out tool messages",
			input: []MemoryMessage{
				{Role: "user", Content: "Hello"},
				{Role: "tool", Content: "Tool result"},
				{Role: "assistant", Content: "Hi there"},
			},
			expected: []MemoryMessage{
				{Role: "user", Content: "Hello"},
				{Role: "assistant", Content: "Hi there"},
			},
		},
		{
			name: "keep only user and final assistant",
			input: []MemoryMessage{
				{Role: "user", Content: "First message"},
				{Role: "assistant", Content: "Thinking...", Metadata: map[string]any{"tool_calls": true}},
				{Role: "tool", Content: "Tool output"},
				{Role: "assistant", Content: "Final response"},
			},
			expected: []MemoryMessage{
				{Role: "user", Content: "First message"},
				{Role: "assistant", Content: "Thinking...", Metadata: map[string]any{"tool_calls": true}},
				{Role: "assistant", Content: "Final response"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := filterMessagesForMemory(tt.input)

			if len(result) != len(tt.expected) {
				t.Errorf("expected %d messages, got %d", len(tt.expected), len(result))
				return
			}

			for i := range result {
				if result[i].Role != tt.expected[i].Role {
					t.Errorf("message %d: expected role %q, got %q", i, tt.expected[i].Role, result[i].Role)
				}
				if result[i].Content != tt.expected[i].Content {
					t.Errorf("message %d: expected content %q, got %q", i, tt.expected[i].Content, result[i].Content)
				}
			}
		})
	}
}

func TestDefaultMemoryConfig(t *testing.T) {
	cfg := DefaultMemoryConfig()

	if !cfg.Enabled {
		t.Error("expected Enabled to be true by default")
	}
	if cfg.DebounceSeconds != 30 {
		t.Errorf("expected DebounceSeconds to be 30, got %d", cfg.DebounceSeconds)
	}
	if cfg.MaxFacts != 100 {
		t.Errorf("expected MaxFacts to be 100, got %d", cfg.MaxFacts)
	}
	if cfg.FactConfidenceThreshold != 0.7 {
		t.Errorf("expected FactConfidenceThreshold to be 0.7, got %f", cfg.FactConfidenceThreshold)
	}
	if !cfg.InjectionEnabled {
		t.Error("expected InjectionEnabled to be true by default")
	}
	if cfg.MaxInjectionTokens != 2000 {
		t.Errorf("expected MaxInjectionTokens to be 2000, got %d", cfg.MaxInjectionTokens)
	}
}
