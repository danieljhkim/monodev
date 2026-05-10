package engine

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/danieljhkim/monodev/internal/config"
	"github.com/danieljhkim/monodev/internal/hash"
	"github.com/danieljhkim/monodev/internal/state"
)

func newUnapplyDriftEngine(gitRepo *trackGitRepo, stateStore *mockStateStore, fs *removeCapturingFS, hasher *hash.FakeHasher) *Engine {
	return New(
		gitRepo,
		newTrackStoreRepo(),
		stateStore,
		fs,
		hasher,
		&mockClock{},
		config.Paths{Root: "/tmp/monodev", Stores: "/tmp/monodev/stores", Workspaces: "/tmp/workspaces"},
	)
}

func TestUnapply_CopyModeChecksumDriftFailsWithoutForce(t *testing.T) {
	gitRepo := &trackGitRepo{root: "/repo", fingerprint: "fp1", workspacePath: "."}
	stateStore := newMockStateStore()
	workspaceID := state.ComputeWorkspaceID("fp1", ".")
	ws := state.NewWorkspaceState("fp1", ".", "copy")
	ws.Applied = true
	ws.ActiveStore = "active-store"
	ws.Paths["config.yml"] = state.PathOwnership{
		Store:    "active-store",
		Type:     "copy",
		Checksum: "original-checksum",
	}
	stateStore.workspaces[workspaceID] = ws

	fs := newRemoveCapturingFS("/repo/config.yml")
	hasher := hash.NewFakeHasher()
	hasher.SetHash("/repo/config.yml", "modified-checksum")
	eng := newUnapplyDriftEngine(gitRepo, stateStore, fs, hasher)

	result, err := eng.Unapply(context.Background(), &UnapplyRequest{
		CWD: "/repo",
	})

	if result != nil {
		t.Fatalf("Unapply result = %#v, want nil", result)
	}
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("Unapply error = %v, want ErrValidation", err)
	}
	if !errors.Is(err, ErrDrift) {
		t.Fatalf("Unapply error = %v, want ErrDrift", err)
	}
	errText := err.Error()
	if !strings.Contains(errText, "config.yml") {
		t.Fatalf("Unapply error = %q, want workspace-relative path", errText)
	}
	if !strings.Contains(errText, "local modifications detected") {
		t.Fatalf("Unapply error = %q, want local modifications message", errText)
	}
	if len(fs.removed) != 0 {
		t.Fatalf("RemoveAll calls = %v, want none", fs.removed)
	}
	if !fs.existingPaths["/repo/config.yml"] {
		t.Fatal("drifted workspace file was removed")
	}

	updated, err := stateStore.LoadWorkspace(workspaceID)
	if err != nil {
		t.Fatalf("failed to load workspace state: %v", err)
	}
	if _, ok := updated.Paths["config.yml"]; !ok {
		t.Fatal("workspaceState.Paths removed drifted config.yml; want entry intact")
	}
}

func TestStackUnapply_CopyModeChecksumDriftFailsWithoutForce(t *testing.T) {
	gitRepo := &trackGitRepo{root: "/repo", fingerprint: "fp1", workspacePath: "."}
	stateStore := newMockStateStore()
	workspaceID := state.ComputeWorkspaceID("fp1", ".")
	ws := state.NewWorkspaceState("fp1", ".", "copy")
	ws.Applied = true
	ws.ActiveStore = "active-store"
	ws.Stack = []string{"stack-store"}
	ws.Paths["stack/config.yml"] = state.PathOwnership{
		Store:    "stack-store",
		Type:     "copy",
		Checksum: "original-checksum",
	}
	ws.Paths["active.yml"] = state.PathOwnership{Store: "active-store", Type: "copy"}
	stateStore.workspaces[workspaceID] = ws

	fs := newRemoveCapturingFS("/repo/stack/config.yml", "/repo/active.yml")
	hasher := hash.NewFakeHasher()
	hasher.SetHash("/repo/stack/config.yml", "modified-checksum")
	eng := newUnapplyDriftEngine(gitRepo, stateStore, fs, hasher)

	result, err := eng.StackUnapply(context.Background(), &StackUnapplyRequest{
		CWD: "/repo",
	})

	if result != nil {
		t.Fatalf("StackUnapply result = %#v, want nil", result)
	}
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("StackUnapply error = %v, want ErrValidation", err)
	}
	if !errors.Is(err, ErrDrift) {
		t.Fatalf("StackUnapply error = %v, want ErrDrift", err)
	}
	errText := err.Error()
	if !strings.Contains(errText, "stack/config.yml") {
		t.Fatalf("StackUnapply error = %q, want workspace-relative path", errText)
	}
	if !strings.Contains(errText, "local modifications detected") {
		t.Fatalf("StackUnapply error = %q, want local modifications message", errText)
	}
	if len(fs.removed) != 0 {
		t.Fatalf("RemoveAll calls = %v, want none", fs.removed)
	}
	if !fs.existingPaths["/repo/stack/config.yml"] {
		t.Fatal("drifted stack workspace file was removed")
	}

	updated, err := stateStore.LoadWorkspace(workspaceID)
	if err != nil {
		t.Fatalf("failed to load workspace state: %v", err)
	}
	if _, ok := updated.Paths["stack/config.yml"]; !ok {
		t.Fatal("workspaceState.Paths removed drifted stack/config.yml; want entry intact")
	}
}

