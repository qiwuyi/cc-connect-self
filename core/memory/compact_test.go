package memory

import (
	"context"
	"testing"
	"time"
)

// --- NeedsCompaction ---

func TestNeedsCompaction_Disabled(t *testing.T) {
	cfg := CompactConfig{Enabled: false, Threshold: 10}
	if NeedsCompaction(100, cfg) {
		t.Error("disabled config should never need compaction")
	}
}

func TestNeedsCompaction_BelowThreshold(t *testing.T) {
	cfg := CompactConfig{Enabled: true, Threshold: 50}
	if NeedsCompaction(30, cfg) {
		t.Error("count below threshold should not need compaction")
	}
}

func TestNeedsCompaction_AtThreshold(t *testing.T) {
	cfg := CompactConfig{Enabled: true, Threshold: 50}
	if NeedsCompaction(50, cfg) {
		t.Error("count at threshold should not need compaction (only > triggers)")
	}
}

func TestNeedsCompaction_AboveThreshold(t *testing.T) {
	cfg := CompactConfig{Enabled: true, Threshold: 50}
	if !NeedsCompaction(51, cfg) {
		t.Error("count above threshold should need compaction")
	}
}

// --- SimpleCompactor ---

func makeMemory(id, content string, daysAgo int) MemoryItem {
	t := time.Now().UTC().AddDate(0, 0, -daysAgo)
	return MemoryItem{
		ID:        id,
		Memory:    content,
		Hash:      hashMemory(content),
		CreatedAt: t.Format(time.RFC3339),
		UpdatedAt: t.Format(time.RFC3339),
	}
}

func TestSimpleCompactor_BelowTarget_NoChange(t *testing.T) {
	c := NewSimpleCompactor()
	memories := []MemoryItem{
		makeMemory("m1", "User likes Python", 1),
		makeMemory("m2", "User prefers dark mode", 2),
	}

	result, err := c.Compact(context.Background(), memories, 10)
	if err != nil {
		t.Fatalf("Compact failed: %v", err)
	}
	if len(result) != 2 {
		t.Errorf("below target: expected 2 items unchanged, got %d", len(result))
	}
}

func TestSimpleCompactor_RemovesExactDuplicates(t *testing.T) {
	c := NewSimpleCompactor()
	content := "User likes Python programming"
	memories := []MemoryItem{
		makeMemory("m1", content, 3),
		makeMemory("m2", content, 2), // Exact duplicate hash
		makeMemory("m3", content, 1), // Exact duplicate hash
		makeMemory("m4", "User prefers dark mode", 1),
	}

	// target=3: triggers compaction path (4 > 3), dedup reduces to 2 unique
	result, err := c.Compact(context.Background(), memories, 3)
	if err != nil {
		t.Fatalf("Compact failed: %v", err)
	}
	// Should deduplicate: only 2 unique memories
	if len(result) != 2 {
		t.Errorf("expected 2 after dedup, got %d", len(result))
	}
}

func TestSimpleCompactor_RespectsTargetCount(t *testing.T) {
	c := NewSimpleCompactor()
	memories := []MemoryItem{
		makeMemory("m1", "User likes Python", 5),
		makeMemory("m2", "User prefers dark mode", 4),
		makeMemory("m3", "User works at Anthropic", 3),
		makeMemory("m4", "User drinks coffee daily", 2),
		makeMemory("m5", "User reads tech blogs", 1),
	}

	result, err := c.Compact(context.Background(), memories, 3)
	if err != nil {
		t.Fatalf("Compact failed: %v", err)
	}
	if len(result) > 3 {
		t.Errorf("expected at most 3 items, got %d", len(result))
	}
}

func TestSimpleCompactor_SkipsSimilarMemories(t *testing.T) {
	c := NewSimpleCompactor()
	// Use memories with >0.75 Jaccard similarity to trigger skip
	// m1: "user likes coffee morning routine daily" (6 words)
	// m2: "user likes coffee morning routine daily extra" (7 words) → common=6, union=7 → 0.857 > 0.75
	memories := []MemoryItem{
		makeMemory("m1", "user likes coffee morning routine daily", 3),
		makeMemory("m2", "user likes coffee morning routine daily extra", 2),
		makeMemory("m3", "user prefers dark mode UI theme", 1),
	}

	// target=2: triggers compaction (3 > 2)
	result, err := c.Compact(context.Background(), memories, 2)
	if err != nil {
		t.Fatalf("Compact failed: %v", err)
	}
	// m1 and m2 are very similar (>0.75) — kept count should respect target
	if len(result) > 2 {
		t.Errorf("similar memories should be skipped, got %d items", len(result))
	}
}

func TestSimpleCompactor_PreservesNewest(t *testing.T) {
	c := NewSimpleCompactor()
	// Add many distinct memories but over target
	memories := []MemoryItem{
		makeMemory("m1", "User likes Python", 10),
		makeMemory("m2", "User prefers dark mode", 9),
		makeMemory("m3", "User works in AI field", 8),
		makeMemory("m4", "User drinks coffee", 7),
		makeMemory("m5", "User reads books", 1), // newest
	}

	result, err := c.Compact(context.Background(), memories, 3)
	if err != nil {
		t.Fatalf("Compact failed: %v", err)
	}

	// Newest (m5) should be preserved
	found := false
	for _, item := range result {
		if item.ID == "m5" {
			found = true
			break
		}
	}
	if !found {
		t.Error("newest memory (m5) should be preserved after compaction")
	}
}

