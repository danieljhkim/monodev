//go:build integration
// +build integration

package integration

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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

type realRemoteClient struct {
	repoRoot   string
	storeRepo  *stores.FileStoreRepo
	stateStore state.StateStore
	syncer     *sync.Syncer
}

func runIntegrationGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func newRealRemoteClient(t *testing.T, baseDir, name, bareRemote string) *realRemoteClient {
	t.Helper()
	repoRoot := filepath.Join(baseDir, name)
	storesDir := filepath.Join(baseDir, name+"-stores")
	workspacesDir := filepath.Join(baseDir, name+"-workspaces")
	for _, dir := range []string{repoRoot, storesDir, workspacesDir} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatal(err)
		}
	}
	runIntegrationGit(t, repoRoot, "init")
	runIntegrationGit(t, repoRoot, "remote", "add", "origin", bareRemote)

	fs := fsops.NewRealFS()
	storeRepo := stores.NewFileStoreRepo(fs, storesDir)
	configStore := remote.NewFileRemoteConfigStore(fs)
	if err := configStore.Save(repoRoot, remote.DefaultRemoteConfig()); err != nil {
		t.Fatalf("save remote config failed: %v", err)
	}

	stateStore := state.NewFileStateStore(fs, workspacesDir)
	return &realRemoteClient{
		repoRoot:   repoRoot,
		storeRepo:  storeRepo,
		stateStore: stateStore,
		syncer: sync.New(
			remote.NewRealGitPersistence(),
			storeRepo,
			stateStore,
			persist.NewSnapshotManager(fs),
			configStore,
			fs,
			hash.NewSHA256Hasher(),
			&clock.RealClock{},
		),
	}
}

func TestPushPull_WorkspaceReferenceRestoresAcrossCheckoutRoots(t *testing.T) {
	t.Setenv("GIT_AUTHOR_NAME", "Monodev Integration Test")
	t.Setenv("GIT_AUTHOR_EMAIL", "monodev-integration@example.com")
	t.Setenv("GIT_COMMITTER_NAME", "Monodev Integration Test")
	t.Setenv("GIT_COMMITTER_EMAIL", "monodev-integration@example.com")

	baseDir := t.TempDir()
	bareRemote := filepath.Join(baseDir, "remote.git")
	if err := os.MkdirAll(bareRemote, 0755); err != nil {
		t.Fatal(err)
	}
	runIntegrationGit(t, bareRemote, "init", "--bare")
	clientA := newRealRemoteClient(t, baseDir, "source-checkout", bareRemote)
	clientB := newRealRemoteClient(t, baseDir, "target-checkout", bareRemote)
	if clientA.repoRoot == clientB.repoRoot {
		t.Fatal("clients must use distinct checkout roots")
	}

	for _, storeID := range []string{"stack-store", "active-store"} {
		writeClientStoreVersion(t, clientA, storeID, storeID)
	}
	remoteWorkspaceID := "source-workspace"
	if err := clientA.stateStore.SaveWorkspace(remoteWorkspaceID, &state.WorkspaceState{
		Repo:             "source-local-fingerprint",
		WorkspacePath:    ".",
		AbsolutePath:     clientA.repoRoot,
		Applied:          true,
		Mode:             "copy",
		Stack:            []string{"stack-store"},
		AppliedStores:    []state.AppliedStore{{Store: "stack-store", Type: "copy"}, {Store: "active-store", Type: "copy"}},
		ActiveStore:      "active-store",
		ActiveStoreScope: "component",
		Paths: map[string]state.PathOwnership{
			"remote-only.txt": {Store: "active-store", Type: "copy"},
		},
	}); err != nil {
		t.Fatal(err)
	}

	if _, err := clientA.syncer.PushStore(context.Background(), &sync.PushRequest{
		RepoRoot:           clientA.repoRoot,
		StoreIDs:           []string{"stack-store", "active-store"},
		WorkspaceID:        remoteWorkspaceID,
		RepositoryIdentity: bareRemote,
		WithWorkspace:      true,
	}); err != nil {
		t.Fatalf("client A push workspace reference: %v", err)
	}

	localWorkspaceID := "target-workspace"
	result, err := clientB.syncer.PullStore(context.Background(), &sync.PullRequest{
		RepoRoot:           clientB.repoRoot,
		WorkspaceID:        remoteWorkspaceID,
		LocalWorkspaceID:   localWorkspaceID,
		RepoFingerprint:    "target-local-fingerprint",
		RepositoryIdentity: bareRemote,
		WorkspacePath:      ".",
		WithStores:         true,
	})
	if err != nil {
		t.Fatalf("client B pull workspace reference: %v", err)
	}
	if !result.WorkspaceReferenceFound || !result.WorkspaceReferenceValidated || !result.PulledWorkspace {
		t.Fatalf("workspace result = %#v, want found, validated, and restored", result)
	}
	restored, err := clientB.stateStore.LoadWorkspace(localWorkspaceID)
	if err != nil {
		t.Fatal(err)
	}
	if restored.AbsolutePath != clientB.repoRoot {
		t.Fatalf("restored AbsolutePath = %q, want local root %q", restored.AbsolutePath, clientB.repoRoot)
	}
	if restored.AbsolutePath == clientA.repoRoot {
		t.Fatal("restored workspace retained the remote machine absolute path")
	}
	if restored.ActiveStore != "active-store" || len(restored.Stack) != 0 {
		t.Fatalf("restored workspace metadata = %#v", restored)
	}
	if restored.Applied || len(restored.Paths) != 0 || len(restored.AppliedStores) != 0 {
		t.Fatalf("restored workspace claims unapplied files: %#v", restored)
	}
}

