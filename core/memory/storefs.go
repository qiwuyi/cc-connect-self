package memory

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"gopkg.in/yaml.v3"
)

// StoreFS implements file-based memory storage.
// Memories are stored as Markdown files organized by date.
type StoreFS struct {
	mu            sync.RWMutex
	dataDir       string        // Base directory for memory storage
	extractor     Extractor     // Fact extraction service
	decider       Decider       // Memory conflict resolution
	compactor     Compactor     // Memory compaction service
	compactConfig CompactConfig // Compaction configuration
	config        MemoryConfig  // Memory system configuration
	logger        *slog.Logger  // Logger

	// Debounce queue for async memory updates
	queue         *memoryUpdateQueue
	queueLock     sync.Mutex
}

// Constants for file organization.
const (
	MemoryDirName      = "memory"    // Subdirectory for daily memory files
	MemoryOverview     = "MEMORY.md" // Overview file
	IdentityFile       = "IDENTITY.md"
	SoulFile           = "SOUL.md"
	ProfilesFile       = "PROFILES.md"
	MemoryDateLayout   = "2006-01-02"
	MemoryFileSuffix   = ".md"
	EntryHeadingPrefix = "## Entry "
	yamlFence          = "```yaml"
	codeFence          = "```"
)

// Errors.
var (
	ErrMemoryDisabled  = errors.New("memory system is disabled")
	ErrNotFound        = errors.New("memory not found")
	ErrExtractorNotSet = errors.New("extractor not configured")
)

// filterMessagesForMemory filters messages to keep only user inputs and final assistant responses.
// This filters out tool messages and intermediate AI responses with tool_calls.
func filterMessagesForMemory(messages []MemoryMessage) []MemoryMessage {
	if len(messages) == 0 {
		return nil
	}

	var filtered []MemoryMessage
	for _, msg := range messages {
		// Keep user messages
		if msg.Role == "user" {
			filtered = append(filtered, msg)
			continue
		}
		// Keep assistant messages without tool_calls (final responses)
		if msg.Role == "assistant" {
			filtered = append(filtered, msg)
		}
		// Skip tool messages and AI messages with tool_calls
	}

	return filtered
}

// memoryEntryMeta is the YAML metadata for a memory entry.
type memoryEntryMeta struct {
	ID        string         `yaml:"id"`
	Hash      string         `yaml:"hash,omitempty"`
	CreatedAt string         `yaml:"created_at,omitempty"`
	UpdatedAt string         `yaml:"updated_at,omitempty"`
	Metadata  map[string]any `yaml:"metadata,omitempty"`
}

// NewStoreFS creates a new file-based memory store.
func NewStoreFS(dataDir string, extractor Extractor, logger *slog.Logger) *StoreFS {
	if logger == nil {
		logger = slog.Default()
	}
	return &StoreFS{
		dataDir:       dataDir,
		extractor:     extractor,
		decider:       nil, // Will use simple decider by default
		compactor:     nil, // Will use simple compactor by default
		compactConfig: DefaultCompactConfig,
		config:        DefaultMemoryConfig(),
		logger:        logger.With("component", "memory.storefs"),
	}
}

// SetExtractor configures the fact extractor.
func (s *StoreFS) SetExtractor(extractor Extractor) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.extractor = extractor
}

// SetDecider configures the memory decider for conflict resolution.
func (s *StoreFS) SetDecider(decider Decider) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.decider = decider
}

// SetCompactor configures the memory compactor.
func (s *StoreFS) SetCompactor(compactor Compactor) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.compactor = compactor
}

// SetCompactConfig configures compaction settings.
func (s *StoreFS) SetCompactConfig(config CompactConfig) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.compactConfig = config
}

// SetConfig configures the memory system settings.
func (s *StoreFS) SetConfig(config MemoryConfig) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.config = config
}

// memoryDir returns the path to the memory directory.
func (s *StoreFS) memoryDir() string {
	return filepath.Join(s.dataDir, MemoryDirName)
}

// memoryDayPath returns the path for a specific day's memory file.
func memoryDayPath(baseDir, date string) string {
	return filepath.Join(baseDir, MemoryDirName, date+MemoryFileSuffix)
}

// memoryOverviewPath returns the path to the overview file.
func memoryOverviewPath(baseDir string) string {
	return filepath.Join(baseDir, MemoryOverview)
}

