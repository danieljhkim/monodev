//go:build integration
// +build integration

package integration

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/danieljhkim/monodev/internal/engine"
	"github.com/danieljhkim/monodev/internal/stores"
)

// TestWorktree_SharedStoreAppliesOutsideMainCheckout verifies the parallel-
// agent story end to end: a store tracked and committed from the main
// checkout is visible from a linked `git worktree` created at an unrelated
// path (not nested under the main checkout), and applying it there installs
// the tracked dev-only file into the worktree's own working tree.
func TestWorktree_SharedStoreAppliesOutsideMainCheckout(t *testing.T) {
	mainDir := initGitRepo(t)
	if err := os.WriteFile(filepath.Join(mainDir, "app.go"), []byte("package app\n"), 0600); err != nil {
		t.Fatal(err)
	}
	runGit(t, mainDir, "add", "app.go")
	runGit(t, mainDir, "commit", "-m", "init")

	eng := newIdentityEngine(t)
	ctx := context.Background()

	if err := eng.CreateStore(ctx, &engine.CreateStoreRequest{
		CWD:     mainDir,
		StoreID: "dev-overlay",
		Name:    "Dev overlay",
		Scope:   stores.ScopeGlobal,
		Owner:   "tester",
	}); err != nil {
		t.Fatalf("CreateStore: %v", err)
	}

	devFile := filepath.Join(mainDir, ".env.local")
	if err := os.WriteFile(devFile, []byte("SECRET=shh\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := eng.Track(ctx, &engine.TrackRequest{CWD: mainDir, Paths: []string{".env.local"}}); err != nil {
		t.Fatalf("Track: %v", err)
	}
	if _, err := eng.Commit(ctx, &engine.CommitRequest{CWD: mainDir, All: true}); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	// Create the worktree completely outside the main checkout's directory
	// tree, mirroring how a parallel coding agent's workspace is laid out.
	worktreeParent := t.TempDir()
	worktreeDir := filepath.Join(worktreeParent, "parallel-agent")
	runGit(t, mainDir, "worktree", "add", worktreeDir, "-b", "agent-branch")

	if _, err := eng.Apply(ctx, &engine.ApplyRequest{
		CWD:      worktreeDir,
		Mode:     "copy",
		StoreIDs: []string{"dev-overlay"},
	}); err != nil {
		t.Fatalf("Apply (worktree): %v", err)
	}

	got, err := os.ReadFile(filepath.Join(worktreeDir, ".env.local"))
	if err != nil {
		t.Fatalf("expected tracked file applied in worktree: %v", err)
	}
	if string(got) != "SECRET=shh\n" {
		t.Errorf("worktree overlay content = %q, want %q", got, "SECRET=shh\n")
	}

	// The store lives once, addressed by ID, regardless of which worktree
	// applies it.
	status, err := eng.Status(ctx, &engine.StatusRequest{CWD: mainDir})
	if err != nil {
		t.Fatalf("Status (main): %v", err)
	}
	if status.ActiveStore != "dev-overlay" {
		t.Errorf("main checkout ActiveStore = %q, want dev-overlay", status.ActiveStore)
	}
}

// TestWorktree_AppliedLedgerIsIndependentPerWorktree asserts the other half
// of the design: while the *store* (its content, addressed by ID) is shared
// across worktrees of the same repository, the *applied-overlay ledger* is
// not. Unapplying in a linked worktree must not disturb the main checkout's
// own applied state, because each worktree has its own working tree and
// therefore its own set of files actually on disk.
func TestWorktree_AppliedLedgerIsIndependentPerWorktree(t *testing.T) {
	mainDir := initGitRepo(t)
	runGit(t, mainDir, "commit", "--allow-empty", "-m", "init")

	eng := newIdentityEngine(t)
	ctx := context.Background()

	if err := eng.CreateStore(ctx, &engine.CreateStoreRequest{
		CWD:     mainDir,
		StoreID: "dev-overlay",
		Name:    "Dev overlay",
		Scope:   stores.ScopeGlobal,
		Owner:   "tester",
	}); err != nil {
		t.Fatalf("CreateStore: %v", err)
	}

	devFile := filepath.Join(mainDir, "local.cfg")
	if err := os.WriteFile(devFile, []byte("mode=dev\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := eng.Track(ctx, &engine.TrackRequest{CWD: mainDir, Paths: []string{"local.cfg"}}); err != nil {
		t.Fatalf("Track: %v", err)
	}
	if _, err := eng.Commit(ctx, &engine.CommitRequest{CWD: mainDir, All: true}); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	// Apply in the main checkout using the active store (no explicit StoreIDs).
	if _, err := eng.Apply(ctx, &engine.ApplyRequest{CWD: mainDir, Mode: "copy"}); err != nil {
		t.Fatalf("Apply (main): %v", err)
	}

	worktreeDir := filepath.Join(t.TempDir(), "agent-two")
	runGit(t, mainDir, "worktree", "add", worktreeDir, "-b", "agent-two-branch")

	if _, err := eng.Apply(ctx, &engine.ApplyRequest{
		CWD:      worktreeDir,
		Mode:     "copy",
		StoreIDs: []string{"dev-overlay"},
	}); err != nil {
		t.Fatalf("Apply (worktree): %v", err)
	}

	mainStatusBefore, err := eng.Status(ctx, &engine.StatusRequest{CWD: mainDir})
	if err != nil {
		t.Fatalf("Status (main, before): %v", err)
	}
	if !mainStatusBefore.Applied {
		t.Fatalf("main checkout expected Applied=true before worktree unapply")
	}

	if _, err := eng.Unapply(ctx, &engine.UnapplyRequest{CWD: worktreeDir}); err != nil {
		t.Fatalf("Unapply (worktree): %v", err)
	}

	if _, err := os.Stat(filepath.Join(worktreeDir, "local.cfg")); !os.IsNotExist(err) {
		t.Errorf("expected local.cfg removed from worktree, stat err = %v", err)
	}

	if _, err := os.Stat(devFile); err != nil {
		t.Errorf("main checkout's applied file was removed by worktree unapply: %v", err)
	}
	mainStatusAfter, err := eng.Status(ctx, &engine.StatusRequest{CWD: mainDir})
	if err != nil {
		t.Fatalf("Status (main, after): %v", err)
	}
	if !mainStatusAfter.Applied {
		t.Errorf("main checkout's applied ledger was cleared by unapplying in the worktree")
	}
	if mainStatusAfter.WorkspaceID != mainStatusBefore.WorkspaceID {
		t.Errorf("main checkout workspace ID changed: %s -> %s", mainStatusBefore.WorkspaceID, mainStatusAfter.WorkspaceID)
	}
}

// TestWorktree_DistinctWorkspaceIDs is a narrower unit-style check that two
// linked worktrees of the same repository, at the same relative path, do not
// collide on workspace ID with each other or with the main checkout — the
// property the applied-overlay ledger separation depends on.
func TestWorktree_DistinctWorkspaceIDs(t *testing.T) {
	mainDir := initGitRepo(t)
	runGit(t, mainDir, "commit", "--allow-empty", "-m", "init")

	eng := newIdentityEngine(t)

	worktreeA := filepath.Join(t.TempDir(), "wt-a")
	worktreeB := filepath.Join(mainDir, "..", "wt-b-nested")
	runGit(t, mainDir, "worktree", "add", worktreeA, "-b", "wt-a-branch")
	runGit(t, mainDir, "worktree", "add", worktreeB, "-b", "wt-b-branch")

	_, mainFP, mainRel, err := eng.DiscoverWorkspace(mainDir)
	if err != nil {
		t.Fatalf("DiscoverWorkspace (main): %v", err)
	}
	_, fpA, relA, err := eng.DiscoverWorkspace(worktreeA)
	if err != nil {
		t.Fatalf("DiscoverWorkspace (wt-a): %v", err)
	}
	_, fpB, relB, err := eng.DiscoverWorkspace(worktreeB)
	if err != nil {
		t.Fatalf("DiscoverWorkspace (wt-b): %v", err)
	}

	if mainRel != "." || relA != "." || relB != "." {
		t.Fatalf("expected all worktree roots at relative path %q: main=%q a=%q b=%q", ".", mainRel, relA, relB)
	}
	if mainFP == fpA || mainFP == fpB || fpA == fpB {
		t.Errorf("expected distinct fingerprints for main/wt-a/wt-b, got main=%s a=%s b=%s", mainFP, fpA, fpB)
	}
}
