package memory

import (
	"context"
	"os"
	"testing"
)

func TestStoreFS_AddAndGetAll(t *testing.T) {
	tmp := t.TempDir()
	store := NewStoreFS(tmp, nil, nil)

	// Add a memory
	item, err := store.Add(context.Background(), AddRequest{
		Memory:   "User prefers concise responses",
		Metadata: map[string]any{"user_id": "test123"},
	})
	if err != nil {
		t.Fatalf("Add failed: %v", err)
	}

	if item.ID == "" {
		t.Error("expected non-empty ID")
	}
	if item.Memory != "User prefers concise responses" {
		t.Errorf("expected memory content, got %q", item.Memory)
	}

	// GetAll should return the memory
	items, err := store.GetAll(context.Background(), nil)
	if err != nil {
		t.Fatalf("GetAll failed: %v", err)
	}

	if len(items) != 1 {
		t.Errorf("expected 1 item, got %d", len(items))
	}
	if items[0].Memory != "User prefers concise responses" {
		t.Errorf("expected memory content, got %q", items[0].Memory)
	}
}

func TestStoreFS_Search(t *testing.T) {
	tmp := t.TempDir()
	store := NewStoreFS(tmp, nil, nil)

	// Add some memories
	_, _ = store.Add(context.Background(), AddRequest{Memory: "User likes Python programming"})
	_, _ = store.Add(context.Background(), AddRequest{Memory: "User prefers dark mode"})
	_, _ = store.Add(context.Background(), AddRequest{Memory: "User works on Go projects"})

	// Search for "User"
	results, err := store.Search(context.Background(), SearchRequest{Query: "Python"})
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}

	if len(results.Results) != 1 {
		t.Errorf("expected 1 result for 'Python', got %d", len(results.Results))
	}
	if results.Total != 1 {
		t.Errorf("expected total 1, got %d", results.Total)
	}
}

func TestStoreFS_Search_EmptyQuery(t *testing.T) {
	tmp := t.TempDir()
	store := NewStoreFS(tmp, nil, nil)

	_, _ = store.Add(context.Background(), AddRequest{Memory: "Test memory 1"})
	_, _ = store.Add(context.Background(), AddRequest{Memory: "Test memory 2"})

	// Empty query should return all
	results, err := store.Search(context.Background(), SearchRequest{Query: ""})
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}

	if len(results.Results) != 2 {
		t.Errorf("expected 2 results for empty query, got %d", len(results.Results))
	}
}

func TestStoreFS_Delete(t *testing.T) {
	tmp := t.TempDir()
	store := NewStoreFS(tmp, nil, nil)

	// Add a memory
	item, _ := store.Add(context.Background(), AddRequest{Memory: "Memory to delete"})

	// Verify it exists
	items, _ := store.GetAll(context.Background(), nil)
	if len(items) != 1 {
		t.Fatalf("expected 1 item before delete, got %d", len(items))
	}

	// Delete it
	err := store.Delete(context.Background(), item.ID)
	if err != nil {
		t.Fatalf("Delete failed: %v", err)
	}

	// Verify it's gone
	items, _ = store.GetAll(context.Background(), nil)
	if len(items) != 0 {
		t.Errorf("expected 0 items after delete, got %d", len(items))
	}
}

