package memory

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// DecisionType represents the action to take for a memory.
type DecisionType string

const (
	DecisionAdd    DecisionType = "ADD"    // Add as new memory
	DecisionUpdate DecisionType = "UPDATE" // Update existing memory
	DecisionDelete DecisionType = "DELETE" // Delete existing memory
	DecisionNoop   DecisionType = "NOOP"   // No action needed
)

// Decision represents the outcome of comparing a new memory against existing ones.
type Decision struct {
	Type       DecisionType `json:"type"`        // ADD, UPDATE, DELETE, or NOOP
	MemoryID   string       `json:"memory_id"`   // ID of existing memory (for UPDATE/DELETE)
	Reason     string       `json:"reason"`      // Explanation for the decision
	NewContent string       `json:"new_content"` // Updated content (for UPDATE)
}

// Decider decides how to handle a new memory against existing memories.
type Decider interface {
	// Decide compares a new memory against existing ones and returns the action to take.
	// This prevents duplicate memories and handles updates intelligently.
	Decide(ctx context.Context, newMemory string, existing []MemoryItem) (*Decision, error)
}

// LLMDecider uses an LLM to make intelligent decisions about memory conflicts.
type LLMDecider struct {
	llm LLMClient
}

// NewLLMDecider creates a new LLM-based decider.
func NewLLMDecider(llm LLMClient) *LLMDecider {
	return &LLMDecider{llm: llm}
}

// Decide uses LLM to determine the best action for a new memory.
func (d *LLMDecider) Decide(ctx context.Context, newMemory string, existing []MemoryItem) (*Decision, error) {
	if d.llm == nil {
		// Fallback: check for exact duplicates using hash
		return d.fallbackDecide(newMemory, existing)
	}

	// If no existing memories, always add
	if len(existing) == 0 {
		return &Decision{Type: DecisionAdd, Reason: "No existing memories"}, nil
	}

	// Build the decision prompt
	prompt := d.buildDecisionPrompt(newMemory, existing)

	response, err := d.llm.ChatComplete(ctx, decisionSystemPrompt, prompt)
	if err != nil {
		// Fallback on LLM error
		return d.fallbackDecide(newMemory, existing)
	}

	// Parse the decision
	decision, err := d.parseDecision(response)
	if err != nil {
		// Fallback on parse error
		return d.fallbackDecide(newMemory, existing)
	}

	return decision, nil
}

// fallbackDecide checks for exact hash matches when LLM is unavailable.
func (d *LLMDecider) fallbackDecide(newMemory string, existing []MemoryItem) (*Decision, error) {
	newHash := hashMemory(newMemory)

	for _, item := range existing {
		if item.Hash == newHash {
			return &Decision{
				Type:   DecisionNoop,
				Reason: "Exact duplicate (hash match)",
			}, nil
		}
	}

	return &Decision{Type: DecisionAdd, Reason: "No exact match found"}, nil
}

// buildDecisionPrompt creates the prompt for the LLM decision.
func (d *LLMDecider) buildDecisionPrompt(newMemory string, existing []MemoryItem) string {
	var b strings.Builder

	b.WriteString("NEW MEMORY TO STORE:\n")
	b.WriteString(newMemory)
	b.WriteString("\n\n")

	b.WriteString("EXISTING MEMORIES:\n")
	for i, item := range existing {
		b.WriteString(fmt.Sprintf("%d. [ID: %s] %s\n", i+1, item.ID, item.Memory))
	}

	b.WriteString("\nCompare the new memory against existing ones.\n")
	b.WriteString("Rules:\n")
	b.WriteString("- If the new memory is semantically identical or very similar to an existing one → UPDATE\n")
	b.WriteString("- If the new memory conflicts with or replaces an existing one → UPDATE\n")
	b.WriteString("- If the new memory is completely new and unique → ADD\n")
	b.WriteString("- If the new memory adds no value (already known) → NOOP\n")

	return b.String()
}