func TestUnapply_ForceRemovesDriftedCopyAndUpdatesWorkspaceState(t *testing.T) {
	gitRepo := &trackGitRepo{root: "/repo", fingerprint: "fp1", workspacePath: "."}
	stateStore := newMockStateStore()
	workspaceID := state.ComputeWorkspaceID("fp1", ".")
	ws := state.NewWorkspaceState("fp1", ".", "copy")
	ws.Applied = true
	ws.ActiveStore = "active-store"
	ws.Stack = []string{"stack-store"}
	ws.Paths["config.yml"] = state.PathOwnership{
		Store:    "active-store",
		Type:     "copy",
		Checksum: "original-checksum",
	}
	ws.Paths["stack.yml"] = state.PathOwnership{Store: "stack-store", Type: "copy"}
	ws.AppliedStores = []state.AppliedStore{
		{Store: "active-store", Type: "copy"},
		{Store: "stack-store", Type: "copy"},
	}
	stateStore.workspaces[workspaceID] = ws

	fs := newRemoveCapturingFS("/repo/config.yml", "/repo/stack.yml")
	hasher := hash.NewFakeHasher()
	hasher.SetHash("/repo/config.yml", "modified-checksum")
	eng := newUnapplyDriftEngine(gitRepo, stateStore, fs, hasher)

	result, err := eng.Unapply(context.Background(), &UnapplyRequest{
		CWD:   "/repo",
		Force: true,
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Removed) != 1 || result.Removed[0] != "config.yml" {
		t.Fatalf("Removed = %v, want [config.yml]", result.Removed)
	}
	if len(fs.removed) != 1 || fs.removed[0] != "/repo/config.yml" {
		t.Fatalf("RemoveAll calls = %v, want [/repo/config.yml]", fs.removed)
	}
	if fs.existingPaths["/repo/config.yml"] {
		t.Fatal("drifted workspace file still exists; want force to remove it")
	}

	updated, err := stateStore.LoadWorkspace(workspaceID)
	if err != nil {
		t.Fatalf("failed to load workspace state: %v", err)
	}
	if _, ok := updated.Paths["config.yml"]; ok {
		t.Fatal("workspaceState.Paths still contains force-removed config.yml")
	}
	if _, ok := updated.Paths["stack.yml"]; !ok {
		t.Fatal("workspaceState.Paths removed stack.yml; want stack entry intact")
	}
	if updated.GetAppliedStore("active-store") != nil {
		t.Fatal("AppliedStores still contains active-store; want pruned after force unapply")
	}
	if updated.GetAppliedStore("stack-store") == nil {
		t.Fatal("AppliedStores removed stack-store; want stack store retained")
	}
}

func TestUnapply_MissingCopyAndSymlinkPathsStillRemoveStateEntries(t *testing.T) {
	tests := []struct {
		name      string
		relPath   string
		ownership state.PathOwnership
		existing  []string
	}{
		{
			name:    "missing copy with checksum",
			relPath: "missing.yml",
			ownership: state.PathOwnership{
				Store:    "active-store",
				Type:     "copy",
				Checksum: "original-checksum",
			},
		},
		{
			name:    "symlink skips checksum drift validation",
			relPath: "linked.yml",
			ownership: state.PathOwnership{
				Store:    "active-store",
				Type:     "symlink",
				Checksum: "original-checksum",
			},
			existing: []string{"/repo/linked.yml"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gitRepo := &trackGitRepo{root: "/repo", fingerprint: "fp1", workspacePath: "."}
			stateStore := newMockStateStore()
			workspaceID := state.ComputeWorkspaceID("fp1", ".")
			ws := state.NewWorkspaceState("fp1", ".", "copy")
			ws.Applied = true
			ws.ActiveStore = "active-store"
			ws.Stack = []string{"stack-store"}
			ws.Paths[tt.relPath] = tt.ownership
			ws.Paths["stack.yml"] = state.PathOwnership{Store: "stack-store", Type: "copy"}
			stateStore.workspaces[workspaceID] = ws

			fs := newRemoveCapturingFS(tt.existing...)
			hasher := hash.NewFakeHasher()
			hasher.SetHash("/repo/"+tt.relPath, "modified-checksum")
			eng := newUnapplyDriftEngine(gitRepo, stateStore, fs, hasher)

			result, err := eng.Unapply(context.Background(), &UnapplyRequest{
				CWD: "/repo",
			})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(result.Removed) != 1 || result.Removed[0] != tt.relPath {
				t.Fatalf("Removed = %v, want [%s]", result.Removed, tt.relPath)
			}

			updated, err := stateStore.LoadWorkspace(workspaceID)
			if err != nil {
				t.Fatalf("failed to load workspace state: %v", err)
			}
			if _, ok := updated.Paths[tt.relPath]; ok {
				t.Fatalf("workspaceState.Paths still contains %s", tt.relPath)
			}
			if _, ok := updated.Paths["stack.yml"]; !ok {
				t.Fatal("workspaceState.Paths removed stack.yml; want unrelated entry intact")
			}
		})
	}
}
