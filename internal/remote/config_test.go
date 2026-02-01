package remote

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/danieljhkim/monodev/internal/fsops"
)

func TestDefaultRemoteConfig(t *testing.T) {
	config := DefaultRemoteConfig()
	if config.Remote != DefaultRemoteName {
		t.Errorf("expected remote %q, got %q", DefaultRemoteName, config.Remote)
	}
	if config.Branch != DefaultBranch {
		t.Errorf("expected branch %q, got %q", DefaultBranch, config.Branch)
	}
	if config.UpdatedAt.IsZero() {
		t.Error("expected non-zero UpdatedAt")
	}
}

func TestFileRemoteConfigStore_SaveAndLoad(t *testing.T) {
	tmpDir := t.TempDir()
	repoRoot := filepath.Join(tmpDir, "repo")
	if err := os.MkdirAll(repoRoot, 0755); err != nil {
		t.Fatal(err)
	}

	fs := fsops.NewRealFS()
	store := NewFileRemoteConfigStore(fs)

	// Save a config
	config := &RemoteConfig{
		Remote:    "upstream",
		Branch:    "refs/notes/custom",
		UpdatedAt: time.Now(),
	}

	if err := store.Save(repoRoot, config); err != nil {
		t.Fatalf("failed to save config: %v", err)
	}

	// Load it back
	loaded, err := store.Load(repoRoot)
	if err != nil {
		t.Fatalf("failed to load config: %v", err)
	}

	if loaded.Remote != config.Remote {
		t.Errorf("expected remote %q, got %q", config.Remote, loaded.Remote)
	}
	if loaded.Branch != config.Branch {
		t.Errorf("expected branch %q, got %q", config.Branch, loaded.Branch)
	}
	if loaded.UpdatedAt.Sub(config.UpdatedAt) > time.Second {
		t.Errorf("UpdatedAt mismatch: expected %v, got %v", config.UpdatedAt, loaded.UpdatedAt)
	}
}

func TestFileRemoteConfigStore_LoadNotConfigured(t *testing.T) {
	tmpDir := t.TempDir()
	repoRoot := filepath.Join(tmpDir, "repo")

	fs := fsops.NewRealFS()
	store := NewFileRemoteConfigStore(fs)

	// Try to load when config doesn't exist
	_, err := store.Load(repoRoot)
	if err != ErrRemoteNotConfigured {
		t.Errorf("expected ErrRemoteNotConfigured, got %v", err)
	}
}

func TestFileRemoteConfigStore_Exists(t *testing.T) {
	tmpDir := t.TempDir()
	repoRoot := filepath.Join(tmpDir, "repo")
	if err := os.MkdirAll(repoRoot, 0755); err != nil {
		t.Fatal(err)
	}

	fs := fsops.NewRealFS()
	store := NewFileRemoteConfigStore(fs)

	// Should not exist initially
	exists, err := store.Exists(repoRoot)
	if err != nil {
		t.Fatalf("Exists() failed: %v", err)
	}
	if exists {
		t.Error("expected config to not exist initially")
	}

	// Save a config
	config := DefaultRemoteConfig()
	if err := store.Save(repoRoot, config); err != nil {
		t.Fatalf("failed to save config: %v", err)
	}

	// Should exist now
	exists, err = store.Exists(repoRoot)
	if err != nil {
		t.Fatalf("Exists() failed: %v", err)
	}
	if !exists {
		t.Error("expected config to exist after save")
	}
}