func TestStoreFS_Delete_NotFound(t *testing.T) {
	tmp := t.TempDir()
	store := NewStoreFS(tmp, nil, nil)

	err := store.Delete(context.Background(), "nonexistent")
	if err != ErrNotFound {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestStoreFS_Update(t *testing.T) {
	tmp := t.TempDir()
	store := NewStoreFS(tmp, nil, nil)

	// Add a memory
	item, _ := store.Add(context.Background(), AddRequest{Memory: "Original memory content"})

	// Update it
	updated, err := store.Update(context.Background(), item.ID, "Updated memory content")
	if err != nil {
		t.Fatalf("Update failed: %v", err)
	}

	if updated.Memory != "Updated memory content" {
		t.Errorf("expected updated memory, got %q", updated.Memory)
	}
	if updated.Hash == "" {
		t.Error("expected hash to be set")
	}
	if updated.UpdatedAt == "" {
		t.Error("expected UpdatedAt to be set")
	}

	// Verify persistence
	items, _ := store.GetAll(context.Background(), nil)
	if len(items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(items))
	}
	if items[0].Memory != "Updated memory content" {
		t.Errorf("expected persisted update, got %q", items[0].Memory)
	}
}

func TestStoreFS_Update_NotFound(t *testing.T) {
	tmp := t.TempDir()
	store := NewStoreFS(tmp, nil, nil)

	_, err := store.Update(context.Background(), "nonexistent", "new content")
	if err != ErrNotFound {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestStoreFS_DeleteBatch(t *testing.T) {
	tmp := t.TempDir()
	store := NewStoreFS(tmp, nil, nil)

	// Add multiple memories
	item1, _ := store.Add(context.Background(), AddRequest{Memory: "Memory 1"})
	item2, _ := store.Add(context.Background(), AddRequest{Memory: "Memory 2"})
	item3, _ := store.Add(context.Background(), AddRequest{Memory: "Memory 3"})

	// Delete batch
	err := store.DeleteBatch(context.Background(), []string{item1.ID, item3.ID})
	if err != nil {
		t.Fatalf("DeleteBatch failed: %v", err)
	}

	// Verify only item2 remains
	items, _ := store.GetAll(context.Background(), nil)
	if len(items) != 1 {
		t.Errorf("expected 1 item after batch delete, got %d", len(items))
	}
	if items[0].ID != item2.ID {
		t.Errorf("expected item2 to remain, got %v", items[0])
	}
}

func TestStoreFS_DeleteBatch_Empty(t *testing.T) {
	tmp := t.TempDir()
	store := NewStoreFS(tmp, nil, nil)

	_, _ = store.Add(context.Background(), AddRequest{Memory: "Memory 1"})

	// Empty batch should be no-op
	err := store.DeleteBatch(context.Background(), []string{})
	if err != nil {
		t.Errorf("empty batch should not error, got %v", err)
	}

	items, _ := store.GetAll(context.Background(), nil)
	if len(items) != 1 {
		t.Errorf("expected 1 item, got %d", len(items))
	}
}

func TestStoreFS_DeleteAll(t *testing.T) {
	tmp := t.TempDir()
	store := NewStoreFS(tmp, nil, nil)

	// Add multiple memories
	_, _ = store.Add(context.Background(), AddRequest{Memory: "Memory 1"})
	_, _ = store.Add(context.Background(), AddRequest{Memory: "Memory 2"})
	_, _ = store.Add(context.Background(), AddRequest{Memory: "Memory 3"})

	// Delete all
	err := store.DeleteAll(context.Background(), nil)
	if err != nil {
		t.Fatalf("DeleteAll failed: %v", err)
	}

	// Verify all gone
	items, _ := store.GetAll(context.Background(), nil)
	if len(items) != 0 {
		t.Errorf("expected 0 items after DeleteAll, got %d", len(items))
	}
}

func TestStoreFS_Usage(t *testing.T) {
	tmp := t.TempDir()
	store := NewStoreFS(tmp, nil, nil)

	_, _ = store.Add(context.Background(), AddRequest{Memory: "Short"})
	_, _ = store.Add(context.Background(), AddRequest{Memory: "A longer memory entry with more text"})

	usage, err := store.Usage(context.Background(), nil)
	if err != nil {
		t.Fatalf("Usage failed: %v", err)
	}

	if usage.Count != 2 {
		t.Errorf("expected count 2, got %d", usage.Count)
	}
	if usage.TotalBytes <= 0 {
		t.Errorf("expected positive TotalBytes, got %d", usage.TotalBytes)
	}
}

func TestStoreFS_OnBeforeChat(t *testing.T) {
	tmp := t.TempDir()
	store := NewStoreFS(tmp, nil, nil)

	// Add some memories with correct metadata key
	_, _ = store.Add(context.Background(), AddRequest{
		Memory:   "User prefers Chinese language",
		Metadata: map[string]any{MetadataKeyUserID: "user1"},
	})
	_, _ = store.Add(context.Background(), AddRequest{
		Memory:   "User likes concise responses",
		Metadata: map[string]any{MetadataKeyUserID: "user1"},
	})

	// Query for relevant memories - use a query that is a substring of memory content
	result, err := store.OnBeforeChat(context.Background(), BeforeChatRequest{
		Query:     "Chinese", // Must be substring of memory content
		UserID:    "user1",
		SessionID: "session1",
		Platform:  "test",
	})
	if err != nil {
		t.Fatalf("OnBeforeChat failed: %v", err)
	}

	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if result.ContextText == "" {
		t.Error("expected non-empty context text")
	}
	if !contains(result.ContextText, "memory-context") {
		t.Errorf("expected memory-context in result, got %q", result.ContextText)
	}
	if !contains(result.ContextText, "Chinese") {
		t.Errorf("expected 'Chinese' in context, got %q", result.ContextText)
	}
}

func TestStoreFS_Disabled(t *testing.T) {
	store := NewStoreFS("", nil, nil) // empty dataDir means disabled

	// Operations should be no-ops
	_, err := store.Add(context.Background(), AddRequest{Memory: "test"})
	if err != ErrMemoryDisabled {
		t.Errorf("expected ErrMemoryDisabled, got %v", err)
	}

	err = store.Delete(context.Background(), "id")
	if err != ErrMemoryDisabled {
		t.Errorf("expected ErrMemoryDisabled, got %v", err)
	}
}

func TestStoreFS_FilterByMetadata(t *testing.T) {
	tmp := t.TempDir()
	store := NewStoreFS(tmp, nil, nil)

	// Add memories with different user IDs
	_, _ = store.Add(context.Background(), AddRequest{
		Memory:   "User A preference",
		Metadata: map[string]any{"user_id": "userA"},
	})
	_, _ = store.Add(context.Background(), AddRequest{
		Memory:   "User B preference",
		Metadata: map[string]any{"user_id": "userB"},
	})

	// Filter by userA
	items, err := store.GetAll(context.Background(), map[string]any{"user_id": "userA"})
	if err != nil {
		t.Fatalf("GetAll with filter failed: %v", err)
	}

	if len(items) != 1 {
		t.Errorf("expected 1 item for userA, got %d", len(items))
	}
	if items[0].Metadata["user_id"] != "userA" {
		t.Errorf("expected userA metadata, got %v", items[0].Metadata)
	}
}

func TestParseMemoryDayMD(t *testing.T) {
	content := `# Memory 2026-03-19

## Entry mem_001

` + "```yaml" + `
id: mem_001
hash: abc123
created_at: "2026-03-19T10:00:00Z"
metadata:
  user_id: test123
` + "```" + `

User prefers concise responses

## Entry mem_002

` + "```yaml" + `
id: mem_002
created_at: "2026-03-19T11:00:00Z"
` + "```" + `

User works on Go projects
`

	items, err := parseMemoryDayMD(content)
	if err != nil {
		t.Fatalf("parseMemoryDayMD failed: %v", err)
	}

	if len(items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(items))
	}

	if items[0].ID != "mem_001" {
		t.Errorf("expected ID mem_001, got %q", items[0].ID)
	}
	if items[0].Memory != "User prefers concise responses" {
		t.Errorf("unexpected memory content: %q", items[0].Memory)
	}
	if items[0].Metadata["user_id"] != "test123" {
		t.Errorf("expected metadata user_id, got %v", items[0].Metadata)
	}
}

func TestFormatMemoryDayMD(t *testing.T) {
	items := []MemoryItem{
		{
			ID:        "mem_001",
			Memory:    "Test memory",
			Hash:      "abc123",
			CreatedAt: "2026-03-19T10:00:00Z",
			Metadata:  map[string]any{"user_id": "test"},
		},
	}

	content := formatMemoryDayMD("2026-03-19", items)

	if !contains(content, "# Memory 2026-03-19") {
		t.Error("expected date header in output")
	}
	if !contains(content, "## Entry mem_001") {
		t.Error("expected entry header in output")
	}
	if !contains(content, "Test memory") {
		t.Error("expected memory content in output")
	}

	// Verify round-trip
	parsed, err := parseMemoryDayMD(content)
	if err != nil {
		t.Fatalf("round-trip parse failed: %v", err)
	}
	if len(parsed) != 1 {
		t.Errorf("round-trip expected 1 item, got %d", len(parsed))
	}
	if parsed[0].Memory != "Test memory" {
		t.Errorf("round-trip memory mismatch: %q", parsed[0].Memory)
	}
}

func TestNoopProvider(t *testing.T) {
	p := &NoopProvider{}

	// All operations should be no-ops without errors
	result, err := p.OnBeforeChat(context.Background(), BeforeChatRequest{})
	if err != nil || result != nil {
		t.Errorf("expected nil result and no error, got result=%v, err=%v", result, err)
	}

	err = p.OnAfterChat(context.Background(), AfterChatRequest{})
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}

	resp, err := p.Search(context.Background(), SearchRequest{})
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}
	if len(resp.Results) != 0 {
		t.Errorf("expected empty results, got %d", len(resp.Results))
	}

	item, err := p.Add(context.Background(), AddRequest{})
	if err != nil || item != nil {
		t.Errorf("expected nil item and no error, got item=%v, err=%v", item, err)
	}

	items, err := p.GetAll(context.Background(), nil)
	if err != nil || len(items) != 0 {
		t.Errorf("expected empty slice and no error, got items=%v, err=%v", items, err)
	}
}

