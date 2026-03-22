package profile

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseProfilesMD(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		wantCount int
		wantIDs   []string
	}{
		{
			name:      "empty content",
			input:     "",
			wantCount: 0,
		},
		{
			name:      "only intro",
			input:     "_This is profiles from different users or groups._",
			wantCount: 0,
		},
		{
			name: "single user profile",
			input: `_This is profiles from different users or groups._

## 张三
- ID: user_001
- Type: user
- Email: zhangsan@example.com
- Telegram: @zhangsan
- Feishu: ou_xxx
`,
			wantCount: 1,
			wantIDs:   []string{"user_001"},
		},
		{
			name: "multiple profiles",
			input: `_This is profiles from different users or groups._

## 张三
- ID: user_001
- Type: user
- Email: zhangsan@example.com

## 李四
- ID: user_002
- Type: user
- Telegram: @lisi

## 开发组
- ID: group_dev
- Type: group
- Members: 张三, 李四
`,
			wantCount: 3,
			wantIDs:   []string{"user_001", "user_002", "group_dev"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			profiles, err := parseProfilesMD(tt.input)
			if err != nil {
				t.Fatalf("parseProfilesMD() error = %v", err)
			}
			if len(profiles) != tt.wantCount {
				t.Errorf("parseProfilesMD() got %d profiles, want %d", len(profiles), tt.wantCount)
			}
			for _, id := range tt.wantIDs {
				if _, ok := profiles[id]; !ok {
					t.Errorf("parseProfilesMD() missing profile %s", id)
				}
			}
		})
	}
}

func TestParseProfilesMD_Properties(t *testing.T) {
	input := `_This is profiles from different users or groups._

## 张三
- ID: user_001
- Type: user
- Email: zhangsan@example.com
- Telegram: @zhangsan
- Feishu: ou_xxx
`

	profiles, err := parseProfilesMD(input)
	if err != nil {
		t.Fatalf("parseProfilesMD() error = %v", err)
	}

	p := profiles["user_001"]
	if p == nil {
		t.Fatal("profile user_001 not found")
	}

	// Check basic properties
	if p.DisplayName != "张三" {
		t.Errorf("DisplayName = %q, want %q", p.DisplayName, "张三")
	}
	if p.Type != TypeUser {
		t.Errorf("Type = %q, want %q", p.Type, TypeUser)
	}

	// Check platform IDs
	if p.PlatformIDs["telegram"] != "@zhangsan" {
		t.Errorf("Telegram = %q, want %q", p.PlatformIDs["telegram"], "@zhangsan")
	}
	if p.PlatformIDs["feishu"] != "ou_xxx" {
		t.Errorf("Feishu = %q, want %q", p.PlatformIDs["feishu"], "ou_xxx")
	}

	// Check attributes
	if p.Attributes["Email"] != "zhangsan@example.com" {
		t.Errorf("Email = %q, want %q", p.Attributes["Email"], "zhangsan@example.com")
	}
}

func TestParseProfilesMD_GroupType(t *testing.T) {
	input := `## 开发组
- ID: group_dev
- Type: group
- Members: 张三, 李四
`

	profiles, err := parseProfilesMD(input)
	if err != nil {
		t.Fatalf("parseProfilesMD() error = %v", err)
	}

	p := profiles["group_dev"]
	if p == nil {
		t.Fatal("profile group_dev not found")
	}

	if p.Type != TypeGroup {
		t.Errorf("Type = %q, want %q", p.Type, TypeGroup)
	}
}

func TestFormatProfilesMD(t *testing.T) {
	profiles := map[string]*Profile{
		"user_001": {
			ID:          "user_001",
			Type:        TypeUser,
			DisplayName: "张三",
			PlatformIDs: map[string]string{
				"telegram": "@zhangsan",
				"feishu":   "ou_xxx",
			},
			Attributes: map[string]string{
				"Email": "zhangsan@example.com",
			},
		},
	}

	output := formatProfilesMD(profiles)

	// Check essential parts
	if !contains(output, "## 张三") {
		t.Error("formatProfilesMD() missing profile heading")
	}
	if !contains(output, "ID: user_001") {
		t.Error("formatProfilesMD() missing ID")
	}
	if !contains(output, "Type: user") {
		t.Error("formatProfilesMD() missing Type")
	}
	if !contains(output, "telegram: @zhangsan") {
		t.Error("formatProfilesMD() missing telegram platform ID")
	}
}

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

