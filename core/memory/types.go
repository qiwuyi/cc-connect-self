// Package memory provides cross-session memory for cc-connect-memory.
// It implements a simple file-based memory system ( "off" mode)
// without vector dependencies.
package memory

// MemoryItem represents a single memory entry.
type MemoryItem struct {
	ID        string         `json:"id"`
	Memory    string         `json:"memory"`
	Hash      string         `json:"hash,omitempty"`
	CreatedAt string         `json:"created_at,omitempty"`
	UpdatedAt string         `json:"updated_at,omitempty"`
	Score     float64        `json:"score,omitempty"`
	Metadata  map[string]any `json:"metadata,omitempty"`
}

// BeforeChatRequest is passed to OnBeforeChat before sending to the agent.
type BeforeChatRequest struct {
	Query     string         // User's current message
	SessionID string         // Session identifier
	Platform  string         // Platform name (e.g., "feishu", "telegram")
	UserID    string         // User identifier (optional)
	ChatID    string         // Chat/group identifier (optional)
	Filters   map[string]any // Additional filters for memory search (optional)
}

// BeforeChatResult contains memory context to inject into the conversation.
type BeforeChatResult struct {
	ContextText string // Formatted text to inject as context
}

// AfterChatRequest is passed to OnAfterChat after the conversation turn.
type AfterChatRequest struct {
	SessionID  string          // Session identifier
	Platform   string          // Platform name
	UserID     string          // User identifier (optional)
	ChatID     string          // Chat/group identifier (optional)
	Messages   []MemoryMessage // Conversation history
	AgentReply string          // Agent's reply (for context)
}

// MemoryMessage represents a single message in the conversation.
type MemoryMessage struct {
	Role    string `json:"role"`    // "user" or "assistant"
	Content string `json:"content"` // Message content
}

// SearchRequest parameters for memory search.
type SearchRequest struct {
	Query   string         `json:"query"`
	Limit   int            `json:"limit,omitempty"`
	Filters map[string]any `json:"filters,omitempty"`
}

// SearchResponse from memory search.
type SearchResponse struct {
	Results []MemoryItem `json:"results"`
	Total   int          `json:"total"`
}

// AddRequest for adding new memories.
type AddRequest struct {
	Memory   string         `json:"memory"`
	Metadata map[string]any `json:"metadata,omitempty"`
}

// ProfileMetadata keys for user profile information.
const (
	MetadataKeyUserID          = "profile_user_id"
	MetadataKeyChannelIdentity = "profile_channel_identity_id"
	MetadataKeyDisplayName     = "profile_display_name"
	MetadataKeyPlatform        = "profile_platform"
	MetadataKeyRef             = "profile_ref"
)

// BuildProfileMetadata creates metadata with user profile information.
func BuildProfileMetadata(userID, channelIdentityID, displayName, platform string) map[string]any {
	m := make(map[string]any)
	if userID != "" {
		m[MetadataKeyUserID] = userID
		m[MetadataKeyRef] = "user:" + userID
	}
	if channelIdentityID != "" {
		m[MetadataKeyChannelIdentity] = channelIdentityID
		if userID == "" {
			m[MetadataKeyRef] = "channel_identity:" + channelIdentityID
		}
	}
	if displayName != "" {
		m[MetadataKeyDisplayName] = displayName
	}
	if platform != "" {
		m[MetadataKeyPlatform] = platform
	}
	if len(m) == 0 {
		return nil
	}
	return m
}

// MemoryConfig for configuring memory system behavior.
type MemoryConfig struct {
	Enabled               bool    // Enable/disable memory system
	DebounceSeconds       int     // Seconds to wait before processing (debounce)
	MaxFacts              int     // Maximum number of facts to store
	FactConfidenceThreshold float64 // Minimum confidence for storing facts (0.0-1.0)
	InjectionEnabled      bool    // Whether to inject memory into system prompt
	MaxInjectionTokens    int     // Maximum tokens for memory injection
}

// DefaultMemoryConfig returns the default configuration.
func DefaultMemoryConfig() MemoryConfig {
	return MemoryConfig{
		Enabled:               true,
		DebounceSeconds:      30,
		MaxFacts:             100,
		FactConfidenceThreshold: 0.7,
		InjectionEnabled:     true,
		MaxInjectionTokens:   2000,
	}
}

// Constants for memory system configuration.
const (
	DefaultMemoryLimit      = 8   // Max memories to retrieve
	MaxMemoryContextItems  = 16  // Max items in context
	MemoryContextMaxChars  = 220 // Max chars per memory in context
)

// DeduplicateItems removes duplicate MemoryItems by ID.
func DeduplicateItems(items []MemoryItem) []MemoryItem {
	if len(items) == 0 {
		return items
	}
	seen := make(map[string]struct{}, len(items))
	result := make([]MemoryItem, 0, len(items))
	for _, item := range items {
		id := item.ID
		if id == "" {
			id = item.Memory // Fallback to memory content as ID
		}
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		result = append(result, item)
	}
	return result
}

// MergeMetadata merges extra metadata into base metadata.
func MergeMetadata(base map[string]any, extra map[string]any) map[string]any {
	if len(base) == 0 && len(extra) == 0 {
		return nil
	}
	out := make(map[string]any, len(base)+len(extra))
	for k, v := range base {
		out[k] = v
	}
	for k, v := range extra {
		out[k] = v
	}
	return out
}
