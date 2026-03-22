package memory

import (
	"context"
	"testing"
)

// --- calculateSimilarity ---

func TestCalculateSimilarity_Identical(t *testing.T) {
	score := calculateSimilarity("user likes coffee", "user likes coffee")
	if score != 1.0 {
		t.Errorf("identical strings should score 1.0, got %f", score)
	}
}

func TestCalculateSimilarity_Empty(t *testing.T) {
	if s := calculateSimilarity("", "something"); s != 0.0 {
		t.Errorf("empty string should score 0.0, got %f", s)
	}
	if s := calculateSimilarity("something", ""); s != 0.0 {
		t.Errorf("empty string should score 0.0, got %f", s)
	}
}

func TestCalculateSimilarity_NoOverlap(t *testing.T) {
	score := calculateSimilarity("apple orange banana", "cat dog fish")
	if score != 0.0 {
		t.Errorf("no common words should score 0.0, got %f", score)
	}
}

func TestCalculateSimilarity_PartialOverlap(t *testing.T) {
	// "user likes python" vs "user likes go" → 2 common (user, likes), union=4
	score := calculateSimilarity("user likes python", "user likes go")
	if score <= 0.0 || score >= 1.0 {
		t.Errorf("partial overlap should be between 0 and 1, got %f", score)
	}
	// Jaccard: common=2, union=4 → 0.5
	if score < 0.4 || score > 0.6 {
		t.Errorf("expected ~0.5 similarity, got %f", score)
	}
}

func TestCalculateSimilarity_HighSimilarity(t *testing.T) {
	// "user prefers dark mode in editor" vs "user prefers dark mode"
	// common=4 (user,prefers,dark,mode), union=6 → Jaccard=0.667
	score := calculateSimilarity("user prefers dark mode in editor", "user prefers dark mode")
	if score < 0.6 {
		t.Errorf("highly similar strings should score > 0.6, got %f", score)
	}
}

// --- SimpleDecider ---

func TestSimpleDecider_ExactDuplicate(t *testing.T) {
	d := NewSimpleDecider()
	memory := "User likes Python programming"
	existing := []MemoryItem{
		{ID: "mem_1", Memory: memory, Hash: hashMemory(memory)},
	}

	decision, err := d.Decide(context.Background(), memory, existing)
	if err != nil {
		t.Fatalf("Decide failed: %v", err)
	}
	if decision.Type != DecisionNoop {
		t.Errorf("exact duplicate should be NOOP, got %s", decision.Type)
	}
}

func TestSimpleDecider_NewMemory(t *testing.T) {
	d := NewSimpleDecider()
	existing := []MemoryItem{
		{ID: "mem_1", Memory: "User likes Python", Hash: hashMemory("User likes Python")},
	}

	decision, err := d.Decide(context.Background(), "User prefers dark mode UI", existing)
	if err != nil {
		t.Fatalf("Decide failed: %v", err)
	}
	if decision.Type != DecisionAdd {
		t.Errorf("unrelated memory should be ADD, got %s", decision.Type)
	}
}

func TestSimpleDecider_HighSimilarity_Update(t *testing.T) {
	d := NewSimpleDecider()
	existing := []MemoryItem{
		{ID: "mem_1", Memory: "user likes coffee every morning", Hash: hashMemory("user likes coffee every morning")},
	}

	// Very similar but slightly different
	decision, err := d.Decide(context.Background(), "user likes coffee every morning routine", existing)
	if err != nil {
		t.Fatalf("Decide failed: %v", err)
	}
	// Should UPDATE due to high similarity
	if decision.Type != DecisionUpdate {
		t.Errorf("high similarity should be UPDATE, got %s", decision.Type)
	}
	if decision.MemoryID != "mem_1" {
		t.Errorf("expected memory_id=mem_1, got %q", decision.MemoryID)
	}
}

func TestSimpleDecider_EmptyExisting(t *testing.T) {
	d := NewSimpleDecider()

	decision, err := d.Decide(context.Background(), "Brand new memory", nil)
	if err != nil {
		t.Fatalf("Decide failed: %v", err)
	}
	if decision.Type != DecisionAdd {
		t.Errorf("empty existing should result in ADD, got %s", decision.Type)
	}
}