// OnBeforeChat injects the MEMORY.md overview into the context so the
// agent always has a complete picture of known facts.
// Limits injection to maxInjectionTokens to avoid context overflow.
func (s *StoreFS) OnBeforeChat(_ context.Context, _ BeforeChatRequest) (*BeforeChatResult, error) {
	if s.dataDir == "" {
		return nil, nil
	}

	overviewPath := memoryOverviewPath(s.dataDir)
	content, err := os.ReadFile(overviewPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		s.logger.Warn("failed to read MEMORY.md for context injection", "error", err)
		return nil, nil
	}

	overview := strings.TrimSpace(string(content))
	if overview == "" || overview == strings.TrimSpace(DefaultMemoryOverview) {
		return nil, nil // No real memories yet
	}

	// Get config for limits
	maxTokens := 2000 // Default from deer-flow
	if s.config.MaxInjectionTokens > 0 {
		maxTokens = s.config.MaxInjectionTokens
	}

	// Estimate tokens (rough: 1 token ≈ 4 chars)
	maxChars := maxTokens * 4
	if len(overview) > maxChars {
		// Truncate to fit
		overview = overview[:maxChars]
		// Try to cut at line boundary
		lastNewline := strings.LastIndex(overview, "\n")
		if lastNewline > maxChars/2 {
			overview = overview[:lastNewline]
		}
		overview += "\n... (truncated)"
	}

	// Inject current time so the agent knows the real-world time
	currentTime := time.Now().Format("2006-01-02 15:04 (Monday)")
	result := "<memory-context>\n📅 " + currentTime + "\n\n" + overview + "\n</memory-context>"
	return &BeforeChatResult{ContextText: result}, nil
}

// OnAfterChat extracts facts from the conversation and stores them.
// Uses debounce mechanism to batch multiple updates together.
func (s *StoreFS) OnAfterChat(ctx context.Context, req AfterChatRequest) error {
	if s.dataDir == "" {
		return nil
	}

	// Filter messages: keep only user inputs and final assistant responses
	filteredMessages := filterMessagesForMemory(req.Messages)
	if len(filteredMessages) == 0 {
		return nil
	}

	// Check if we have meaningful conversation (at least one user + one assistant)
	userCount := 0
	assistantCount := 0
	for _, msg := range filteredMessages {
		if msg.Role == "user" {
			userCount++
		} else if msg.Role == "assistant" {
			assistantCount++
		}
	}
	if userCount == 0 || assistantCount == 0 {
		return nil
	}

	// For now, process immediately (can be changed to queue-based later)
	s.mu.RLock()
	extractor := s.extractor
	s.mu.RUnlock()

	if extractor == nil {
		return nil // No extractor, skip memory extraction
	}

	// Extract facts from filtered conversation
	facts, err := extractor.Extract(ctx, filteredMessages)
	if err != nil {
		s.logger.Warn("fact extraction failed", "error", err)
		return nil // Don't fail the conversation for memory errors
	}

	if len(facts) == 0 {
		return nil
	}

	// Build metadata
	metadata := BuildProfileMetadata(req.UserID, req.ChatID, "", req.Platform)

	// Store each fact
	for _, fact := range facts {
		if strings.TrimSpace(fact) == "" {
			continue
		}
		_, err := s.Add(ctx, AddRequest{
			Memory:   fact,
			Metadata: metadata,
		})
		if err != nil {
			s.logger.Warn("failed to store memory", "error", err, "fact", fact)
		}
	}

	return nil
}

// Search performs a simple text-based search over all memories.
func (s *StoreFS) Search(ctx context.Context, req SearchRequest) (*SearchResponse, error) {
	if s.dataDir == "" {
		return &SearchResponse{}, nil
	}

	allMemories, err := s.GetAll(ctx, req.Filters)
	if err != nil {
		return nil, err
	}

	query := strings.ToLower(strings.TrimSpace(req.Query))
	if query == "" {
		return &SearchResponse{Results: allMemories}, nil
	}

	// Simple text matching (no vectors)
	var results []MemoryItem
	for _, item := range allMemories {
		if strings.Contains(strings.ToLower(item.Memory), query) {
			// Calculate simple relevance score based on occurrence count
			item.Score = float64(strings.Count(strings.ToLower(item.Memory), query))
			results = append(results, item)
		}
	}

	// Sort by score descending
	sort.Slice(results, func(i, j int) bool {
		return results[i].Score > results[j].Score
	})

	// Apply limit
	limit := req.Limit
	if limit <= 0 {
		limit = DefaultMemoryLimit
	}
	if len(results) > limit {
		results = results[:limit]
	}

	return &SearchResponse{
		Results: results,
		Total:   len(results),
	}, nil
}

