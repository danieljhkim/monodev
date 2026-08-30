//go:build integration
// +build integration

package integration

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/danieljhkim/monodev/internal/clock"
	"github.com/danieljhkim/monodev/internal/config"
	"github.com/danieljhkim/monodev/internal/engine"
	"github.com/danieljhkim/monodev/internal/fsops"
	"github.com/danieljhkim/monodev/internal/gitx"
	"github.com/danieljhkim/monodev/internal/hash"
	"github.com/danieljhkim/monodev/internal/state"
	"github.com/danieljhkim/monodev/internal/stores"
)

func initGitRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	runGit(t, dir, "init")
	runGit(t, dir, "config", "user.email", "test@example.com")
	runGit(t, dir, "config", "user.name", "Test User")
	return dir
}

func newIdentityEngine(t *testing.T) *engine.Engine {
	t.Helper()
	root := t.TempDir()
	workspaces := filepath.Join(root, "workspaces")
	storesDir := filepath.Join(root, "stores")
	if err := os.MkdirAll(workspaces, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(storesDir, 0700); err != nil {
		t.Fatal(err)
	}
	fs := fsops.NewRealFS()
	return engine.New(
		gitx.NewRealGitRepo(),
		stores.NewFileStoreRepo(fs, storesDir),
		state.NewFileStateStore(fs, workspaces),
		fs,
		hash.NewSHA256Hasher(),
		clock.NewFakeClock(time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC)),
		config.Paths{
			Root:       root,
			Stores:     storesDir,
			Workspaces: workspaces,
			Config:     filepath.Join(root, "config.yaml"),
		},
	)
}

func TestWorkspaceIdentity_SSHToHTTPSRestoresActiveStore(t *testing.T) {
	repoDir := initGitRepo(t)
	runGit(t, repoDir, "remote", "add", "origin", "git@github.com:org/identity.git")
	eng := newIdentityEngine(t)
	ctx := context.Background()

	if err := eng.CreateStore(ctx, &engine.CreateStoreRequest{
		CWD:     repoDir,
		StoreID: "dev-store",
		Name:    "Dev Store",
		Scope:   stores.ScopeGlobal,
	}); err != nil {
		t.Fatalf("CreateStore: %v", err)
	}

	before, err := eng.Status(ctx, &engine.StatusRequest{CWD: repoDir})
	if err != nil {
		t.Fatalf("Status before: %v", err)
	}
	if before.ActiveStore != "dev-store" {
		t.Fatalf("ActiveStore before = %q", before.ActiveStore)
	}

	runGit(t, repoDir, "remote", "set-url", "origin", "https://github.com/org/identity.git")

	after, err := eng.Status(ctx, &engine.StatusRequest{CWD: repoDir})
	if err != nil {
		t.Fatalf("Status after: %v", err)
	}
	if after.WorkspaceID != before.WorkspaceID {
		t.Errorf("workspace ID changed after SSH→HTTPS rewrite: %s vs %s", before.WorkspaceID, after.WorkspaceID)
	}
	if after.ActiveStore != "dev-store" {
		t.Errorf("active store was not restored: got %q", after.ActiveStore)
	}
}

func TestWorkspaceIdentity_RemoteLessStableAfterAddingRemote(t *testing.T) {
	repoDir := initGitRepo(t)
	eng := newIdentityEngine(t)

	root, firstFP, rel, err := eng.DiscoverWorkspace(repoDir)
	if err != nil {
		t.Fatalf("DiscoverWorkspace: %v", err)
	}
	if rel != "." {
		t.Fatalf("workspace path = %q, want .", rel)
	}
	id1 := state.ComputeWorkspaceID(firstFP, rel)

	_, secondFP, _, err := eng.DiscoverWorkspace(repoDir)
	if err != nil {
		t.Fatalf("DiscoverWorkspace second: %v", err)
	}
	id2 := state.ComputeWorkspaceID(secondFP, rel)
	if id1 != id2 {
		t.Errorf("workspace ID not stable: %s vs %s", id1, id2)
	}

	runGit(t, repoDir, "remote", "add", "origin", "git@github.com:org/later.git")
	_, afterFP, _, err := eng.DiscoverWorkspace(root)
	if err != nil {
		t.Fatalf("DiscoverWorkspace after remote: %v", err)
	}
	id3 := state.ComputeWorkspaceID(afterFP, rel)
	if id3 != id1 {
		t.Errorf("adding a remote changed workspace ID: %s vs %s", id1, id3)
	}
}
