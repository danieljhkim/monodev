//go:build integration
// +build integration

package integration

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/danieljhkim/monodev/internal/clock"
	"github.com/danieljhkim/monodev/internal/fsops"
	"github.com/danieljhkim/monodev/internal/hash"
	"github.com/danieljhkim/monodev/internal/persist"
	"github.com/danieljhkim/monodev/internal/remote"
	"github.com/danieljhkim/monodev/internal/state"
	"github.com/danieljhkim/monodev/internal/stores"
	"github.com/danieljhkim/monodev/internal/sync"
)

func TestPushPull_RoundTrip(t *testing.T) {
	// Create a temporary directory for testing
	tmpDir := t.TempDir()
	repoRoot := filepath.Join(tmpDir, "repo")
	storesDir := filepath.Join(tmpDir, "stores")
	workspacesDir := filepath.Join(tmpDir, "workspaces")

	// Create directories
	if err := os.MkdirAll(repoRoot, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(storesDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(workspacesDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Initialize a git repo in repoRoot
	if err := os.MkdirAll(filepath.Join(repoRoot, ".git"), 0755); err != nil {
		t.Fatal(err)
	}

	// Create dependencies
	fs := fsops.NewRealFS()
	hasher := hash.NewSHA256Hasher()
	clk := &clock.RealClock{}
	stateStore := state.NewFileStateStore(fs, workspacesDir)
	storeRepo := stores.NewFileStoreRepo(fs, storesDir)
	gitPersist := remote.NewFakeGitPersistence()
	configStore := remote.NewFileRemoteConfigStore(fs)
	snapshotMgr := persist.NewSnapshotManager(fs)

	// Create syncer
	syncer := sync.New(gitPersist, storeRepo, stateStore, snapshotMgr, configStore, fs, hasher, clk)

	// Create a test store
	storeID := "test-store"
	meta := &stores.StoreMeta{
		Name:        "Test Store",
		Description: "A test store",
		Scope:       "local",
	}
	if err := storeRepo.Create(storeID, meta); err != nil {
		t.Fatalf("failed to create store: %v", err)
	}

	// Add a file to the store
	overlayRoot := storeRepo.OverlayRoot(storeID)
	t.Logf("Overlay root: %s", overlayRoot)
	testFile := filepath.Join(overlayRoot, "test.txt")
	if err := fs.AtomicWrite(testFile, []byte("test content"), 0644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	// Verify the store structure
	storeDir := filepath.Dir(overlayRoot)
	t.Logf("Store dir: %s", storeDir)
	if entries, err := os.ReadDir(storeDir); err == nil {
		t.Logf("Store directory contents:")
		for _, entry := range entries {
			t.Logf("  - %s (dir=%v)", entry.Name(), entry.IsDir())
		}
	}

	// Push the store
	ctx := context.Background()
	pushReq := &sync.PushRequest{
		RepoRoot: repoRoot,
		StoreIDs: []string{storeID},
		Remote:   "origin",
	}

	t.Logf("Pushing store from %s to %s", filepath.Join(storesDir, storeID), filepath.Join(repoRoot, ".monodev", "persist", "stores", storeID))
	pushResult, err := syncer.PushStore(ctx, pushReq)
	if err != nil {
		t.Fatalf("failed to push store: %v", err)
	}
	t.Logf("Push result: %+v", pushResult)

	if len(pushResult.PushedStores) != 1 {
		t.Errorf("expected 1 pushed store, got %d", len(pushResult.PushedStores))
	}
	if pushResult.PushedStores[0] != storeID {
		t.Errorf("expected pushed store %q, got %q", storeID, pushResult.PushedStores[0])
	}

	// Verify that EnsureRepo was called
	if len(gitPersist.EnsureRepoCalls) != 1 {
		t.Errorf("expected 1 EnsureRepo call, got %d", len(gitPersist.EnsureRepoCalls))
	}

	// Verify that Commit was called
	if len(gitPersist.CommitCalls) != 1 {
		t.Errorf("expected 1 Commit call, got %d", len(gitPersist.CommitCalls))
	}

	// Verify that Push was called
	if len(gitPersist.PushCalls) != 1 {
		t.Errorf("expected 1 Push call, got %d", len(gitPersist.PushCalls))
	}

	// Verify persist directory contains the store
	persistStoreDir := filepath.Join(repoRoot, ".monodev", "persist", "stores", storeID)

	// List what's actually in .monodev
	monodevDir := filepath.Join(repoRoot, ".monodev")
	if entries, err := os.ReadDir(monodevDir); err == nil {
		t.Logf(".monodev contents:")
		for _, entry := range entries {
			t.Logf("  - %s (dir=%v)", entry.Name(), entry.IsDir())
		}
	}

	persistExists, err := fs.Exists(persistStoreDir)
	if err != nil {
		t.Fatalf("failed to check persist store: %v", err)
	}
	if !persistExists {
		t.Logf("Store directory: %s", filepath.Join(storesDir, storeID))
		t.Logf("Persist directory: %s", persistStoreDir)
		t.Error("persist store directory does not exist")
	} else {
		// Verify the test file was materialized
		persistTestFile := filepath.Join(persistStoreDir, "overlay", "test.txt")
		content, err := fs.ReadFile(persistTestFile)
		if err != nil {
			t.Fatalf("failed to read persist test file: %v", err)
		}
		if string(content) != "test content" {
			t.Errorf("expected content %q, got %q", "test content", string(content))
		}
	}

	// Delete the local store to simulate pulling on a different machine
	if err := storeRepo.Delete(storeID); err != nil {
		t.Fatalf("failed to delete store: %v", err)
	}

	// Verify store is deleted
	storeExists, err := storeRepo.Exists(storeID)
	if err != nil {
		t.Fatalf("failed to check store existence: %v", err)
	}
	if storeExists {
		t.Error("store should be deleted")
	}

	// Pull the store
	pullReq := &sync.PullRequest{
		RepoRoot: repoRoot,
		StoreIDs: []string{storeID},
		Remote:   "origin",
	}

	pullResult, err := syncer.PullStore(ctx, pullReq)
	if err != nil {
		t.Fatalf("failed to pull store: %v", err)
	}

	if len(pullResult.PulledStores) != 1 {
		t.Errorf("expected 1 pulled store, got %d", len(pullResult.PulledStores))
	}
	if pullResult.PulledStores[0] != storeID {
		t.Errorf("expected pulled store %q, got %q", storeID, pullResult.PulledStores[0])
	}

	// Verify that Fetch was called
	if len(gitPersist.FetchCalls) != 1 {
		t.Errorf("expected 1 Fetch call, got %d", len(gitPersist.FetchCalls))
	}

	// Verify that Checkout was called
	if len(gitPersist.CheckoutCalls) != 1 {
		t.Errorf("expected 1 Checkout call, got %d", len(gitPersist.CheckoutCalls))
	}

	// Verify store was restored
	restoredExists, err := storeRepo.Exists(storeID)
	if err != nil {
		t.Fatalf("failed to check store existence: %v", err)
	}
	if !restoredExists {
		t.Error("store should exist after pull")
	}

	// Verify the test file was dematerialized correctly
	restoredTestFile := filepath.Join(storeRepo.OverlayRoot(storeID), "test.txt")
	restoredContent, err := fs.ReadFile(restoredTestFile)
	if err != nil {
		t.Fatalf("failed to read restored test file: %v", err)
	}
	if string(restoredContent) != "test content" {
		t.Errorf("expected content %q, got %q", "test content", string(restoredContent))
	}
}

func TestRemoteConfig_SaveLoad(t *testing.T) {
	tmpDir := t.TempDir()
	repoRoot := filepath.Join(tmpDir, "repo")

	if err := os.MkdirAll(repoRoot, 0755); err != nil {
		t.Fatal(err)
	}

	fs := fsops.NewRealFS()
	configStore := remote.NewFileRemoteConfigStore(fs)

	// Test creating default config
	config := remote.DefaultRemoteConfig()
	if err := configStore.Save(repoRoot, config); err != nil {
		t.Fatalf("failed to save config: %v", err)
	}

	// Load it back
	loaded, err := configStore.Load(repoRoot)
	if err != nil {
		t.Fatalf("failed to load config: %v", err)
	}

	if loaded.Remote != config.Remote {
		t.Errorf("expected remote %q, got %q", config.Remote, loaded.Remote)
	}
	if loaded.Branch != config.Branch {
		t.Errorf("expected branch %q, got %q", config.Branch, loaded.Branch)
	}

	// Test updating config
	loaded.Remote = "upstream"
	if err := configStore.Save(repoRoot, loaded); err != nil {
		t.Fatalf("failed to save updated config: %v", err)
	}

	// Load again
	updated, err := configStore.Load(repoRoot)
	if err != nil {
		t.Fatalf("failed to load updated config: %v", err)
	}

	if updated.Remote != "upstream" {
		t.Errorf("expected remote %q, got %q", "upstream", updated.Remote)
	}
}
