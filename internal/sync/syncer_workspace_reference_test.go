package sync

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
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
		meta := stores.NewStoreMeta("Test Store", time.Now())
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
		if len(ref.Stack) != 0 {
			t.Errorf("Stack = %#v, want empty after stack retirement", ref.Stack)
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

func TestSyncer_PullWorkspaceReferenceRestoresPortableLocalState(t *testing.T) {
	repoRoot, _, syncer, _, storeRepo, configStore, cleanup := setupSyncerTest(t)
	defer cleanup()
	savePullRemoteConfig(t, repoRoot, configStore)

	for _, storeID := range []string{"stack-store", "active-store"} {
		if err := storeRepo.Create(storeID, stores.NewStoreMeta(storeID, time.Now())); err != nil {
			t.Fatalf("create store %q: %v", storeID, err)
		}
		if err := os.WriteFile(filepath.Join(storeRepo.OverlayRoot(storeID), "portable.txt"), []byte(storeID), 0644); err != nil {
			t.Fatalf("write store %q: %v", storeID, err)
		}
	}

	remoteWorkspaceID := "source-workspace"
	if err := syncer.stateStore.SaveWorkspace(remoteWorkspaceID, &state.WorkspaceState{
		Repo:             "source-only-fingerprint",
		WorkspacePath:    "services/api",
		AbsolutePath:     "/remote-machine/checkout/services/api",
		Applied:          true,
		Mode:             "copy",
		Stack:            []string{"stack-store"},
		AppliedStores:    []state.AppliedStore{{Store: "stack-store", Type: "copy"}, {Store: "active-store", Type: "copy"}},
		ActiveStore:      "active-store",
		ActiveStoreScope: "component",
		Paths: map[string]state.PathOwnership{
			"remote-only.txt": {Store: "active-store", Type: "copy", Timestamp: time.Now()},
		},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := syncer.PushStore(context.Background(), &PushRequest{
		RepoRoot:           repoRoot,
		StoreIDs:           []string{"stack-store", "active-store"},
		WorkspaceID:        remoteWorkspaceID,
		RepositoryIdentity: "git@example.test:team/repo.git",
		WithWorkspace:      true,
		Remote:             "origin",
	}); err != nil {
		t.Fatalf("push workspace reference: %v", err)
	}
	if err := syncer.stateStore.DeleteWorkspace(remoteWorkspaceID); err != nil {
		t.Fatal(err)
	}

	localWorkspaceID := "local-workspace"
	result, err := syncer.PullStore(context.Background(), &PullRequest{
		RepoRoot:           repoRoot,
		WorkspaceID:        remoteWorkspaceID,
		LocalWorkspaceID:   localWorkspaceID,
		RepoFingerprint:    "local-only-fingerprint",
		RepositoryIdentity: "git@example.test:team/repo.git",
		WorkspacePath:      "services/api",
		WithStores:         true,
	})
	if err != nil {
		t.Fatalf("pull workspace reference: %v", err)
	}
	if !result.WorkspaceReferenceFound || !result.WorkspaceReferenceValidated || !result.PulledWorkspace {
		t.Fatalf("workspace result = %#v, want found, validated, and restored", result)
	}
	if result.WorkspaceID != localWorkspaceID {
		t.Errorf("WorkspaceID = %q, want local %q", result.WorkspaceID, localWorkspaceID)
	}

	restored, err := syncer.stateStore.LoadWorkspace(localWorkspaceID)
	if err != nil {
		t.Fatal(err)
	}
	if restored.AbsolutePath != filepath.Join(repoRoot, "services/api") {
		t.Errorf("AbsolutePath = %q, want local checkout path", restored.AbsolutePath)
	}
	if restored.Repo != "local-only-fingerprint" {
		t.Errorf("Repo = %q, want local fingerprint", restored.Repo)
	}
	if restored.ActiveStore != "active-store" || restored.ActiveStoreScope != "component" {
		t.Errorf("active store = %q/%q", restored.ActiveStore, restored.ActiveStoreScope)
	}
	if len(restored.Stack) != 0 {
		t.Errorf("Stack = %#v, want empty after stack retirement", restored.Stack)
	}
	if restored.Applied || len(restored.Paths) != 0 || len(restored.AppliedStores) != 0 {
		t.Errorf("restored state claims remote applied files: %#v", restored)
	}
}

func TestSyncer_LoadsLegacyWorkspaceReferenceFixture(t *testing.T) {
	repoRoot, _, syncer, _, _, _, cleanup := setupSyncerTest(t)
	defer cleanup()

	workspaceID := "legacy-workspace"
	path := workspaceReferencePath(repoRoot, workspaceID)
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	fixture, err := os.ReadFile(filepath.Join("testdata", "legacy_workspace_reference.json"))
	if err != nil {
		t.Fatalf("read legacy workspace-reference fixture: %v", err)
	}
	if err := os.WriteFile(path, fixture, 0600); err != nil {
		t.Fatalf("write legacy workspace-reference fixture: %v", err)
	}
	ref, found, err := syncer.loadWorkspaceReference(&PullRequest{
		RepoRoot:           repoRoot,
		WorkspaceID:        workspaceID,
		LocalWorkspaceID:   "local-workspace",
		RepoFingerprint:    "local-fingerprint",
		RepositoryIdentity: "git@example.test:team/repo.git",
		WorkspacePath:      ".",
	})
	if err != nil || !found {
		t.Fatalf("loadWorkspaceReference() = %#v, %v, want legacy fixture", ref, err)
	}
	if ref.SchemaVersion != workspaceReferenceSchemaVersion || ref.ActiveStore != "active-store" {
		t.Fatalf("workspace reference = %#v, want legacy fixture", ref)
	}
}

func TestSyncer_PullWorkspaceReferenceFailsClosed(t *testing.T) {
	for _, tt := range []struct {
		name       string
		mutate     func(*workspaceReference)
		localState bool
		wantError  string
	}{
		{
			name:      "unknown schema",
			mutate:    func(ref *workspaceReference) { ref.SchemaVersion++ },
			wantError: "upgrade monodev",
		},
		{
			name:      "repository mismatch",
			mutate:    func(ref *workspaceReference) { ref.Repo = "git@example.test:other/repo.git" },
			wantError: "repository mismatch",
		},
		{
			name:      "missing store",
			mutate:    func(ref *workspaceReference) { ref.ActiveStore = "missing-store" },
			wantError: "unavailable locally",
		},
		{
			name:       "local state conflict",
			localState: true,
			mutate:     func(*workspaceReference) {},
			wantError:  "already exists",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			repoRoot, _, syncer, _, storeRepo, configStore, cleanup := setupSyncerTest(t)
			defer cleanup()
			savePullRemoteConfig(t, repoRoot, configStore)
			if err := storeRepo.Create("active-store", stores.NewStoreMeta("active", time.Now())); err != nil {
				t.Fatal(err)
			}

			remoteWorkspaceID := "remote-workspace"
			ref := workspaceReference{
				SchemaVersion: workspaceReferenceSchemaVersion,
				WorkspaceID:   remoteWorkspaceID,
				Repo:          "git@example.test:team/repo.git",
				WorkspacePath: ".",
				Mode:          "copy",
				ActiveStore:   "active-store",
				Stack:         []string{},
				PathOwnership: workspacePathOwnershipSummary{},
			}
			tt.mutate(&ref)
			data, err := json.Marshal(ref)
			if err != nil {
				t.Fatal(err)
			}
			refPath := workspaceReferencePath(repoRoot, remoteWorkspaceID)
			if err := os.MkdirAll(filepath.Dir(refPath), 0755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(refPath, data, 0600); err != nil {
				t.Fatal(err)
			}

			localWorkspaceID := "local-workspace"
			if tt.localState {
				if err := syncer.stateStore.SaveWorkspace(localWorkspaceID, &state.WorkspaceState{ActiveStore: "local-store", Paths: map[string]state.PathOwnership{}}); err != nil {
					t.Fatal(err)
				}
			}
			_, err = syncer.PullStore(context.Background(), &PullRequest{
				RepoRoot:           repoRoot,
				WorkspaceID:        remoteWorkspaceID,
				LocalWorkspaceID:   localWorkspaceID,
				RepoFingerprint:    "local-fingerprint",
				RepositoryIdentity: "git@example.test:team/repo.git",
				WorkspacePath:      ".",
			})
			if err == nil || !strings.Contains(err.Error(), tt.wantError) {
				t.Fatalf("PullStore error = %v, want %q", err, tt.wantError)
			}
			if tt.name == "unknown schema" {
				for _, want := range []string{refPath, "schemaVersion 2", "supported schemaVersion 1", "upgrade monodev"} {
					if !strings.Contains(err.Error(), want) {
						t.Errorf("PullStore error = %q, want %q", err, want)
					}
				}
			}

			stateAfter, loadErr := syncer.stateStore.LoadWorkspace(localWorkspaceID)
			if tt.localState {
				if loadErr != nil || stateAfter.ActiveStore != "local-store" {
					t.Fatalf("local workspace state changed after refused restore: %#v, %v", stateAfter, loadErr)
				}
			} else if !os.IsNotExist(loadErr) {
				t.Fatalf("workspace state was written after refused restore: %#v, %v", stateAfter, loadErr)
			}
		})
	}
}

// TestSyncer_PullWorkspaceReferenceAcceptsEquivalentRepositoryIdentity covers
// producers and consumers that spell the same remote differently. Restoring
// must follow the documented remote-identity equivalence rules rather than
// comparing the raw strings.
func TestSyncer_PullWorkspaceReferenceAcceptsEquivalentRepositoryIdentity(t *testing.T) {
	aliasBase := t.TempDir()
	realRemote := filepath.Join(aliasBase, "real", "remote.git")
	if err := os.MkdirAll(realRemote, 0755); err != nil {
		t.Fatal(err)
	}
	aliasRemote := filepath.Join(aliasBase, "alias", "remote.git")
	if err := os.Symlink(filepath.Join(aliasBase, "real"), filepath.Join(aliasBase, "alias")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	for _, tt := range []struct {
		name            string
		pushedIdentity  string
		pullingIdentity string
	}{
		{
			name:            "ssh and https spellings",
			pushedIdentity:  "git@example.test:team/repo.git",
			pullingIdentity: "https://example.test/team/repo",
		},
		{
			name:            "local remote reached through a filesystem alias",
			pushedIdentity:  realRemote,
			pullingIdentity: aliasRemote,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			repoRoot, _, syncer, _, storeRepo, configStore, cleanup := setupSyncerTest(t)
			defer cleanup()
			savePullRemoteConfig(t, repoRoot, configStore)

			if err := storeRepo.Create("active-store", stores.NewStoreMeta("active", time.Now())); err != nil {
				t.Fatal(err)
			}

			remoteWorkspaceID := "source-workspace"
			if err := syncer.stateStore.SaveWorkspace(remoteWorkspaceID, &state.WorkspaceState{
				Repo:          "source-only-fingerprint",
				WorkspacePath: "services/api",
				Mode:          "copy",
				ActiveStore:   "active-store",
				Paths:         map[string]state.PathOwnership{},
			}); err != nil {
				t.Fatal(err)
			}
			if _, err := syncer.PushStore(context.Background(), &PushRequest{
				RepoRoot:           repoRoot,
				StoreIDs:           []string{"active-store"},
				WorkspaceID:        remoteWorkspaceID,
				RepositoryIdentity: tt.pushedIdentity,
				WithWorkspace:      true,
				Remote:             "origin",
			}); err != nil {
				t.Fatalf("push workspace reference: %v", err)
			}
			if err := syncer.stateStore.DeleteWorkspace(remoteWorkspaceID); err != nil {
				t.Fatal(err)
			}

			result, err := syncer.PullStore(context.Background(), &PullRequest{
				RepoRoot:           repoRoot,
				WorkspaceID:        remoteWorkspaceID,
				LocalWorkspaceID:   "local-workspace",
				RepoFingerprint:    "local-only-fingerprint",
				RepositoryIdentity: tt.pullingIdentity,
				WorkspacePath:      "services/api",
			})
			if err != nil {
				t.Fatalf("pull workspace reference pushed as %q and pulled as %q: %v", tt.pushedIdentity, tt.pullingIdentity, err)
			}
			if !result.WorkspaceReferenceValidated || !result.PulledWorkspace {
				t.Fatalf("workspace result = %#v, want validated and restored", result)
			}
			restored, err := syncer.stateStore.LoadWorkspace("local-workspace")
			if err != nil {
				t.Fatalf("load restored workspace: %v", err)
			}
			if restored.ActiveStore != "active-store" {
				t.Errorf("restored ActiveStore = %q, want active-store", restored.ActiveStore)
			}
		})
	}
}