func TestTruncateSnippet(t *testing.T) {
	tests := []struct {
		input    string
		max      int
		expected string
	}{
		{"short", 10, "short"},
		{"exactly10!", 10, "exactly10!"},
		{"this is a longer string", 10, "this is a..."},
		{"  trimmed  ", 5, "trimm..."},
	}

	for _, tt := range tests {
		got := TruncateSnippet(tt.input, tt.max)
		if got != tt.expected {
			t.Errorf("TruncateSnippet(%q, %d) = %q, want %q", tt.input, tt.max, got, tt.expected)
		}
	}
}

func TestBuildProfileMetadata(t *testing.T) {
	tests := []struct {
		name                string
		userID              string
		channelIdentityID   string
		displayName         string
		platform            string
		expectedKeys        []string
		expectedRef         string
	}{
		{
			name:         "with user ID",
			userID:       "user123",
			displayName:  "Test User",
			platform:     "telegram",
			expectedKeys: []string{MetadataKeyUserID, MetadataKeyDisplayName, MetadataKeyPlatform, MetadataKeyRef},
			expectedRef:  "user:user123",
		},
		{
			name:              "with channel identity only",
			channelIdentityID: "chan456",
			platform:          "feishu",
			expectedKeys:      []string{MetadataKeyChannelIdentity, MetadataKeyPlatform, MetadataKeyRef},
			expectedRef:       "channel_identity:chan456",
		},
		{
			name:         "both user and channel",
			userID:       "user123",
			channelIdentityID: "chan456",
			expectedKeys: []string{MetadataKeyUserID, MetadataKeyChannelIdentity, MetadataKeyRef},
			expectedRef:  "user:user123",
		},
		{
			name:         "empty",
			expectedKeys: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := BuildProfileMetadata(tt.userID, tt.channelIdentityID, tt.displayName, tt.platform)

			if len(m) != len(tt.expectedKeys) {
				t.Errorf("expected %d keys, got %d", len(tt.expectedKeys), len(m))
			}

			for _, key := range tt.expectedKeys {
				if _, ok := m[key]; !ok {
					t.Errorf("expected key %q in metadata", key)
				}
			}

			if tt.expectedRef != "" && m[MetadataKeyRef] != tt.expectedRef {
				t.Errorf("expected ref %q, got %q", tt.expectedRef, m[MetadataKeyRef])
			}
		})
	}
}