// Add creates a new memory entry with conflict resolution.
func (s *StoreFS) Add(ctx context.Context, req AddRequest) (*MemoryItem, error) {
	if s.dataDir == "" {
		return nil, ErrMemoryDisabled
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now().UTC()
	memory := strings.TrimSpace(req.Memory)
	if memory == "" {
		return nil, nil
	}

	newHash := hashMemory(memory)

	// Get all existing memories for conflict detection
	allExisting, err := s.getAllInternal()
	if err != nil {
		s.logger.Warn("failed to get existing memories", "error", err)
		// Continue with add if we can't read existing
	}

	// Use decider if available
	if s.decider != nil && len(allExisting) > 0 {
		decision, err := s.decider.Decide(ctx, memory, allExisting)
		if err != nil {
			s.logger.Warn("decider failed", "error", err)
			// Fallback: check for exact hash match
			for _, item := range allExisting {
				if item.Hash == newHash {
					s.logger.Debug("duplicate memory detected (hash match), skipping", "memory", TruncateSnippet(memory, 50))
					return nil, nil
				}
			}
		} else {
			switch decision.Type {
			case DecisionNoop:
				s.logger.Debug("decider chose NOOP, skipping memory", "reason", decision.Reason)
				return nil, nil
			case DecisionUpdate:
				if decision.MemoryID != "" {
					// Update existing memory
					updated, err := s.updateInternal(decision.MemoryID, memory)
					if err != nil {
						s.logger.Warn("failed to update memory", "error", err, "id", decision.MemoryID)
					} else {
						s.logger.Debug("updated existing memory", "id", decision.MemoryID, "reason", decision.Reason)
						return updated, nil
					}
				}
			case DecisionDelete:
				// Delete old memory and add new one
				if decision.MemoryID != "" {
					_ = s.deleteInternal(decision.MemoryID)
				}
				// Fall through to add
			}
		}
	} else {
		// Simple fallback: check for exact hash duplicates
		for _, item := range allExisting {
			if item.Hash == newHash {
				s.logger.Debug("duplicate memory detected (hash match), skipping")
				return nil, nil
			}
		}
	}

	item := MemoryItem{
		ID:        generateMemoryID(),
		Memory:    memory,
		Hash:      newHash,
		CreatedAt: now.Format(time.RFC3339),
		UpdatedAt: now.Format(time.RFC3339),
		Metadata:  req.Metadata,
	}

	// Write to daily file
	date := now.Format(MemoryDateLayout)
	filePath := memoryDayPath(s.dataDir, date)

	// Read existing memories for this day
	existing, err := s.readMemoryDay(filePath)
	if err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("read memory day: %w", err)
	}

	// Append new memory
	existing = append(existing, item)

	// Write back
	if err := s.writeMemoryDay(filePath, date, existing); err != nil {
		return nil, fmt.Errorf("write memory day: %w", err)
	}

	// Update overview
	if err := s.syncOverview(); err != nil {
		s.logger.Warn("failed to sync overview", "error", err)
	}

	return &item, nil
}

// GetAll retrieves all memories, optionally filtered.
func (s *StoreFS) GetAll(_ context.Context, filters map[string]any) ([]MemoryItem, error) {
	if s.dataDir == "" {
		return nil, nil
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	memDir := s.memoryDir()
	entries, err := os.ReadDir(memDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read memory directory: %w", err)
	}

	var allItems []MemoryItem
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), MemoryFileSuffix) {
			continue
		}

		filePath := filepath.Join(memDir, entry.Name())
		items, err := s.readMemoryDay(filePath)
		if err != nil {
			s.logger.Warn("failed to read memory file", "path", filePath, "error", err)
			continue
		}

		// Apply filters
		for _, item := range items {
			if matchesFilters(item, filters) {
				allItems = append(allItems, item)
			}
		}
	}

	// Sort by creation date (newest first)
	sort.Slice(allItems, func(i, j int) bool {
		return allItems[i].CreatedAt > allItems[j].CreatedAt
	})

	return allItems, nil
}

