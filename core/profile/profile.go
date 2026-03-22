// Package profile manages user and group profiles (PROFILES.md).
// These profiles enable multi-user identity recognition and memory association.
package profile

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// ProfileType distinguishes between user and group profiles.
type ProfileType string

const (
	TypeUser  ProfileType = "user"
	TypeGroup ProfileType = "group"
)

// Profile represents a user or group profile.
type Profile struct {
	ID          string            `json:"id"`           // Unique identifier (e.g., "user_001", "group_dev")
	Type        ProfileType       `json:"type"`         // "user" or "group"
	DisplayName string            `json:"display_name"` // Human-readable name
	PlatformIDs map[string]string `json:"platform_ids"` // Platform-specific IDs (e.g., "feishu": "ou_xxx", "telegram": "@user")
	Attributes  map[string]string `json:"attributes"`   // Additional attributes (email, phone, etc.)
}

// Manager handles loading, saving, and querying user profiles.
type Manager struct {
	mu       sync.RWMutex
	dataDir  string
	profiles map[string]*Profile // ID -> Profile
	filePath string              // Path to PROFILES.md
}

// NewManager creates a new profile manager.
func NewManager(dataDir string) *Manager {
	return &Manager{
		dataDir:  dataDir,
		profiles: make(map[string]*Profile),
		filePath: filepath.Join(dataDir, "PROFILES.md"),
	}
}

// Load reads and parses the PROFILES.md file.
func (m *Manager) Load() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	content, err := os.ReadFile(m.filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // No profiles file yet
		}
		return fmt.Errorf("read PROFILES.md: %w", err)
	}

	profiles, err := parseProfilesMD(string(content))
	if err != nil {
		return fmt.Errorf("parse PROFILES.md: %w", err)
	}

	m.profiles = profiles
	return nil
}

// Get retrieves a profile by ID.
func (m *Manager) Get(id string) *Profile {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.profiles[id]
}

// GetByPlatformID finds a profile by platform and platform-specific ID.
// Returns nil if not found.
func (m *Manager) GetByPlatformID(platform, platformID string) *Profile {
	m.mu.RLock()
	defer m.mu.RUnlock()

	for _, p := range m.profiles {
		if p.PlatformIDs[platform] == platformID {
			return p
		}
	}
	return nil
}

// GetAll returns all profiles.
func (m *Manager) GetAll() []*Profile {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make([]*Profile, 0, len(m.profiles))
	for _, p := range m.profiles {
		result = append(result, p)
	}
	return result
}

// GetByType returns profiles filtered by type.
func (m *Manager) GetByType(typ ProfileType) []*Profile {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var result []*Profile
	for _, p := range m.profiles {
		if p.Type == typ {
			result = append(result, p)
		}
	}
	return result
}