func TestDeduplicateItems(t *testing.T) {
	items := []MemoryItem{
		{ID: "mem_1", Memory: "Memory 1"},
		{ID: "mem_2", Memory: "Memory 2"},
		{ID: "mem_1", Memory: "Memory 1 duplicate"}, // Duplicate ID
		{ID: "", Memory: "Memory without ID"},
		{ID: "", Memory: "Memory without ID"}, // Duplicate content
	}

	result := DeduplicateItems(items)

	// Should have 3 unique items: mem_1, mem_2, and one "Memory without ID"
	if len(result) != 3 {
		t.Errorf("expected 3 unique items, got %d", len(result))
	}
}

func TestMergeMetadata(t *testing.T) {
	tests := []struct {
		name     string
		base     map[string]any
		extra    map[string]any
		expected map[string]any
	}{
		{
			name:     "both nil",
			base:     nil,
			extra:    nil,
			expected: nil,
		},
		{
			name:     "base only",
			base:     map[string]any{"a": 1},
			extra:    nil,
			expected: map[string]any{"a": 1},
		},
		{
			name:     "extra only",
			base:     nil,
			extra:    map[string]any{"b": 2},
			expected: map[string]any{"b": 2},
		},
		{
			name:     "both with override",
			base:     map[string]any{"a": 1, "c": 3},
			extra:    map[string]any{"b": 2, "c": 4},
			expected: map[string]any{"a": 1, "b": 2, "c": 4},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := MergeMetadata(tt.base, tt.extra)

			if tt.expected == nil {
				if result != nil {
					t.Errorf("expected nil, got %v", result)
				}
				return
			}

			if len(result) != len(tt.expected) {
				t.Errorf("expected %d keys, got %d", len(tt.expected), len(result))
			}

			for k, v := range tt.expected {
				if result[k] != v {
					t.Errorf("expected %s=%v, got %v", k, v, result[k])
				}
			}
		})
	}
}

// Helper function
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsHelper(s, substr))
}

func containsHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func TestEnsureDefaultFiles(t *testing.T) {
	tmp := t.TempDir()

	// First call should create files
	created, err := EnsureDefaultFiles(tmp)
	if err != nil {
		t.Fatalf("EnsureDefaultFiles failed: %v", err)
	}
	if !created {
		t.Error("expected created=true on first call")
	}

	// Check MEMORY.md exists and has correct content
	content, err := os.ReadFile(tmp + "/" + MemoryOverview)
	if err != nil {
		t.Fatalf("read MEMORY.md failed: %v", err)
	}
	if !contains(string(content), "This is your core memory") {
		t.Error("MEMORY.md should contain intro text")
	}

	// Check memory/ directory exists
	memDir := tmp + "/" + MemoryDirName
	if stat, err := os.Stat(memDir); err != nil || !stat.IsDir() {
		t.Error("memory/ directory should exist")
	}

	// Second call should not create files
	created2, err := EnsureDefaultFiles(tmp)
	if err != nil {
		t.Fatalf("second EnsureDefaultFiles failed: %v", err)
	}
	if created2 {
		t.Error("expected created=false on second call")
	}
}
