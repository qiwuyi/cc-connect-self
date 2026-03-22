package memory

import "context"

// Provider is the core interface for the memory system.
// Implementations handle storage, retrieval, and lifecycle of memories.
type Provider interface {
	// OnBeforeChat retrieves relevant memories before a conversation turn.
	// Returns context text to inject into the system prompt.
	OnBeforeChat(ctx context.Context, req BeforeChatRequest) (*BeforeChatResult, error)

	// OnAfterChat stores new memories after a conversation turn.
	// Extracts facts from the conversation and persists them.
	OnAfterChat(ctx context.Context, req AfterChatRequest) error

	// Search retrieves memories matching the query.
	Search(ctx context.Context, req SearchRequest) (*SearchResponse, error)

	// Add manually adds a memory entry.
	Add(ctx context.Context, req AddRequest) (*MemoryItem, error)

	// GetAll retrieves all memories, optionally filtered.
	GetAll(ctx context.Context, filters map[string]any) ([]MemoryItem, error)

	// Update modifies an existing memory by ID.
	Update(ctx context.Context, id string, memory string) (*MemoryItem, error)

	// Delete removes a memory by ID.
	Delete(ctx context.Context, id string) error

	// DeleteBatch removes multiple memories by IDs.
	DeleteBatch(ctx context.Context, ids []string) error

	// DeleteAll removes all memories, optionally filtered.
	DeleteAll(ctx context.Context, filters map[string]any) error

	// Usage returns memory statistics.
	Usage(ctx context.Context, filters map[string]any) (*UsageResponse, error)

	// Compact performs memory compaction if needed.
	// Returns statistics about the compaction run.
	Compact(ctx context.Context) (*CompactResult, error)
}

// Extractor defines the interface for extracting facts from conversations.
// Implementations typically use an LLM to identify important information.
type Extractor interface {
	// Extract extracts factual memories from conversation messages.
	Extract(ctx context.Context, messages []MemoryMessage) ([]string, error)
}

// NoopProvider is a no-op implementation that does nothing.
// Used when memory is disabled.
type NoopProvider struct{}

func (*NoopProvider) OnBeforeChat(_ context.Context, _ BeforeChatRequest) (*BeforeChatResult, error) {
	return nil, nil
}

func (*NoopProvider) OnAfterChat(_ context.Context, _ AfterChatRequest) error {
	return nil
}

func (*NoopProvider) Search(_ context.Context, _ SearchRequest) (*SearchResponse, error) {
	return &SearchResponse{}, nil
}

func (*NoopProvider) Add(_ context.Context, _ AddRequest) (*MemoryItem, error) {
	return nil, nil
}

func (*NoopProvider) GetAll(_ context.Context, _ map[string]any) ([]MemoryItem, error) {
	return nil, nil
}

func (*NoopProvider) Update(_ context.Context, _ string, _ string) (*MemoryItem, error) {
	return nil, nil
}

func (*NoopProvider) Delete(_ context.Context, _ string) error {
	return nil
}

func (*NoopProvider) DeleteBatch(_ context.Context, _ []string) error {
	return nil
}

func (*NoopProvider) DeleteAll(_ context.Context, _ map[string]any) error {
	return nil
}

func (*NoopProvider) Usage(_ context.Context, _ map[string]any) (*UsageResponse, error) {
	return &UsageResponse{}, nil
}

func (*NoopProvider) Compact(_ context.Context) (*CompactResult, error) {
	return &CompactResult{}, nil
}