func TestSimpleDecider_Refinement_Update(t *testing.T) {
	d := NewSimpleDecider()
	existing := []MemoryItem{
		{ID: "mem_1", Memory: "user likes go", Hash: hashMemory("user likes go")},
	}

	// New memory contains old one (refinement) with medium similarity
	// "user likes go programming language" contains "user likes go"
	decision, err := d.Decide(context.Background(), "user likes go programming language projects", existing)
	if err != nil {
		t.Fatalf("Decide failed: %v", err)
	}
	// Should be either UPDATE (refinement detected) or ADD (not similar enough)
	if decision.Type != DecisionUpdate && decision.Type != DecisionAdd {
		t.Errorf("refinement should be UPDATE or ADD, got %s", decision.Type)
	}
}

// --- LLMDecider fallback ---

func TestLLMDecider_FallbackExactDuplicate(t *testing.T) {
	d := NewLLMDecider(nil) // nil LLM forces fallback
	memory := "User prefers concise responses"
	existing := []MemoryItem{
		{ID: "mem_1", Memory: memory, Hash: hashMemory(memory)},
	}

	decision, err := d.Decide(context.Background(), memory, existing)
	if err != nil {
		t.Fatalf("Decide failed: %v", err)
	}
	if decision.Type != DecisionNoop {
		t.Errorf("fallback: exact duplicate should be NOOP, got %s", decision.Type)
	}
}

func TestLLMDecider_FallbackNewMemory(t *testing.T) {
	d := NewLLMDecider(nil) // nil LLM forces fallback
	existing := []MemoryItem{
		{ID: "mem_1", Memory: "User likes Python", Hash: hashMemory("User likes Python")},
	}

	decision, err := d.Decide(context.Background(), "Completely different topic", existing)
	if err != nil {
		t.Fatalf("Decide failed: %v", err)
	}
	if decision.Type != DecisionAdd {
		t.Errorf("fallback: new memory should be ADD, got %s", decision.Type)
	}
}

func TestLLMDecider_EmptyExisting(t *testing.T) {
	d := NewLLMDecider(nil)

	decision, err := d.Decide(context.Background(), "First memory", []MemoryItem{})
	if err != nil {
		t.Fatalf("Decide failed: %v", err)
	}
	if decision.Type != DecisionAdd {
		t.Errorf("empty existing should be ADD, got %s", decision.Type)
	}
}

// --- parseDecision ---

func TestParseDecision_ValidJSON(t *testing.T) {
	d := &LLMDecider{}
	response := `{"type": "ADD", "reason": "new unique memory"}`

	decision, err := d.parseDecision(response)
	if err != nil {
		t.Fatalf("parseDecision failed: %v", err)
	}
	if decision.Type != DecisionAdd {
		t.Errorf("expected ADD, got %s", decision.Type)
	}
	if decision.Reason != "new unique memory" {
		t.Errorf("expected reason, got %q", decision.Reason)
	}
}

func TestParseDecision_UpdateJSON(t *testing.T) {
	d := &LLMDecider{}
	response := `{"type": "UPDATE", "memory_id": "mem_123", "reason": "updated preference", "new_content": "User prefers tea"}`

	decision, err := d.parseDecision(response)
	if err != nil {
		t.Fatalf("parseDecision failed: %v", err)
	}
	if decision.Type != DecisionUpdate {
		t.Errorf("expected UPDATE, got %s", decision.Type)
	}
	if decision.MemoryID != "mem_123" {
		t.Errorf("expected memory_id=mem_123, got %q", decision.MemoryID)
	}
}

func TestParseDecision_MarkdownCodeBlock(t *testing.T) {
	d := &LLMDecider{}
	response := "```json\n{\"type\": \"NOOP\", \"reason\": \"duplicate\"}\n```"

	decision, err := d.parseDecision(response)
	if err != nil {
		t.Fatalf("parseDecision failed: %v", err)
	}
	if decision.Type != DecisionNoop {
		t.Errorf("expected NOOP, got %s", decision.Type)
	}
}

func TestParseDecision_PlainText_Noop(t *testing.T) {
	d := &LLMDecider{}
	response := "NOOP - this memory already exists"

	decision, err := d.parseDecision(response)
	if err != nil {
		t.Fatalf("parseDecision failed: %v", err)
	}
	if decision.Type != DecisionNoop {
		t.Errorf("expected NOOP from plain text, got %s", decision.Type)
	}
}

func TestParseDecision_PlainText_Update(t *testing.T) {
	d := &LLMDecider{}
	response := "UPDATE - ID: mem_456 - preference changed"

	decision, err := d.parseDecision(response)
	if err != nil {
		t.Fatalf("parseDecision failed: %v", err)
	}
	if decision.Type != DecisionUpdate {
		t.Errorf("expected UPDATE from plain text, got %s", decision.Type)
	}
}