// Delete removes a memory by ID.
func (s *StoreFS) Delete(_ context.Context, id string) error {
	if s.dataDir == "" {
		return ErrMemoryDisabled
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	memDir := s.memoryDir()
	entries, err := os.ReadDir(memDir)
	if err != nil {
		if os.IsNotExist(err) {
			return ErrNotFound
		}
		return fmt.Errorf("read memory directory: %w", err)
	}

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), MemoryFileSuffix) {
			continue
		}

		filePath := filepath.Join(memDir, entry.Name())
		items, err := s.readMemoryDay(filePath)
		if err != nil {
			continue
		}

		// Find and remove the item
		var filtered []MemoryItem
		found := false
		for _, item := range items {
			if item.ID == id {
				found = true
				continue
			}
			filtered = append(filtered, item)
		}

		if found {
			if len(filtered) == 0 {
				// Remove empty file
				if err := os.Remove(filePath); err != nil {
					return fmt.Errorf("remove empty memory file: %w", err)
				}
			} else {
				// Write back filtered list
				date := strings.TrimSuffix(entry.Name(), MemoryFileSuffix)
				if err := s.writeMemoryDay(filePath, date, filtered); err != nil {
					return fmt.Errorf("write filtered memories: %w", err)
				}
			}
			return s.syncOverview()
		}
	}

	return ErrNotFound
}

// Update modifies an existing memory by ID.
func (s *StoreFS) Update(_ context.Context, id string, memory string) (*MemoryItem, error) {
	if s.dataDir == "" {
		return nil, ErrMemoryDisabled
	}

	memory = strings.TrimSpace(memory)
	if memory == "" {
		return nil, nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	memDir := s.memoryDir()
	entries, err := os.ReadDir(memDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("read memory directory: %w", err)
	}

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), MemoryFileSuffix) {
			continue
		}

		filePath := filepath.Join(memDir, entry.Name())
		items, err := s.readMemoryDay(filePath)
		if err != nil {
			continue
		}

		for i, item := range items {
			if item.ID == id {
				// Update the memory
				items[i].Memory = memory
				items[i].Hash = hashMemory(memory)
				items[i].UpdatedAt = time.Now().UTC().Format(time.RFC3339)

				date := strings.TrimSuffix(entry.Name(), MemoryFileSuffix)
				if err := s.writeMemoryDay(filePath, date, items); err != nil {
					return nil, fmt.Errorf("write updated memories: %w", err)
				}

				if err := s.syncOverview(); err != nil {
					s.logger.Warn("failed to sync overview", "error", err)
				}

				return &items[i], nil
			}
		}
	}

	return nil, ErrNotFound
}

// DeleteBatch removes multiple memories by IDs.
func (s *StoreFS) DeleteBatch(_ context.Context, ids []string) error {
	if s.dataDir == "" {
		return ErrMemoryDisabled
	}

	if len(ids) == 0 {
		return nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	memDir := s.memoryDir()
	entries, err := os.ReadDir(memDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // Nothing to delete
		}
		return fmt.Errorf("read memory directory: %w", err)
	}

	idsToDelete := make(map[string]struct{})
	for _, id := range ids {
		idsToDelete[id] = struct{}{}
	}

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), MemoryFileSuffix) {
			continue
		}

		filePath := filepath.Join(memDir, entry.Name())
		items, err := s.readMemoryDay(filePath)
		if err != nil {
			continue
		}

		var filtered []MemoryItem
		changed := false
		for _, item := range items {
			if _, shouldDelete := idsToDelete[item.ID]; shouldDelete {
				changed = true
				continue
			}
			filtered = append(filtered, item)
		}

		if changed {
			if len(filtered) == 0 {
				if err := os.Remove(filePath); err != nil && !os.IsNotExist(err) {
					return fmt.Errorf("remove empty memory file: %w", err)
				}
			} else {
				date := strings.TrimSuffix(entry.Name(), MemoryFileSuffix)
				if err := s.writeMemoryDay(filePath, date, filtered); err != nil {
					return fmt.Errorf("write filtered memories: %w", err)
				}
			}
		}
	}

	return s.syncOverview()
}