func writeClientStoreVersion(t *testing.T, client *realRemoteClient, storeID, version string) {
	t.Helper()
	exists, err := client.storeRepo.Exists(storeID)
	if err != nil {
		t.Fatal(err)
	}
	if !exists {
		meta := &stores.StoreMeta{Name: "Remote Store", Description: "transport test", Scope: "local"}
		if err := client.storeRepo.Create(storeID, meta); err != nil {
			t.Fatalf("create store failed: %v", err)
		}
	}
	if err := os.WriteFile(filepath.Join(client.storeRepo.OverlayRoot(storeID), "version.txt"), []byte(version), 0644); err != nil {
		t.Fatalf("write store version failed: %v", err)
	}
}

func readClientStoreVersion(t *testing.T, client *realRemoteClient, storeID string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(client.storeRepo.OverlayRoot(storeID), "version.txt"))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

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
	if len(gitPersist.CheckoutFetchedCalls) != 1 {
		t.Errorf("expected 1 CheckoutFetched call, got %d", len(gitPersist.CheckoutFetchedCalls))
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

func TestPushPull_MaterializesLatestFetchedCommitAcrossClients(t *testing.T) {
	t.Setenv("GIT_AUTHOR_NAME", "Monodev Integration Test")
	t.Setenv("GIT_AUTHOR_EMAIL", "monodev-integration@example.com")
	t.Setenv("GIT_COMMITTER_NAME", "Monodev Integration Test")
	t.Setenv("GIT_COMMITTER_EMAIL", "monodev-integration@example.com")

	baseDir := t.TempDir()
	bareRemote := filepath.Join(baseDir, "remote.git")
	if err := os.MkdirAll(bareRemote, 0755); err != nil {
		t.Fatal(err)
	}
	runIntegrationGit(t, bareRemote, "init", "--bare")

	clientA := newRealRemoteClient(t, baseDir, "client-a", bareRemote)
	clientB := newRealRemoteClient(t, baseDir, "client-b", bareRemote)
	ctx := context.Background()
	storeID := "portable-store"

	writeClientStoreVersion(t, clientA, storeID, "v1")
	if _, err := clientA.syncer.PushStore(ctx, &sync.PushRequest{
		RepoRoot: clientA.repoRoot,
		StoreIDs: []string{storeID},
	}); err != nil {
		t.Fatalf("client A push v1 failed: %v", err)
	}

	if _, err := clientB.syncer.PullStore(ctx, &sync.PullRequest{
		RepoRoot: clientB.repoRoot,
		StoreIDs: []string{storeID},
	}); err != nil {
		t.Fatalf("client B pull v1 failed: %v", err)
	}
	if got := readClientStoreVersion(t, clientB, storeID); got != "v1" {
		t.Fatalf("client B pulled %q, want v1", got)
	}

	writeClientStoreVersion(t, clientB, storeID, "v2")
	if _, err := clientB.syncer.PushStore(ctx, &sync.PushRequest{
		RepoRoot: clientB.repoRoot,
		StoreIDs: []string{storeID},
	}); err != nil {
		t.Fatalf("client B push v2 failed: %v", err)
	}

	// Remove A's local copy so this pull exercises transport and checksum
	// verification without invoking the separate local-content force gate.
	if err := clientA.storeRepo.Delete(storeID); err != nil {
		t.Fatalf("delete client A local store failed: %v", err)
	}
	pullResult, err := clientA.syncer.PullStore(ctx, &sync.PullRequest{
		RepoRoot: clientA.repoRoot,
		StoreIDs: []string{storeID},
	})
	if err != nil {
		t.Fatalf("client A pull v2 failed: %v", err)
	}
	if !pullResult.Verified {
		t.Fatal("client A v2 pull was not checksum verified")
	}
	if got := readClientStoreVersion(t, clientA, storeID); got != "v2" {
		t.Fatalf("client A pulled %q, want v2", got)
	}

	clientAHead := runIntegrationGit(t, clientA.repoRoot,
		"--git-dir", filepath.Join(clientA.repoRoot, ".monodev", ".git"),
		"--work-tree", filepath.Join(clientA.repoRoot, ".monodev"),
		"rev-parse", "HEAD",
	)
	remoteHead := runIntegrationGit(t, bareRemote, "rev-parse", "refs/heads/monodev/persist")
	if clientAHead != remoteHead {
		t.Fatalf("client A persistence HEAD = %s, remote fetched commit = %s", clientAHead, remoteHead)
	}

	// A later remote content change must be compared against A's existing v2
	// local store only after the fetched v3 commit is materialized.
	writeClientStoreVersion(t, clientB, storeID, "v3")
	if _, err := clientB.syncer.PushStore(ctx, &sync.PushRequest{
		RepoRoot: clientB.repoRoot,
		StoreIDs: []string{storeID},
	}); err != nil {
		t.Fatalf("client B push v3 failed: %v", err)
	}
	_, err = clientA.syncer.PullStore(ctx, &sync.PullRequest{
		RepoRoot: clientA.repoRoot,
		StoreIDs: []string{storeID},
	})
	if !errors.Is(err, sync.ErrPulledContentChanged) {
		t.Fatalf("client A pull v3 error = %v, want ErrPulledContentChanged", err)
	}
	if got := readClientStoreVersion(t, clientA, storeID); got != "v2" {
		t.Fatalf("refused pull overwrote client A local store with %q", got)
	}
	persistedV3, err := os.ReadFile(filepath.Join(clientA.repoRoot, ".monodev", "persist", "stores", storeID, "overlay", "version.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(persistedV3) != "v3" {
		t.Fatalf("client A persistence work tree = %q after fetch, want v3", persistedV3)
	}

	if _, err := clientA.syncer.PullStore(ctx, &sync.PullRequest{
		RepoRoot: clientA.repoRoot,
		StoreIDs: []string{storeID},
		Force:    true,
	}); err != nil {
		t.Fatalf("client A explicit forced content pull failed: %v", err)
	}
	if got := readClientStoreVersion(t, clientA, storeID); got != "v3" {
		t.Fatalf("client A forced pull materialized %q, want v3", got)
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