func TestManager_GetByPlatformID(t *testing.T) {
	m := NewManager(t.TempDir())
	m.profiles = map[string]*Profile{
		"user_001": {
			ID:          "user_001",
			Type:        TypeUser,
			DisplayName: "张三",
			PlatformIDs: map[string]string{
				"telegram": "@zhangsan",
				"feishu":   "ou_xxx",
			},
		},
		"user_002": {
			ID:          "user_002",
			Type:        TypeUser,
			DisplayName: "李四",
			PlatformIDs: map[string]string{
				"telegram": "@lisi",
			},
		},
	}

	tests := []struct {
		platform   string
		platformID string
		wantID     string
		wantNil    bool
	}{
		{"telegram", "@zhangsan", "user_001", false},
		{"feishu", "ou_xxx", "user_001", false},
		{"telegram", "@lisi", "user_002", false},
		{"telegram", "@notfound", "", true},
		{"discord", "12345", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.platform+"_"+tt.platformID, func(t *testing.T) {
			p := m.GetByPlatformID(tt.platform, tt.platformID)
			if tt.wantNil {
				if p != nil {
					t.Errorf("GetByPlatformID() = %v, want nil", p)
				}
				return
			}
			if p == nil {
				t.Fatalf("GetByPlatformID() = nil, want profile %s", tt.wantID)
			}
			if p.ID != tt.wantID {
				t.Errorf("GetByPlatformID() ID = %q, want %q", p.ID, tt.wantID)
			}
		})
	}
}

func TestManager_AddDelete(t *testing.T) {
	dir := t.TempDir()
	m := NewManager(dir)

	// Add profile
	p := &Profile{
		ID:          "user_001",
		Type:        TypeUser,
		DisplayName: "张三",
		PlatformIDs: map[string]string{"telegram": "@zhangsan"},
	}

	if err := m.Add(p); err != nil {
		t.Fatalf("Add() error = %v", err)
	}

	// Verify file was created
	if _, err := os.Stat(filepath.Join(dir, "PROFILES.md")); os.IsNotExist(err) {
		t.Error("PROFILES.md was not created")
	}

	// Verify Get returns the profile
	got := m.Get("user_001")
	if got == nil {
		t.Fatal("Get() returned nil")
	}
	if got.DisplayName != "张三" {
		t.Errorf("DisplayName = %q, want %q", got.DisplayName, "张三")
	}

	// Delete profile
	if err := m.Delete("user_001"); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}

	// Verify deletion
	if m.Get("user_001") != nil {
		t.Error("Get() should return nil after delete")
	}
}

func TestManager_Load(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "PROFILES.md")

	content := `_This is profiles from different users or groups._

## 张三
- ID: user_001
- Type: user
- Telegram: @zhangsan
`

	if err := os.WriteFile(filePath, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	m := NewManager(dir)
	if err := m.Load(); err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	p := m.Get("user_001")
	if p == nil {
		t.Fatal("Get() returned nil")
	}
	if p.DisplayName != "张三" {
		t.Errorf("DisplayName = %q, want %q", p.DisplayName, "张三")
	}
}

func TestManager_GetByType(t *testing.T) {
	m := NewManager(t.TempDir())
	m.profiles = map[string]*Profile{
		"user_001": {ID: "user_001", Type: TypeUser},
		"user_002": {ID: "user_002", Type: TypeUser},
		"group_dev": {ID: "group_dev", Type: TypeGroup},
	}

	users := m.GetByType(TypeUser)
	if len(users) != 2 {
		t.Errorf("GetByType(user) = %d profiles, want 2", len(users))
	}

	groups := m.GetByType(TypeGroup)
	if len(groups) != 1 {
		t.Errorf("GetByType(group) = %d profiles, want 1", len(groups))
	}
}

func TestEnsureDefaultFile(t *testing.T) {
	dir := t.TempDir()

	// First call should create the file
	created, err := EnsureDefaultFile(dir)
	if err != nil {
		t.Fatalf("EnsureDefaultFile() error = %v", err)
	}
	if !created {
		t.Error("EnsureDefaultFile() created = false, want true")
	}

	// Check file exists
	if _, err := os.Stat(filepath.Join(dir, "PROFILES.md")); os.IsNotExist(err) {
		t.Error("PROFILES.md was not created")
	}

	// Second call should not create (already exists)
	created2, err := EnsureDefaultFile(dir)
	if err != nil {
		t.Fatalf("EnsureDefaultFile() second call error = %v", err)
	}
	if created2 {
		t.Error("EnsureDefaultFile() second call created = true, want false")
	}
}
