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

func writeTestWorkspaceReference(t *testing.T, repoRoot string, ref workspaceReference) {
	t.Helper()
	data, err := json.Marshal(ref)
	if err != nil {
		t.Fatal(err)
	}
	path := workspaceReferencePath(repoRoot, ref.WorkspaceID)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatal(err)
	}
}

func testWorkspaceReference(id, repo, workspacePath string, generatedAt time.Time, storeIDs ...string) workspaceReference {
	ref := workspaceReference{
		SchemaVersion: workspaceReferenceSchemaVersion,
		WorkspaceID:   id,
		Repo:          repo,
		WorkspacePath: workspacePath,
		Mode:          "copy",
		Stack:         []string{},
		PathOwnership: workspacePathOwnershipSummary{Paths: []workspacePathOwnership{}},
		GeneratedAt:   generatedAt,
	}
	for _, storeID := range storeIDs {
		ref.AppliedStores = append(ref.AppliedStores, state.AppliedStore{Store: storeID, Type: "copy"})
	}
	if len(storeIDs) > 0 {
		ref.ActiveStore = storeIDs[len(storeIDs)-1]
	}
	return ref
}

func TestFindWorkspaceReference(t *testing.T) {
	const repo = "https://example.com/team/project.git"
	older := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	newer := older.Add(time.Hour)

	t.Run("no persistence work tree", func(t *testing.T) {
		repoRoot, _, syncer, _, _, _, cleanup := setupSyncerTest(t)
		defer cleanup()
		match, err := syncer.FindWorkspaceReference(repoRoot, repo, ".")
		if err != nil || match != nil {
			t.Fatalf("FindWorkspaceReference = %#v, %v; want nil, nil", match, err)
		}
	})

	t.Run("matches by repository identity and path, newest wins", func(t *testing.T) {
		repoRoot, _, syncer, _, _, _, cleanup := setupSyncerTest(t)
		defer cleanup()
		writeTestWorkspaceReference(t, repoRoot, testWorkspaceReference("old-machine", repo, "services/api", older, "old-store"))
		writeTestWorkspaceReference(t, repoRoot, testWorkspaceReference("new-machine", "git@example.com:team/project.git", "services/api", newer, "base", "notes"))
		writeTestWorkspaceReference(t, repoRoot, testWorkspaceReference("other-path", repo, "services/web", newer.Add(time.Hour), "web"))
		writeTestWorkspaceReference(t, repoRoot, testWorkspaceReference("other-repo", "https://example.com/other/project.git", "services/api", newer.Add(time.Hour), "other"))

		match, err := syncer.FindWorkspaceReference(repoRoot, repo, "services/api")
		if err != nil {
			t.Fatalf("FindWorkspaceReference error = %v", err)
		}
		if match == nil || match.WorkspaceID != "new-machine" {
			t.Fatalf("match = %#v, want new-machine", match)
		}
		if match.ActiveStore != "notes" || len(match.AppliedStores) != 2 || match.AppliedStores[0] != "base" {
			t.Fatalf("match stores = %#v", match)
		}
		if len(match.StoreIDs) != 2 {
			t.Fatalf("match.StoreIDs = %v, want base and notes", match.StoreIDs)
		}
	})

	t.Run("skips unreadable and newer-schema references", func(t *testing.T) {
		repoRoot, _, syncer, _, _, _, cleanup := setupSyncerTest(t)
		defer cleanup()
		future := testWorkspaceReference("future", repo, ".", newer)
		future.SchemaVersion = workspaceReferenceSchemaVersion + 1
		writeTestWorkspaceReference(t, repoRoot, future)
		dir := workspaceReferencesDir(repoRoot)
		if err := os.WriteFile(filepath.Join(dir, "broken.json"), []byte("{"), 0644); err != nil {
			t.Fatal(err)
		}

		match, err := syncer.FindWorkspaceReference(repoRoot, repo, ".")
		if err != nil || match != nil {
			t.Fatalf("FindWorkspaceReference = %#v, %v; want nil, nil", match, err)
		}
	})
}

func TestPullStoreSkipFetchRestoresFromWorkTree(t *testing.T) {
	repoRoot, _, syncer, git, storeRepo, configStore, cleanup := setupSyncerTest(t)
	defer cleanup()
	delete(configStore.configs, repoRoot)

	storeID := "notes"
	if err := storeRepo.Create(storeID, stores.NewStoreMeta(storeID, time.Now())); err != nil {
		t.Fatal(err)
	}
	if err := syncer.snapshotMgr.Materialize(storeID, storeRepo, repoRoot); err != nil {
		t.Fatal(err)
	}
	const repo = "https://example.com/team/project.git"
	writeTestWorkspaceReference(t, repoRoot, testWorkspaceReference("remote-ws", repo, ".", time.Now(), storeID))

	result, err := syncer.PullStore(context.Background(), &PullRequest{
		RepoRoot:           repoRoot,
		StoreIDs:           []string{storeID},
		WorkspaceID:        "remote-ws",
		LocalWorkspaceID:   "local-ws",
		RepoFingerprint:    "local-fingerprint",
		RepositoryIdentity: repo,
		WorkspacePath:      ".",
		WithStores:         true,
		SkipFetch:          true,
	})
	if err != nil {
		t.Fatalf("PullStore error = %v", err)
	}
	if len(git.FetchCalls) != 0 {
		t.Fatalf("SkipFetch pull fetched %d times", len(git.FetchCalls))
	}
	if !result.PulledWorkspace || result.WorkspaceID != "local-ws" {
		t.Fatalf("result = %#v, want restored local-ws", result)
	}
	restored, err := syncer.stateStore.LoadWorkspace("local-ws")
	if err != nil {
		t.Fatalf("restored workspace: %v", err)
	}
	if restored.ActiveStore != storeID || restored.Applied {
		t.Fatalf("restored = %#v, want active %s and not applied", restored, storeID)
	}
}