// parseDecision extracts the decision from LLM response.
func (d *LLMDecider) parseDecision(response string) (*Decision, error) {
	response = strings.TrimSpace(response)

	// Try JSON parsing first
	if strings.HasPrefix(response, "{") {
		var decision Decision
		if err := json.Unmarshal([]byte(response), &decision); err == nil {
			return &decision, nil
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
		response = strings.TrimSpace(response)
	}

	// Try JSON parsing again
	if strings.HasPrefix(response, "{") {
		var decision Decision
		if err := json.Unmarshal([]byte(response), &decision); err == nil {
			return &decision, nil
		}
	}

	// Fallback: parse simple text format
	return d.parseSimpleDecision(response)
}

// parseSimpleDecision handles non-JSON LLM responses.
func (d *LLMDecider) parseSimpleDecision(response string) (*Decision, error) {
	response = strings.ToUpper(strings.TrimSpace(response))

	decision := &Decision{Type: DecisionAdd, Reason: "Fallback: parsed from text"}

	if strings.Contains(response, "NOOP") || strings.Contains(response, "SKIP") {
		decision.Type = DecisionNoop
	} else if strings.Contains(response, "UPDATE") {
		decision.Type = DecisionUpdate
		// Try to extract memory ID
		for _, line := range strings.Split(response, "\n") {
			if strings.Contains(line, "ID:") {
				parts := strings.Split(line, "ID:")
				if len(parts) > 1 {
					decision.MemoryID = strings.TrimSpace(parts[1])
				}
			}
		}
	} else if strings.Contains(response, "DELETE") {
		decision.Type = DecisionDelete
	}

	return decision, nil
}

// decisionSystemPrompt is the system prompt for the decider LLM.
const decisionSystemPrompt = `You are a memory management system that decides how to handle new memories.

Your task: Compare a NEW memory against EXISTING memories and decide the appropriate action.

Decision types:
- ADD: The new memory is completely new and unique, store it as a new entry
- UPDATE: The new memory updates or refines an existing memory (e.g., "User likes coffee" → "User prefers tea")
- DELETE: The new memory invalidates an existing memory (rare, use with caution)
- NOOP: The new memory is identical or adds no new information (duplicate)

Output format (JSON):
{
  "type": "ADD|UPDATE|DELETE|NOOP",
  "memory_id": "existing_memory_id (for UPDATE/DELETE)",
  "reason": "brief explanation",
  "new_content": "updated content (for UPDATE, if content changed)"
}

Guidelines:
- Be conservative: prefer ADD over UPDATE unless clearly the same topic
- Minor wording differences should be NOOP, not UPDATE
- User preferences that change over time should UPDATE the old preference
- Be concise in your reasoning`

// SimpleDecider uses rule-based heuristics without LLM.
type SimpleDecider struct{}

// NewSimpleDecider creates a rule-based decider (no LLM required).
func NewSimpleDecider() *SimpleDecider {
	return &SimpleDecider{}
}

// Decide makes decisions based on hash matching and simple text similarity.
func (d *SimpleDecider) Decide(_ context.Context, newMemory string, existing []MemoryItem) (*Decision, error) {
	newHash := hashMemory(newMemory)
	newLower := strings.ToLower(newMemory)

	for _, item := range existing {
		// Exact hash match → NOOP
		if item.Hash == newHash {
			return &Decision{
				Type:   DecisionNoop,
				Reason: "Exact duplicate (hash match)",
			}, nil
		}

		// High text similarity → UPDATE (if same topic, different detail)
		similarity := calculateSimilarity(newLower, strings.ToLower(item.Memory))
		if similarity > 0.85 {
			return &Decision{
				Type:       DecisionUpdate,
				MemoryID:   item.ID,
				Reason:     "High similarity detected",
				NewContent: newMemory,
			}, nil
		}

		// Medium similarity → check if it's an update to existing
		if similarity > 0.6 {
			// Check if new memory contains the old one (refinement)
			if strings.Contains(newLower, strings.ToLower(item.Memory)) {
				return &Decision{
					Type:       DecisionUpdate,
					MemoryID:   item.ID,
					Reason:     "New memory extends existing one",
					NewContent: newMemory,
				}, nil
			}
		}
	}

	return &Decision{Type: DecisionAdd, Reason: "No similar memories found"}, nil
}

// calculateSimilarity returns a simple similarity score (0-1) between two strings.
func calculateSimilarity(a, b string) float64 {
	if a == b {
		return 1.0
	}
	if len(a) == 0 || len(b) == 0 {
		return 0.0
	}

	// Simple word overlap similarity
	wordsA := strings.Fields(a)
	wordsB := strings.Fields(b)

	if len(wordsA) == 0 || len(wordsB) == 0 {
		return 0.0
	}

	// Count common words
	setA := make(map[string]struct{}, len(wordsA))
	for _, w := range wordsA {
		setA[w] = struct{}{}
	}

	common := 0
	for _, w := range wordsB {
		if _, ok := setA[w]; ok {
			common++
		}
	}

	// Jaccard similarity
	union := len(wordsA) + len(wordsB) - common
	if union == 0 {
		return 0.0
	}

	return float64(common) / float64(union)
}

// Ensure implementations satisfy the interface.
var _ Decider = (*LLMDecider)(nil)
var _ Decider = (*SimpleDecider)(nil)
