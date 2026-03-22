package memory

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
)

// Compactor merges and compresses memories when they grow too numerous.
type Compactor interface {
	// Compact analyzes memories and returns a condensed set.
	// The goal is to preserve important information while reducing memory count.
	Compact(ctx context.Context, memories []MemoryItem, targetCount int) ([]MemoryItem, error)
}

// LLMCompactor uses an LLM to intelligently merge related memories.
type LLMCompactor struct {
	llm LLMClient
}

// NewLLMCompactor creates a new LLM-based compactor.
func NewLLMCompactor(llm LLMClient) *LLMCompactor {
	return &LLMCompactor{llm: llm}
}

// Compact merges similar memories to reduce total count.
func (c *LLMCompactor) Compact(ctx context.Context, memories []MemoryItem, targetCount int) ([]MemoryItem, error) {
	if c.llm == nil {
		return c.fallbackCompact(memories, targetCount)
	}

	if len(memories) <= targetCount {
		return memories, nil
	}

	// Group memories by similarity for batch processing
	groups := c.groupSimilarMemories(memories)

	var compacted []MemoryItem
	for _, group := range groups {
		if len(group) == 1 {
			compacted = append(compacted, group[0])
			continue
		}

		// Merge this group
		merged, err := c.mergeGroup(ctx, group)
		if err != nil {
			// On error, keep the most recent memory from the group
			sort.Slice(group, func(i, j int) bool {
				return group[i].CreatedAt > group[j].CreatedAt
			})
			compacted = append(compacted, group[0])
			continue
		}
		compacted = append(compacted, merged)
	}

	// If still over target, recursively compact
	if len(compacted) > targetCount {
		return c.Compact(ctx, compacted, targetCount)
	}

	return compacted, nil
}

// fallbackCompact uses simple rules when LLM is unavailable.
func (c *LLMCompactor) fallbackCompact(memories []MemoryItem, targetCount int) ([]MemoryItem, error) {
	if len(memories) <= targetCount {
		return memories, nil
	}

	// Sort by recency (newest first)
	sort.Slice(memories, func(i, j int) bool {
		return memories[i].CreatedAt > memories[j].CreatedAt
	})

	// Keep the most recent ones, grouped by similarity
	kept := make(map[string]bool)
	var result []MemoryItem

	for _, mem := range memories {
		if len(result) >= targetCount {
			break
		}

		// Check if we already have something similar
		similar := false
		memLower := strings.ToLower(mem.Memory)
		for _, existing := range result {
			if calculateSimilarity(memLower, strings.ToLower(existing.Memory)) > 0.7 {
				similar = true
				break
			}
		}

		if !similar && !kept[mem.ID] {
			result = append(result, mem)
			kept[mem.ID] = true
		}
	}

	return result, nil
}

// groupSimilarMemories groups memories by semantic similarity.
func (c *LLMCompactor) groupSimilarMemories(memories []MemoryItem) [][]MemoryItem {
	if len(memories) == 0 {
		return nil
	}

	// Sort by recency
	sort.Slice(memories, func(i, j int) bool {
		return memories[i].CreatedAt > memories[j].CreatedAt
	})

	var groups [][]MemoryItem
	used := make(map[string]bool)

	for i, mem := range memories {
		if used[mem.ID] {
			continue
		}

		group := []MemoryItem{mem}
		used[mem.ID] = true

		memLower := strings.ToLower(mem.Memory)

		// Find similar memories
		for j := i + 1; j < len(memories); j++ {
			other := memories[j]
			if used[other.ID] {
				continue
			}

			similarity := calculateSimilarity(memLower, strings.ToLower(other.Memory))
			if similarity > 0.6 {
				group = append(group, other)
				used[other.ID] = true
			}
		}

		groups = append(groups, group)
	}

	return groups
}

// mergeGroup uses LLM to merge a group of related memories.
func (c *LLMCompactor) mergeGroup(ctx context.Context, group []MemoryItem) (MemoryItem, error) {
	if len(group) == 0 {
		return MemoryItem{}, fmt.Errorf("empty group")
	}
	if len(group) == 1 {
		return group[0], nil
	}

	// Build merge prompt
	var memoriesText strings.Builder
	for i, mem := range group {
		memoriesText.WriteString(fmt.Sprintf("%d. %s\n", i+1, mem.Memory))
	}

	prompt := fmt.Sprintf(`Merge these related memories into a single, concise memory that captures the essential information:

%s

Requirements:
- Preserve all important facts
- Eliminate redundancy
- Be concise (max 150 characters)
- Use clear, factual language

Output only the merged memory text, no explanation.`, memoriesText.String())

	mergedContent, err := c.llm.ChatComplete(ctx, compactSystemPrompt, prompt)
	if err != nil {
		return MemoryItem{}, err
	}

	mergedContent = strings.TrimSpace(mergedContent)
	if len(mergedContent) > 200 {
		mergedContent = mergedContent[:200] + "..."
	}

	// Use the newest memory's metadata
	sort.Slice(group, func(i, j int) bool {
		return group[i].CreatedAt > group[j].CreatedAt
	})
	newest := group[0]

	return MemoryItem{
		ID:        generateMemoryID(),
		Memory:    mergedContent,
		Hash:      hashMemory(mergedContent),
		CreatedAt: newest.CreatedAt,
		UpdatedAt: time.Now().UTC().Format(time.RFC3339),
		Metadata:  newest.Metadata,
	}, nil
}