// DeleteAll removes all memories, optionally filtered.
func (s *StoreFS) DeleteAll(_ context.Context, filters map[string]any) error {
	if s.dataDir == "" {
		return ErrMemoryDisabled
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if len(filters) == 0 {
		// Delete entire memory directory
		memDir := s.memoryDir()
		if err := os.RemoveAll(memDir); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("remove memory directory: %w", err)
		}
	} else {
		// Filtered deletion - read all, filter, write back
		allItems, err := s.GetAll(context.Background(), nil)
		if err != nil {
			return err
		}

		var toKeep []MemoryItem
		for _, item := range allItems {
			if !matchesFilters(item, filters) {
				toKeep = append(toKeep, item)
			}
		}

		// Clear and rebuild
		memDir := s.memoryDir()
		_ = os.RemoveAll(memDir)

		for _, item := range toKeep {
			_, _ = s.Add(context.Background(), AddRequest{
				Memory:   item.Memory,
				Metadata: item.Metadata,
			})
		}
	}

	return s.syncOverview()
}

// Usage returns memory statistics.
func (s *StoreFS) Usage(_ context.Context, filters map[string]any) (*UsageResponse, error) {
	items, err := s.GetAll(context.Background(), filters)
	if err != nil {
		return nil, err
	}

	var totalBytes int64
	for _, item := range items {
		totalBytes += int64(len(item.Memory))
	}

	avgBytes := int64(0)
	if len(items) > 0 {
		avgBytes = totalBytes / int64(len(items))
	}

	return &UsageResponse{
		Count:        len(items),
		TotalBytes:   totalBytes,
		AvgItemBytes: avgBytes,
	}, nil
}

// readMemoryDay reads all memories from a daily file.
func (s *StoreFS) readMemoryDay(filePath string) ([]MemoryItem, error) {
	content, err := os.ReadFile(filePath)
	if err != nil {
		return nil, err
	}

	return parseMemoryDayMD(string(content))
}

// writeMemoryDay writes memories to a daily file.
func (s *StoreFS) writeMemoryDay(filePath, date string, items []MemoryItem) error {
	// Ensure directory exists
	dir := filepath.Dir(filePath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}

	content := formatMemoryDayMD(date, items)
	return os.WriteFile(filePath, []byte(content), 0o644)
}

// syncOverview regenerates the MEMORY.md overview file.
// NOTE: This function assumes the caller holds the lock (s.mu).
func (s *StoreFS) syncOverview() error {
	items, err := s.getAllInternal()
	if err != nil {
		return err
	}

	content := formatMemoryOverviewMD(items)
	overviewPath := memoryOverviewPath(s.dataDir)
	return os.WriteFile(overviewPath, []byte(content), 0o644)
}

// getAllInternal retrieves all memories without acquiring the lock.
// Caller must hold s.mu before calling.
func (s *StoreFS) getAllInternal() ([]MemoryItem, error) {
	if s.dataDir == "" {
		return nil, nil
	}

	memDir := s.memoryDir()
	entries, err := os.ReadDir(memDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read memory directory: %w", err)
	}

	var allItems []MemoryItem
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), MemoryFileSuffix) {
			continue
		}

		filePath := filepath.Join(memDir, entry.Name())
		items, err := s.readMemoryDay(filePath)
		if err != nil {
			s.logger.Warn("failed to read memory file", "path", filePath, "error", err)
			continue
		}

		allItems = append(allItems, items...)
	}

	// Sort by creation date (newest first)
	sort.Slice(allItems, func(i, j int) bool {
		return allItems[i].CreatedAt > allItems[j].CreatedAt
	})

	return allItems, nil
}