// Add creates a new profile and saves to file.
func (m *Manager) Add(p *Profile) error {
	if p.ID == "" {
		return fmt.Errorf("profile ID is required")
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	m.profiles[p.ID] = p
	return m.saveUnlocked()
}

// Update modifies an existing profile.
func (m *Manager) Update(p *Profile) error {
	if p.ID == "" {
		return fmt.Errorf("profile ID is required")
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.profiles[p.ID]; !exists {
		return fmt.Errorf("profile not found: %s", p.ID)
	}

	m.profiles[p.ID] = p
	return m.saveUnlocked()
}

// Delete removes a profile by ID.
func (m *Manager) Delete(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.profiles[id]; !exists {
		return fmt.Errorf("profile not found: %s", id)
	}

	delete(m.profiles, id)
	return m.saveUnlocked()
}

// Save writes all profiles to PROFILES.md.
func (m *Manager) Save() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.saveUnlocked()
}

// saveUnlocked writes profiles to file. Caller must hold the lock.
func (m *Manager) saveUnlocked() error {
	content := formatProfilesMD(m.profiles)

	// Ensure directory exists
	if err := os.MkdirAll(m.dataDir, 0o755); err != nil {
		return fmt.Errorf("create data directory: %w", err)
	}

	if err := os.WriteFile(m.filePath, []byte(content), 0o644); err != nil {
		return fmt.Errorf("write PROFILES.md: %w", err)
	}

	return nil
}

// parseProfilesMD parses the PROFILES.md file content.
// Format:
//
//	_This is profiles from different users or groups._
//
//	## 张三
//	- ID: user_001
//	- Type: user
//	- Email: zhangsan@example.com
//	- Telegram: @zhangsan
//	- Feishu: ou_xxx
//
//	## 开发组
//	- ID: group_dev
//	- Type: group
//	- Members: 张三, 李四, 王五
func parseProfilesMD(content string) (map[string]*Profile, error) {
	content = strings.TrimSpace(content)
	if content == "" {
		return nil, nil
	}

	profiles := make(map[string]*Profile)
	lines := strings.Split(content, "\n")

	var currentProfile *Profile

	for _, line := range lines {
		line = strings.TrimSpace(line)

		// Skip empty lines and intro text
		if line == "" || strings.HasPrefix(line, "_This is profiles") {
			continue
		}

		// New profile section
		if strings.HasPrefix(line, "## ") {
			// Save previous profile
			if currentProfile != nil && currentProfile.ID != "" {
				profiles[currentProfile.ID] = currentProfile
			}

			displayName := strings.TrimSpace(strings.TrimPrefix(line, "## "))
			currentProfile = &Profile{
				DisplayName: displayName,
				PlatformIDs: make(map[string]string),
				Attributes:  make(map[string]string),
				Type:        TypeUser, // Default to user
			}
			continue
		}

		// Parse attributes
		if currentProfile == nil {
			continue
		}

		if !strings.HasPrefix(line, "- ") {
			continue
		}

		attrLine := strings.TrimPrefix(line, "- ")
		parts := strings.SplitN(attrLine, ":", 2)
		if len(parts) != 2 {
			continue
		}

		key := strings.TrimSpace(parts[0])
		value := strings.TrimSpace(parts[1])

		switch strings.ToLower(key) {
		case "id":
			currentProfile.ID = value
		case "type":
			if strings.ToLower(value) == "group" {
				currentProfile.Type = TypeGroup
			} else {
				currentProfile.Type = TypeUser
			}
		case "name":
			if currentProfile.DisplayName == "" {
				currentProfile.DisplayName = value
			}
		// Platform IDs
		case "feishu", "telegram", "discord", "slack", "dingtalk", "wecom", "qq", "line":
			currentProfile.PlatformIDs[strings.ToLower(key)] = value
		default:
			currentProfile.Attributes[key] = value
		}
	}

	// Save last profile
	if currentProfile != nil && currentProfile.ID != "" {
		profiles[currentProfile.ID] = currentProfile
	}

	return profiles, nil
}

// formatProfilesMD formats profiles as Markdown.
func formatProfilesMD(profiles map[string]*Profile) string {
	var sb strings.Builder
	sb.WriteString("_This is profiles from different users or groups._\n\n")

	for _, p := range profiles {
		sb.WriteString("## ")
		sb.WriteString(p.DisplayName)
		sb.WriteString("\n")
		sb.WriteString("- ID: ")
		sb.WriteString(p.ID)
		sb.WriteString("\n")
		sb.WriteString("- Type: ")
		sb.WriteString(string(p.Type))
		sb.WriteString("\n")

		// Platform IDs
		for platform, id := range p.PlatformIDs {
			sb.WriteString("- ")
			sb.WriteString(platform)
			sb.WriteString(": ")
			sb.WriteString(id)
			sb.WriteString("\n")
		}

		// Other attributes
		for key, value := range p.Attributes {
			sb.WriteString("- ")
			sb.WriteString(key)
			sb.WriteString(": ")
			sb.WriteString(value)
			sb.WriteString("\n")
		}

		sb.WriteString("\n")
	}

	return sb.String()
}

// Default file template.
const DefaultProfiles = `_This is profiles from different users or groups._

## 示例用户
- ID: user_example
- Type: user
- Email: example@example.com
- Telegram: @example_user
- Feishu: ou_xxx

## 示例群组
- ID: group_example
- Type: group
- Members: 示例用户, 其他成员
- Platform: feishu
- Channel: 示例群组频道
`

// EnsureDefaultFile creates default PROFILES.md if it doesn't exist.
// Returns true if the file was created.
func EnsureDefaultFile(dataDir string) (bool, error) {
	filePath := filepath.Join(dataDir, "PROFILES.md")

	if _, err := os.Stat(filePath); err == nil {
		return false, nil // File exists
	}

	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return false, fmt.Errorf("create data directory: %w", err)
	}

	if err := os.WriteFile(filePath, []byte(DefaultProfiles), 0o644); err != nil {
		return false, fmt.Errorf("write default PROFILES.md: %w", err)
	}

	return true, nil
}