// --- LLMCompactor fallback ---

func TestLLMCompactor_FallbackBelowTarget(t *testing.T) {
	c := NewLLMCompactor(nil) // nil LLM → forces fallback
	memories := []MemoryItem{
		makeMemory("m1", "User likes Python", 1),
		makeMemory("m2", "User prefers dark mode", 2),
	}

	result, err := c.Compact(context.Background(), memories, 10)
	if err != nil {
		t.Fatalf("Compact failed: %v", err)
	}
	if len(result) != 2 {
		t.Errorf("below target: expected 2 unchanged, got %d", len(result))
	}
}

func TestLLMCompactor_FallbackRespectsTarget(t *testing.T) {
	c := NewLLMCompactor(nil)
	memories := []MemoryItem{
		makeMemory("m1", "User likes Python", 5),
		makeMemory("m2", "User prefers dark mode", 4),
		makeMemory("m3", "User works in AI", 3),
		makeMemory("m4", "User drinks coffee", 2),
		makeMemory("m5", "User reads books", 1),
	}

	result, err := c.Compact(context.Background(), memories, 3)
	if err != nil {
		t.Fatalf("Compact failed: %v", err)
	}
	if len(result) > 3 {
		t.Errorf("expected at most 3 items, got %d", len(result))
	}
}

// --- groupSimilarMemories ---

func TestGroupSimilarMemories_EmptyInput(t *testing.T) {
	c := &LLMCompactor{}
	groups := c.groupSimilarMemories(nil)
	if len(groups) != 0 {
		t.Errorf("empty input should return no groups, got %d", len(groups))
	}
}

func TestGroupSimilarMemories_AllDistinct(t *testing.T) {
	c := &LLMCompactor{}
	memories := []MemoryItem{
		makeMemory("m1", "User likes Python", 1),
		makeMemory("m2", "User prefers dark mode", 1),
		makeMemory("m3", "User drinks coffee daily", 1),
	}

	groups := c.groupSimilarMemories(memories)
	// All distinct → each in its own group
	if len(groups) != 3 {
		t.Errorf("3 distinct memories should form 3 groups, got %d", len(groups))
	}
}

func TestGroupSimilarMemories_SimilarGrouped(t *testing.T) {
	c := &LLMCompactor{}
	// Need Jaccard > 0.6 to group: use highly overlapping sentences
	// m1: "user drinks coffee every morning" (5 words)
	// m2: "user drinks coffee every morning daily" (6 words) → common=5, union=6 → 0.833 > 0.6
	memories := []MemoryItem{
		makeMemory("m1", "user drinks coffee every morning", 2),
		makeMemory("m2", "user drinks coffee every morning daily", 1),
		makeMemory("m3", "user prefers dark mode UI theme", 1), // Different
	}

	groups := c.groupSimilarMemories(memories)
	// m1 and m2 should be in same group, m3 alone → 2 groups
	if len(groups) != 2 {
		t.Errorf("expected 2 groups (1 similar pair + 1 distinct), got %d", len(groups))
	}

	// Find the group with 2 items
	twoItemGroup := false
	for _, g := range groups {
		if len(g) == 2 {
			twoItemGroup = true
			break
		}
	}
	if !twoItemGroup {
		t.Error("expected one group with 2 similar memories")
	}
}

// --- parseCompactResponse ---

func TestParseCompactResponse_ValidJSON(t *testing.T) {
	response := `{"merged": "User drinks coffee every morning", "kept_ids": ["m1"], "removed_ids": ["m2"]}`
	result, err := parseCompactResponse(response)
	if err != nil {
		t.Fatalf("parseCompactResponse failed: %v", err)
	}
	if result.Merged != "User drinks coffee every morning" {
		t.Errorf("expected merged content, got %q", result.Merged)
	}
	if len(result.RemovedIDs) != 1 || result.RemovedIDs[0] != "m2" {
		t.Errorf("expected removed_ids=[m2], got %v", result.RemovedIDs)
	}
}

func TestParseCompactResponse_MarkdownBlock(t *testing.T) {
	response := "```json\n{\"merged\": \"User prefers dark mode\"}\n```"
	result, err := parseCompactResponse(response)
	if err != nil {
		t.Fatalf("parseCompactResponse failed: %v", err)
	}
	if result.Merged != "User prefers dark mode" {
		t.Errorf("expected merged content, got %q", result.Merged)
	}
}

func TestParseCompactResponse_PlainText(t *testing.T) {
	response := "User prefers dark mode themes in all applications"
	result, err := parseCompactResponse(response)
	if err != nil {
		t.Fatalf("parseCompactResponse failed: %v", err)
	}
	if result.Merged != response {
		t.Errorf("plain text should be returned as merged, got %q", result.Merged)
	}
}