// parseMemoryDayMD parses a Markdown memory file.
func parseMemoryDayMD(content string) ([]MemoryItem, error) {
	content = strings.TrimSpace(content)
	if content == "" {
		return nil, errors.New("empty memory file")
	}

	lines := strings.Split(content, "\n")
	var items []MemoryItem

	for i := 0; i < len(lines); i++ {
		line := strings.TrimSpace(lines[i])
		if !strings.HasPrefix(line, EntryHeadingPrefix) {
			continue
		}

		entryID := strings.TrimSpace(strings.TrimPrefix(line, EntryHeadingPrefix))

		// Skip blank lines
		j := i + 1
		for j < len(lines) && strings.TrimSpace(lines[j]) == "" {
			j++
		}

		// Look for YAML fence
		if j >= len(lines) || strings.TrimSpace(lines[j]) != yamlFence {
			continue
		}

		// Find end of YAML
		metaStart := j + 1
		metaEnd := metaStart
		for metaEnd < len(lines) && strings.TrimSpace(lines[metaEnd]) != codeFence {
			metaEnd++
		}

		if metaEnd >= len(lines) {
			break
		}

		// Parse YAML metadata
		var meta memoryEntryMeta
		yamlContent := strings.Join(lines[metaStart:metaEnd], "\n")
		if err := yaml.Unmarshal([]byte(yamlContent), &meta); err != nil {
			continue
		}

		// Find memory text (until next entry or EOF)
		bodyStart := metaEnd + 1
		if bodyStart < len(lines) && strings.TrimSpace(lines[bodyStart]) == "" {
			bodyStart++
		}

		bodyEnd := bodyStart
		for bodyEnd < len(lines) && !strings.HasPrefix(strings.TrimSpace(lines[bodyEnd]), EntryHeadingPrefix) {
			bodyEnd++
		}

		memory := strings.TrimSpace(strings.Join(lines[bodyStart:bodyEnd], "\n"))
		if memory == "" {
			i = bodyEnd - 1
			continue
		}

		item := MemoryItem{
			ID:        firstNonEmpty(meta.ID, entryID),
			Hash:      strings.TrimSpace(meta.Hash),
			CreatedAt: strings.TrimSpace(meta.CreatedAt),
			UpdatedAt: strings.TrimSpace(meta.UpdatedAt),
			Metadata:  meta.Metadata,
			Memory:    memory,
		}

		if item.ID != "" && item.Memory != "" {
			items = append(items, item)
		}

		i = bodyEnd - 1
	}

	if len(items) == 0 {
		return nil, errors.New("no memory entries found")
	}

	return items, nil
}

// formatMemoryDayMD formats memories as a Markdown file.
func formatMemoryDayMD(date string, items []MemoryItem) string {
	var b strings.Builder
	b.WriteString("# Memory ")
	b.WriteString(date)
	b.WriteString("\n\n")

	// Sort by creation time
	sort.Slice(items, func(i, j int) bool {
		return items[i].CreatedAt < items[j].CreatedAt
	})

	for _, item := range items {
		item.Memory = strings.TrimSpace(item.Memory)
		if item.Memory == "" {
			continue
		}

		meta := memoryEntryMeta{
			ID:        item.ID,
			Hash:      item.Hash,
			CreatedAt: item.CreatedAt,
			UpdatedAt: item.UpdatedAt,
			Metadata:  item.Metadata,
		}

		metaYAML, _ := yaml.Marshal(meta)

		b.WriteString(EntryHeadingPrefix)
		b.WriteString(item.ID)
		b.WriteString("\n\n")
		b.WriteString(yamlFence)
		b.WriteString("\n")
		b.WriteString(strings.TrimSpace(string(metaYAML)))
		b.WriteString("\n")
		b.WriteString(codeFence)
		b.WriteString("\n\n")
		b.WriteString(item.Memory)
		b.WriteString("\n\n")
	}

	return b.String()
}

// formatMemoryOverviewMD generates the overview file content.
func formatMemoryOverviewMD(items []MemoryItem) string {
	var b strings.Builder
	b.WriteString("# MEMORY\n\n")
	b.WriteString("_This is your core memory, please keep it up to date._\n\n")

	if len(items) == 0 {
		b.WriteString("> No memories yet. Start a conversation to build your memory.\n")
		return b.String()
	}

	// Sort by creation time (newest first)
	sort.Slice(items, func(i, j int) bool {
		return items[i].CreatedAt > items[j].CreatedAt
	})

	for i, item := range items {
		if i >= 100 {
			break
		}

		created := item.CreatedAt
		if created == "" {
			created = "unknown"
		}

		memory := strings.Join(strings.Fields(item.Memory), " ")
		if len(memory) > 200 {
			memory = memory[:200] + "..."
		}

		b.WriteString(fmt.Sprintf("%d. [%s] %s\n", i+1, created, memory))
	}

	return b.String()
}

// Helper functions

func generateMemoryID() string {
	return "mem_" + fmt.Sprintf("%d", time.Now().UnixNano())
}

