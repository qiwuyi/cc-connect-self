package identity

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadIdentity(t *testing.T) {
	// Setup: create temp directory with IDENTITY.md
	tmpDir := t.TempDir()
	identityPath := filepath.Join(tmpDir, "IDENTITY.md")

	content := `This file defines your identity. Treat it as yours.

- **Name:** 小助手
- **Creature:** AI 助手
- **Vibe:** 温暖、靠谱、简洁
- **Emoji:** 🤖
- **Background:**
  我是 cc-connect-memory 的 AI 助手，帮助用户进行编程和日常任务。`

	if err := os.WriteFile(identityPath, []byte(content), 0o644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	// Test: load identity
	identity, err := LoadIdentity(tmpDir)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	if identity == nil {
		t.Fatal("expected non-nil identity")
	}

	if identity.Content == "" {
		t.Error("expected non-empty content")
	}

	if identity.Path != identityPath {
		t.Errorf("expected path %s, got %s", identityPath, identity.Path)
	}
}

func TestLoadIdentity_NotFound(t *testing.T) {
	// Setup: empty temp directory
	tmpDir := t.TempDir()

	// Test: load from empty directory
	identity, err := LoadIdentity(tmpDir)
	if err != nil {
		t.Fatalf("Load should not fail when file not found: %v", err)
	}

	if identity != nil {
		t.Error("expected nil identity when file not found")
	}
}

func TestLoadIdentity_EmptyFile(t *testing.T) {
	// Setup: create empty IDENTITY.md
	tmpDir := t.TempDir()
	identityPath := filepath.Join(tmpDir, "IDENTITY.md")
	if err := os.WriteFile(identityPath, []byte("   \n\n  "), 0o644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	// Test: load empty file
	identity, err := LoadIdentity(tmpDir)
	if err != nil {
		t.Fatalf("Load should not fail for empty file: %v", err)
	}

	if identity != nil {
		t.Error("expected nil identity for empty file")
	}
}

func TestLoadSoul(t *testing.T) {
	// Setup: create temp directory with SOUL.md
	tmpDir := t.TempDir()
	soulPath := filepath.Join(tmpDir, "SOUL.md")

	content := `## Core Truths

**Be genuinely helpful, not performatively helpful.**

## Boundaries

- Private things stay private. Period.
- When in doubt, ask before acting externally.`

	if err := os.WriteFile(soulPath, []byte(content), 0o644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	// Test: load soul
	soul, err := LoadSoul(tmpDir)
	if err != nil {
		t.Fatalf("LoadSoul failed: %v", err)
	}

	if soul == nil {
		t.Fatal("expected non-nil soul")
	}

	if soul.Content == "" {
		t.Error("expected non-empty content")
	}
}

func TestManager_LoadAll(t *testing.T) {
	// Setup: create temp directory with both files
	tmpDir := t.TempDir()

	identityContent := `- **Name:** TestBot
- **Emoji:** 🤖`
	soulContent := `## Core Truths
Be helpful.`

	if err := os.WriteFile(filepath.Join(tmpDir, "IDENTITY.md"), []byte(identityContent), 0o644); err != nil {
		t.Fatalf("failed to write IDENTITY.md: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "SOUL.md"), []byte(soulContent), 0o644); err != nil {
		t.Fatalf("failed to write SOUL.md: %v", err)
	}

	// Test: create manager and load all
	mgr := NewManager(tmpDir)
	if err := mgr.LoadAll(); err != nil {
		t.Fatalf("LoadAll failed: %v", err)
	}

	if mgr.Identity() == nil {
		t.Error("expected non-nil identity")
	}
	if mgr.Soul() == nil {
		t.Error("expected non-nil soul")
	}
}

func TestManager_FormatContext(t *testing.T) {
	tests := []struct {
		name     string
		identity string
		soul     string
		want     string
	}{
		{
			name:     "both files present",
			identity: "- **Name:** TestBot",
			soul:     "## Core Truths\nBe helpful.",
			want:     "<identity-context>",
		},
		{
			name:     "only identity",
			identity: "- **Name:** TestBot",
			soul:     "",
			want:     "<identity-context>",
		},
		{
			name:     "only soul",
			identity: "",
			soul:     "## Core Truths\nBe helpful.",
			want:     "<identity-context>",
		},
		{
			name:     "neither file",
			identity: "",
			soul:     "",
			want:     "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()

			if tt.identity != "" {
				if err := os.WriteFile(filepath.Join(tmpDir, "IDENTITY.md"), []byte(tt.identity), 0o644); err != nil {
					t.Fatalf("failed to write IDENTITY.md: %v", err)
				}
			}
			if tt.soul != "" {
				if err := os.WriteFile(filepath.Join(tmpDir, "SOUL.md"), []byte(tt.soul), 0o644); err != nil {
					t.Fatalf("failed to write SOUL.md: %v", err)
				}
			}

			mgr := NewManager(tmpDir)
			_ = mgr.LoadAll()

			ctx := mgr.FormatContext()
			if tt.want == "" {
				if ctx != "" {
					t.Errorf("expected empty context, got: %s", ctx)
				}
			} else {
				if ctx == "" {
					t.Error("expected non-empty context")
				}
			}
		})
	}
}

func TestManager_SaveIdentity(t *testing.T) {
	tmpDir := t.TempDir()
	mgr := NewManager(tmpDir)

	content := "- **Name:** NewBot\n- **Emoji:** 🎯"

	if err := mgr.SaveIdentity(content); err != nil {
		t.Fatalf("SaveIdentity failed: %v", err)
	}

	// Verify file was written
	data, err := os.ReadFile(filepath.Join(tmpDir, "IDENTITY.md"))
	if err != nil {
		t.Fatalf("failed to read saved file: %v", err)
	}

	if string(data) != content {
		t.Errorf("expected content %q, got %q", content, string(data))
	}

	// Verify manager state was updated
	if mgr.Identity() == nil || mgr.Identity().Content != content {
		t.Error("manager state not updated")
	}
}

func TestManager_SaveSoul(t *testing.T) {
	tmpDir := t.TempDir()
	mgr := NewManager(tmpDir)

	content := "## Core Truths\nBe concise."

	if err := mgr.SaveSoul(content); err != nil {
		t.Fatalf("SaveSoul failed: %v", err)
	}

	// Verify file was written
	data, err := os.ReadFile(filepath.Join(tmpDir, "SOUL.md"))
	if err != nil {
		t.Fatalf("failed to read saved file: %v", err)
	}

	if string(data) != content {
		t.Errorf("expected content %q, got %q", content, string(data))
	}

	// Verify manager state was updated
	if mgr.Soul() == nil || mgr.Soul().Content != content {
		t.Error("manager state not updated")
	}
}

func TestEnsureDefaultFiles(t *testing.T) {
	tmpDir := t.TempDir()

	created, err := EnsureDefaultFiles(tmpDir)
	if err != nil {
		t.Fatalf("EnsureDefaultFiles failed: %v", err)
	}

	if len(created) != 2 {
		t.Errorf("expected 2 files created, got %d", len(created))
	}

	// Check files exist
	if _, err := os.Stat(filepath.Join(tmpDir, "IDENTITY.md")); os.IsNotExist(err) {
		t.Error("IDENTITY.md not created")
	}
	if _, err := os.Stat(filepath.Join(tmpDir, "SOUL.md")); os.IsNotExist(err) {
		t.Error("SOUL.md not created")
	}

	// Run again - should not create new files
	created2, err := EnsureDefaultFiles(tmpDir)
	if err != nil {
		t.Fatalf("EnsureDefaultFiles (2nd run) failed: %v", err)
	}

	if len(created2) != 0 {
		t.Errorf("expected 0 files created on 2nd run, got %d", len(created2))
	}
}
