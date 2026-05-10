package sync

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/danieljhkim/monodev/internal/state"
	"github.com/danieljhkim/monodev/internal/stores"
)

func TestSyncer_PushWorkspaceReference(t *testing.T) {
	t.Run("pushes store with workspace reference", func(t *testing.T) {
		repoRoot, _, syncer, git, storeRepo, _, cleanup := setupSyncerTest(t)
		defer cleanup()

		storeID := "test-store"
		meta := stores.NewStoreMeta("Test Store", "global", time.Now())
		if err := storeRepo.Create(storeID, meta); err != nil {
			t.Fatalf("failed to create store: %v", err)
		}

		overlayDir := storeRepo.OverlayRoot(storeID)
		if err := os.MkdirAll(overlayDir, 0755); err != nil {
			t.Fatalf("failed to create overlay dir: %v", err)
		}

		workspaceID := "workspace-123"
		appliedAt := time.Date(2026, 5, 10, 19, 0, 0, 0, time.UTC)
		if err := syncer.stateStore.SaveWorkspace(workspaceID, &state.WorkspaceState{
			Repo:             "repo-fingerprint",
			WorkspacePath:    "services/api",
			AbsolutePath:     filepath.Join(repoRoot, "services", "api"),
			Applied:          true,
			Mode:             "copy",
			Stack:            []string{"stack-store"},
			AppliedStores:    []state.AppliedStore{{Store: storeID, Type: "copy"}},
			ActiveStore:      storeID,
			ActiveStoreScope: "component",
			Paths: map[string]state.PathOwnership{
				"cmd/api/main.go": {
					Store:     storeID,
					Type:      "copy",
					Timestamp: appliedAt,
					Checksum:  "sha256:abc",
				},
			},
		}); err != nil {
			t.Fatalf("failed to save workspace: %v", err)
		}

		result, err := syncer.PushStore(context.Background(), &PushRequest{
			RepoRoot:      repoRoot,
			StoreIDs:      []string{storeID},
			WorkspaceID:   workspaceID,
			WithWorkspace: true,
			Remote:        "origin",
		})
		if err != nil {
			t.Fatalf("PushStore failed: %v", err)
		}

		if !result.PushedWorkspace {
			t.Fatal("expected workspace reference to be pushed")
		}
		expectedRefPath := filepath.Join(repoRoot, ".monodev", "persist", "workspaces", workspaceID+".json")
		if result.WorkspaceRefPath != expectedRefPath {
			t.Fatalf("WorkspaceRefPath = %q, want %q", result.WorkspaceRefPath, expectedRefPath)
		}

		data, err := os.ReadFile(expectedRefPath)
		if err != nil {
			t.Fatalf("failed to read workspace reference: %v", err)
		}

		var ref workspaceReference
		if err := json.Unmarshal(data, &ref); err != nil {
			t.Fatalf("workspace reference is not valid JSON: %v", err)
		}
		if ref.SchemaVersion != workspaceReferenceSchemaVersion {
			t.Errorf("SchemaVersion = %d, want %d", ref.SchemaVersion, workspaceReferenceSchemaVersion)
		}
		if ref.WorkspaceID != workspaceID {
			t.Errorf("WorkspaceID = %q, want %q", ref.WorkspaceID, workspaceID)
		}
		if ref.ActiveStore != storeID {
			t.Errorf("ActiveStore = %q, want %q", ref.ActiveStore, storeID)
		}
		if len(ref.Stack) != 1 || ref.Stack[0] != "stack-store" {
			t.Errorf("Stack = %#v, want [stack-store]", ref.Stack)
		}
		if ref.Mode != "copy" {
			t.Errorf("Mode = %q, want copy", ref.Mode)
		}
		if ref.PathOwnership.Count != 1 || len(ref.PathOwnership.Paths) != 1 {
			t.Fatalf("PathOwnership = %#v, want one owned path", ref.PathOwnership)
		}
		if ref.PathOwnership.Paths[0].Path != "cmd/api/main.go" {
			t.Errorf("owned path = %q, want cmd/api/main.go", ref.PathOwnership.Paths[0].Path)
		}
		if len(git.CommitCalls) != 1 {
			t.Fatalf("Commit calls = %d, want 1", len(git.CommitCalls))
		}
		if git.CommitCalls[0].Message != "push: store test-store, workspace" {
			t.Errorf("commit message = %q", git.CommitCalls[0].Message)
		}
	})

	t.Run("pushes workspace only", func(t *testing.T) {
		repoRoot, _, syncer, git, _, _, cleanup := setupSyncerTest(t)
		defer cleanup()

		workspaceID := "workspace-only"
		if err := syncer.stateStore.SaveWorkspace(workspaceID, &state.WorkspaceState{
			Repo:          "repo-fingerprint",
			WorkspacePath: ".",
			AbsolutePath:  repoRoot,
			Applied:       false,
			Mode:          "symlink",
			Stack:         []string{},
			AppliedStores: []state.AppliedStore{},
			ActiveStore:   "active-store",
			Paths:         map[string]state.PathOwnership{},
		}); err != nil {
			t.Fatalf("failed to save workspace: %v", err)
		}

		result, err := syncer.PushStore(context.Background(), &PushRequest{
			RepoRoot:      repoRoot,
			WorkspaceID:   workspaceID,
			WithWorkspace: true,
			Remote:        "origin",
		})
		if err != nil {
			t.Fatalf("PushStore failed: %v", err)
		}

		if len(result.PushedStores) != 0 {
			t.Errorf("PushedStores = %#v, want empty", result.PushedStores)
		}
		if !result.PushedWorkspace {
			t.Error("expected workspace reference to be pushed")
		}
		if _, err := os.Stat(result.WorkspaceRefPath); err != nil {
			t.Fatalf("workspace reference was not written: %v", err)
		}
		if len(git.CommitCalls) != 1 {
			t.Fatalf("Commit calls = %d, want 1", len(git.CommitCalls))
		}
		if git.CommitCalls[0].Message != "push: workspace" {
			t.Errorf("commit message = %q, want push: workspace", git.CommitCalls[0].Message)
		}
	})

	t.Run("dry run with workspace does not write artifact or execute git operations", func(t *testing.T) {
		repoRoot, _, syncer, git, _, _, cleanup := setupSyncerTest(t)
		defer cleanup()

		workspaceID := "dry-run-workspace"
		if err := syncer.stateStore.SaveWorkspace(workspaceID, &state.WorkspaceState{
			Repo:          "repo-fingerprint",
			WorkspacePath: ".",
			AbsolutePath:  repoRoot,
			Mode:          "copy",
			Stack:         []string{"stack-store"},
			ActiveStore:   "active-store",
			Paths:         map[string]state.PathOwnership{},
		}); err != nil {
			t.Fatalf("failed to save workspace: %v", err)
		}

		result, err := syncer.PushStore(context.Background(), &PushRequest{
			RepoRoot:      repoRoot,
			WorkspaceID:   workspaceID,
			WithWorkspace: true,
			Remote:        "origin",
			DryRun:        true,
		})
		if err != nil {
			t.Fatalf("PushStore failed: %v", err)
		}

		if !result.DryRun {
			t.Error("Expected DryRun = true in result")
		}
		if !result.PushedWorkspace {
			t.Error("expected dry run to report workspace reference would be pushed")
		}
		if _, err := os.Stat(result.WorkspaceRefPath); !os.IsNotExist(err) {
			t.Fatalf("workspace reference should not be written in dry run, stat err = %v", err)
		}
		if len(git.EnsureRepoCalls) > 0 {
			t.Error("EnsureRepo should not be called in dry run")
		}
		if len(git.CommitCalls) > 0 {
			t.Error("Commit should not be called in dry run")
		}
		if len(git.PushCalls) > 0 {
			t.Error("Push should not be called in dry run")
		}
	})
}