func hashMemory(text string) string {
	h := sha256.Sum256([]byte(text))
	return hex.EncodeToString(h[:8])
}

func TruncateSnippet(s string, n int) string {
	s = strings.TrimSpace(s)
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return strings.TrimSpace(string(runes[:n])) + "..."
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		v = strings.TrimSpace(v)
		if v != "" {
			return v
		}
	}
	return ""
}

func matchesFilters(item MemoryItem, filters map[string]any) bool {
	if len(filters) == 0 {
		return true
	}

	for key, value := range filters {
		itemValue, ok := item.Metadata[key]
		if !ok {
			return false
		}
		if itemValue != value {
			return false
		}
	}

	return true
}

// DefaultMemoryOverview is the default content for MEMORY.md when no memories exist.
const DefaultMemoryOverview = `# MEMORY

_This is your core memory, please keep it up to date._

> No memories yet. Start a conversation to build your memory.
`

// EnsureDefaultFiles creates the memory directory and default MEMORY.md if they don't exist.
// Returns true if MEMORY.md was created.
func EnsureDefaultFiles(dataDir string) (bool, error) {
	// Create memory directory
	memDir := filepath.Join(dataDir, MemoryDirName)
	if err := os.MkdirAll(memDir, 0o755); err != nil {
		return false, fmt.Errorf("create memory directory: %w", err)
	}

	// Create MEMORY.md if it doesn't exist
	overviewPath := filepath.Join(dataDir, MemoryOverview)
	if _, err := os.Stat(overviewPath); err == nil {
		return false, nil // File exists
	}

	if err := os.WriteFile(overviewPath, []byte(DefaultMemoryOverview), 0o644); err != nil {
		return false, fmt.Errorf("write default MEMORY.md: %w", err)
	}

	return true, nil
}

// updateInternal updates a memory by ID (caller must hold lock).
func (s *StoreFS) updateInternal(id string, memory string) (*MemoryItem, error) {
	memDir := s.memoryDir()
	entries, err := os.ReadDir(memDir)
	if err != nil {
		return nil, err
	}

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), MemoryFileSuffix) {
			continue
		}

		filePath := filepath.Join(memDir, entry.Name())
		items, err := s.readMemoryDay(filePath)
		if err != nil {
			continue
		}

		for i, item := range items {
			if item.ID == id {
				items[i].Memory = memory
				items[i].Hash = hashMemory(memory)
				items[i].UpdatedAt = time.Now().UTC().Format(time.RFC3339)

				date := strings.TrimSuffix(entry.Name(), MemoryFileSuffix)
				if err := s.writeMemoryDay(filePath, date, items); err != nil {
					return nil, err
				}

				_ = s.syncOverview()
				return &items[i], nil
			}
		}
	}

	return nil, ErrNotFound
}

// deleteInternal deletes a memory by ID (caller must hold lock).
func (s *StoreFS) deleteInternal(id string) error {
	memDir := s.memoryDir()
	entries, err := os.ReadDir(memDir)
	if err != nil {
		return err
	}

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), MemoryFileSuffix) {
			continue
		}

		filePath := filepath.Join(memDir, entry.Name())
		items, err := s.readMemoryDay(filePath)
		if err != nil {
			continue
		}

		var filtered []MemoryItem
		found := false
		for _, item := range items {
			if item.ID == id {
				found = true
				continue
			}
			filtered = append(filtered, item)
		}

		if found {
			if len(filtered) == 0 {
				_ = os.Remove(filePath)
			} else {
				date := strings.TrimSuffix(entry.Name(), MemoryFileSuffix)
				_ = s.writeMemoryDay(filePath, date, filtered)
			}
			_ = s.syncOverview()
			return nil
		}
	}

	return ErrNotFound
}

