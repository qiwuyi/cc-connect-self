package memory

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// LLMClient defines the interface for LLM operations needed by the extractor.
type LLMClient interface {
	// ChatComplete sends messages to the LLM and returns the response.
	ChatComplete(ctx context.Context, systemPrompt, userPrompt string) (string, error)
}

// DefaultExtractor extracts facts from conversations using an LLM.
type DefaultExtractor struct {
	llm LLMClient
}

// NewDefaultExtractor creates a new fact extractor.
func NewDefaultExtractor(llm LLMClient) *DefaultExtractor {
	return &DefaultExtractor{llm: llm}
}

// Extract extracts factual memories from conversation messages.
func (e *DefaultExtractor) Extract(ctx context.Context, messages []MemoryMessage) ([]string, error) {
	if e.llm == nil {
		return nil, ErrExtractorNotSet
	}

	if len(messages) == 0 {
		return nil, nil
	}

	// Format conversation for the LLM
	var conversation strings.Builder
	for _, msg := range messages {
		switch msg.Role {
		case "user":
			conversation.WriteString("User: ")
		case "assistant":
			conversation.WriteString("Assistant: ")
		default:
			conversation.WriteString(msg.Role + ": ")
		}
		conversation.WriteString(msg.Content)
		conversation.WriteString("\n")
	}

	systemPrompt := `You are a memory extraction system. Your task is to identify and extract important factual information from conversations that should be remembered for future interactions.

Extract facts that are:
1. User preferences (e.g., "User prefers concise responses")
2. Important personal information (e.g., "User is working on project X")
3. Context about ongoing tasks or projects
4. Recurring patterns or habits mentioned

Do NOT extract:
- Temporary or one-time information
- Generic pleasantries
- Information already in the conversation context
- Technical details that don't need remembering

Output ONLY a JSON array of strings, one fact per string. If nothing important to remember, return an empty array [].
Keep each fact concise (under 100 characters when possible).
Output facts in the language used in the conversation (Chinese, English, etc.).`

	userPrompt := fmt.Sprintf("Extract important facts from this conversation:\n\n%s", conversation.String())

	response, err := e.llm.ChatComplete(ctx, systemPrompt, userPrompt)
	if err != nil {
		return nil, fmt.Errorf("LLM call failed: %w", err)
	}

	// Parse JSON array from response
	response = strings.TrimSpace(response)

	// Handle markdown code blocks
	if strings.HasPrefix(response, "```") {
		// Find the end of the code block
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
	}

	var facts []string
	if err := json.Unmarshal([]byte(response), &facts); err != nil {
		// Try to extract facts from non-JSON response
		facts = extractFactsFromText(response)
	}

	// Clean and validate facts
	var cleanFacts []string
	for _, fact := range facts {
		fact = strings.TrimSpace(fact)
		if fact == "" || len(fact) < 5 {
			continue
		}
		cleanFacts = append(cleanFacts, fact)
	}

	return cleanFacts, nil
}

// extractFactsFromText attempts to extract facts from non-JSON text.
func extractFactsFromText(text string) []string {
	var facts []string
	lines := strings.Split(text, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		// Skip empty lines and markers
		if line == "" || line == "[" || line == "]" || line == "{" || line == "}" {
			continue
		}
		// Remove quotes and commas
		line = strings.Trim(line, `",`)
		line = strings.TrimSpace(line)
		if line != "" && len(line) > 5 {
			facts = append(facts, line)
		}
	}
	return facts
}

// Ensure DefaultExtractor implements Extractor interface.
var _ Extractor = (*DefaultExtractor)(nil)