// compactSystemPrompt guides the LLM for memory compression.
const compactSystemPrompt = `You are a memory compression system. Your task is to merge multiple related memories into a single, concise memory.

Guidelines:
- Combine facts that are about the same topic or person
- Preserve specific details and preferences
- Remove redundant or overlapping information
- Maintain factual accuracy
- Output should be clear and concise (under 150 characters)

Example:
Input:
1. User likes drinking coffee in the morning
2. User prefers dark roast coffee
3. User drinks 2 cups of coffee daily

Output:
User drinks 2 cups of dark roast coffee every morning.

Output ONLY the merged memory text, nothing else.`

// CompactConfig defines when and how to compact memories.
type CompactConfig struct {
	Enabled       bool  // Whether compaction is enabled
	Threshold     int   // Trigger compaction when memory count exceeds this
	TargetCount   int   // Target memory count after compaction
	MinAgeDays    int   // Only compact memories older than this many days
	MaxCompactions int  // Maximum number of memories to compact per run
}

// DefaultCompactConfig provides sensible defaults.
var DefaultCompactConfig = CompactConfig{
	Enabled:        true,
	Threshold:      50,
	TargetCount:    30,
	MinAgeDays:     7,
	MaxCompactions: 20,
}

// CompactResult contains statistics about a compaction run.
type CompactResult struct {
	BeforeCount int      // Number of memories before
	AfterCount  int      // Number of memories after
	MergedIDs   []string // IDs of memories that were merged/removed
	NewMemories []MemoryItem // New merged memories
}

// SimpleCompactor uses rule-based merging without LLM.
type SimpleCompactor struct{}

// NewSimpleCompactor creates a rule-based compactor.
func NewSimpleCompactor() *SimpleCompactor {
	return &SimpleCompactor{}
}

// Compact merges exact duplicates and similar memories using rules.
func (c *SimpleCompactor) Compact(_ context.Context, memories []MemoryItem, targetCount int) ([]MemoryItem, error) {
	if len(memories) <= targetCount {
		return memories, nil
	}

	// Sort by recency
	sort.Slice(memories, func(i, j int) bool {
		return memories[i].CreatedAt > memories[j].CreatedAt
	})

	// Remove exact duplicates by hash
	seenHashes := make(map[string]bool)
	var deduped []MemoryItem
	for _, mem := range memories {
		if !seenHashes[mem.Hash] {
			seenHashes[mem.Hash] = true
			deduped = append(deduped, mem)
		}
	}

	if len(deduped) <= targetCount {
		return deduped, nil
	}

	// Keep most recent, skipping very similar ones
	var result []MemoryItem
	for _, mem := range deduped {
		if len(result) >= targetCount {
			break
		}

		// Check similarity with already kept memories
		similar := false
		memLower := strings.ToLower(mem.Memory)
		for _, existing := range result {
			if calculateSimilarity(memLower, strings.ToLower(existing.Memory)) > 0.75 {
				similar = true
				break
			}
		}

		if !similar {
			result = append(result, mem)
		}
	}

	return result, nil
}

// NeedsCompaction checks if compaction should run based on config.
func NeedsCompaction(count int, config CompactConfig) bool {
	if !config.Enabled {
		return false
	}
	return count > config.Threshold
}

// compactResponse is used to parse LLM compact output.
type compactResponse struct {
	Merged     string   `json:"merged"`
	KeptIDs    []string `json:"kept_ids"`
	RemovedIDs []string `json:"removed_ids"`
}

// parseCompactResponse extracts compact result from LLM output.
func parseCompactResponse(response string) (*compactResponse, error) {
	response = strings.TrimSpace(response)

	// Try JSON parsing
	if strings.HasPrefix(response, "{") {
		var result compactResponse
		if err := json.Unmarshal([]byte(response), &result); err == nil {
			return &result, nil
		}
	}

	// Handle markdown code blocks
	if strings.HasPrefix(response, "```") {
		lines := strings.Split(response, "\n")
		var jsonLines []string
		inBlock := false
		for _, line := range lines {
			if strings.HasPrefix(line, "```") {
				if inBlock {
					break
				}
				inBlock = true
				continue
			}
			if inBlock {
				jsonLines = append(jsonLines, line)
			}
		}
		response = strings.Join(jsonLines, "\n")

		var result compactResponse
		if err := json.Unmarshal([]byte(response), &result); err == nil {
			return &result, nil
		}
	}

	// Return simple result with full text as merged
	return &compactResponse{Merged: response}, nil
}

// Ensure implementations satisfy the interface.
var _ Compactor = (*LLMCompactor)(nil)
var _ Compactor = (*SimpleCompactor)(nil)