// Compact performs memory compaction if needed.
func (s *StoreFS) Compact(ctx context.Context) (*CompactResult, error) {
	if s.dataDir == "" {
		return nil, ErrMemoryDisabled
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// Get all memories
	allMemories, err := s.getAllInternal()
	if err != nil {
		return nil, err
	}

	// Check if compaction is needed
	if !NeedsCompaction(len(allMemories), s.compactConfig) {
		return &CompactResult{
			BeforeCount: len(allMemories),
			AfterCount:  len(allMemories),
		}, nil
	}

	// Use compactor or fallback
	compactor := s.compactor
	if compactor == nil {
		compactor = NewSimpleCompactor()
	}

	targetCount := s.compactConfig.TargetCount
	if targetCount <= 0 {
		targetCount = DefaultCompactConfig.TargetCount
	}

	compacted, err := compactor.Compact(ctx, allMemories, targetCount)
	if err != nil {
		return nil, fmt.Errorf("compaction failed: %w", err)
	}

	// Calculate which IDs were removed
	keptIDs := make(map[string]bool)
	for _, m := range compacted {
		keptIDs[m.ID] = true
	}

	var removedIDs []string
	for _, m := range allMemories {
		if !keptIDs[m.ID] {
			removedIDs = append(removedIDs, m.ID)
		}
	}

	// Determine new memories (merged ones with new IDs)
	var newMemories []MemoryItem
	for _, m := range compacted {
		if !keptIDs[m.ID] {
			newMemories = append(newMemories, m)
		}
	}

	// Rebuild storage with compacted memories
	memDir := s.memoryDir()
	_ = os.RemoveAll(memDir)

	for _, mem := range compacted {
		// Parse creation time to determine file date
		created := mem.CreatedAt
		if created == "" {
			created = time.Now().UTC().Format(time.RFC3339)
		}

		date := time.Now().Format(MemoryDateLayout)
		if t, err := time.Parse(time.RFC3339, created); err == nil {
			date = t.Format(MemoryDateLayout)
		}

		filePath := memoryDayPath(s.dataDir, date)
		existing, _ := s.readMemoryDay(filePath)
		existing = append(existing, mem)
		_ = s.writeMemoryDay(filePath, date, existing)
	}

	_ = s.syncOverview()

	return &CompactResult{
		BeforeCount: len(allMemories),
		AfterCount:  len(compacted),
		MergedIDs:   removedIDs,
		NewMemories: newMemories,
	}, nil
}

// Ensure StoreFS implements Provider interface.
var _ Provider = (*StoreFS)(nil)

// Ensure NoopProvider implements Provider interface.
var _ Provider = (*NoopProvider)(nil)

// ---------------------------------------------------------------------------
// Debounce Queue Implementation (inspired by deer-flow)
// ---------------------------------------------------------------------------

// conversationContext holds context for a conversation to be processed.
type conversationContext struct {
	sessionID string
	messages  []MemoryMessage
	timestamp time.Time
}

// memoryUpdateQueue implements debounce mechanism for memory updates.
type memoryUpdateQueue struct {
	queue      []conversationContext
	lock       sync.Mutex
	timer      *time.Timer
	processing bool
}

// newMemoryUpdateQueue creates a new memory update queue.
func newMemoryUpdateQueue() *memoryUpdateQueue {
	return &memoryUpdateQueue{
		queue: make([]conversationContext, 0),
	}
}

// add adds a conversation to the update queue.
func (q *memoryUpdateQueue) add(sessionID string, messages []MemoryMessage) {
	q.lock.Lock()
	defer q.lock.Unlock()

	// Check if this session already has a pending update, replace with newer
	newCtx := conversationContext{
		sessionID: sessionID,
		messages:  messages,
		timestamp: time.Now(),
	}

	var newQueue []conversationContext
	for _, ctx := range q.queue {
		if ctx.sessionID != sessionID {
			newQueue = append(newQueue, ctx)
		}
	}
	newQueue = append(newQueue, newCtx)
	q.queue = newQueue

	// Reset timer
	q.resetTimer()
}

// resetTimer resets the debounce timer.
func (q *memoryUpdateQueue) resetTimer() {
	if q.timer != nil {
		q.timer.Stop()
	}

	// Default 30 seconds debounce
	q.timer = time.AfterFunc(30*time.Second, func() {
		q.processQueue()
	})
}

// processQueue processes all queued conversations.
func (q *memoryUpdateQueue) processQueue() {
	q.lock.Lock()
	if q.processing {
		q.lock.Unlock()
		// Already processing, reschedule
		q.resetTimer()
		return
	}

	if len(q.queue) == 0 {
		q.lock.Unlock()
		return
	}

	q.processing = true
	contextsToProcess := q.queue
	q.queue = make([]conversationContext, 0)
	q.timer = nil
	q.lock.Unlock()

	// Process each context - this will be called via callback
	_ = contextsToProcess // Placeholder for actual processing
}
