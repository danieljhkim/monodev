package engine

import (
	"context"
	"testing"

	"github.com/danieljhkim/monodev/internal/config"
	"github.com/danieljhkim/monodev/internal/state"
)

type removeCapturingFS struct {
	*trackFileInfoFS
	removed []string
}

func newRemoveCapturingFS(paths ...string) *removeCapturingFS {
	return &removeCapturingFS{
		trackFileInfoFS: newTrackFileInfoFS(paths...),
	}
}

func (m *removeCapturingFS) RemoveAll(path string) error {
	m.removed = append(m.removed, path)
	delete(m.existingPaths, path)
	return nil
}

func TestStackUnapply_RemovesWorkspaceRelativePathForSubdirectoryWorkspace(t *testing.T) {
	gitRepo := &trackGitRepo{
		root:          "/repo",
		fingerprint:   "fp1",
		workspacePath: "services/api",
	}
	storeRepo := newTrackStoreRepo()
	stateStore := newMockStateStore()
	workspaceID := state.ComputeWorkspaceID("fp1", "services/api")
	ws := state.NewWorkspaceState("fp1", "services/api", "copy")
	ws.Stack = []string{"stack-store"}
	ws.ActiveStore = "active-store"
	ws.Paths["config.yml"] = state.PathOwnership{Store: "stack-store", Type: "copy"}
	ws.Paths["active.yml"] = state.PathOwnership{Store: "active-store", Type: "copy"}
	stateStore.workspaces[workspaceID] = ws

	fs := newRemoveCapturingFS(
		"/repo/services/api/config.yml",
		"/repo/services/api/active.yml",
		"/repo/config.yml",
	)
	eng := New(
		gitRepo,
		storeRepo,
		stateStore,
		fs,
		&mockHasher{},
		&mockClock{},
		config.Paths{Root: "/tmp/monodev", Stores: "/tmp/monodev/stores", Workspaces: "/tmp/workspaces"},
	)

	result, err := eng.StackUnapply(context.Background(), &StackUnapplyRequest{
		CWD: "/repo/services/api",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Removed) != 1 || result.Removed[0] != "config.yml" {
		t.Fatalf("Removed = %v, want [config.yml]", result.Removed)
	}

	if len(fs.removed) != 1 {
		t.Fatalf("RemoveAll calls = %v, want one call", fs.removed)
	}
	if got, want := fs.removed[0], "/repo/services/api/config.yml"; got != want {
		t.Fatalf("RemoveAll path = %q, want %q", got, want)
	}
	if !fs.existingPaths["/repo/config.yml"] {
		t.Fatal("repo-root config.yml was removed; want unrelated root file untouched")
	}
	if fs.existingPaths["/repo/services/api/config.yml"] {
		t.Fatal("workspace config.yml still exists; want stack-applied workspace file removed")
	}

	updated, err := stateStore.LoadWorkspace(workspaceID)
	if err != nil {
		t.Fatalf("failed to load workspace state: %v", err)
	}
	if _, ok := updated.Paths["config.yml"]; ok {
		t.Fatal("workspaceState.Paths still contains stack-owned config.yml")
	}
	if _, ok := updated.Paths["active.yml"]; !ok {
		t.Fatal("workspaceState.Paths removed active-store path; want only stack path removed")
	}
}
