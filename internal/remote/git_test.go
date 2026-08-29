package remote

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

const testPersistenceBranch = "monodev/persist"

func runTestGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, out)
	}
	return string(out)
}

func configureTestGitIdentity(t *testing.T) {
	t.Helper()
	t.Setenv("GIT_AUTHOR_NAME", "Monodev Test")
	t.Setenv("GIT_AUTHOR_EMAIL", "monodev-test@example.com")
	t.Setenv("GIT_COMMITTER_NAME", "Monodev Test")
	t.Setenv("GIT_COMMITTER_EMAIL", "monodev-test@example.com")
}

func setupPersistenceTransport(t *testing.T) (*RealGitPersistence, string, string, string) {
	t.Helper()
	configureTestGitIdentity(t)

	base := t.TempDir()
	bareRemote := filepath.Join(base, "remote.git")
	if err := os.MkdirAll(bareRemote, 0755); err != nil {
		t.Fatal(err)
	}
	runTestGit(t, bareRemote, "init", "--bare")

	clientA := filepath.Join(base, "client-a")
	clientB := filepath.Join(base, "client-b")
	for _, root := range []string{clientA, clientB} {
		if err := os.MkdirAll(root, 0755); err != nil {
			t.Fatal(err)
		}
	}

	git := NewRealGitPersistence()
	ctx := context.Background()
	for _, root := range []string{clientA, clientB} {
		if err := git.EnsureRepo(ctx, root, testPersistenceBranch); err != nil {
			t.Fatalf("EnsureRepo(%s) failed: %v", root, err)
		}
		if err := git.SetRemote(ctx, root, "origin", bareRemote); err != nil {
			t.Fatalf("SetRemote(%s) failed: %v", root, err)
		}
	}

	writePersistenceVersion(t, clientA, "v1")
	if err := git.Commit(ctx, clientA, "v1", []string{filepath.Join(clientA, ".monodev", "persist")}); err != nil {
		t.Fatalf("commit v1 failed: %v", err)
	}
	if err := git.Push(ctx, clientA, "origin", testPersistenceBranch, false); err != nil {
		t.Fatalf("push v1 failed: %v", err)
	}
	if err := git.Fetch(ctx, clientB, "origin", testPersistenceBranch); err != nil {
		t.Fatalf("client B fetch v1 failed: %v", err)
	}
	if err := git.CheckoutFetched(ctx, clientB, testPersistenceBranch); err != nil {
		t.Fatalf("client B checkout v1 failed: %v", err)
	}

	return git, bareRemote, clientA, clientB
}

func writePersistenceVersion(t *testing.T, repoRoot, version string) {
	t.Helper()
	path := filepath.Join(repoRoot, ".monodev", "persist", "version.txt")
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(version), 0644); err != nil {
		t.Fatal(err)
	}
}

func readPersistenceVersion(t *testing.T, repoRoot string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(repoRoot, ".monodev", "persist", "version.txt"))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestValidateGitRef(t *testing.T) {
	tests := []struct {
		name    string
		ref     string
		refType string
		wantErr bool
	}{
		// Valid refs
		{name: "simple branch", ref: "main", refType: "branch", wantErr: false},
		{name: "branch with slash", ref: "feature/add-auth", refType: "branch", wantErr: false},
		{name: "branch with dots", ref: "release.1.0", refType: "branch", wantErr: false},
		{name: "branch with underscore", ref: "my_branch", refType: "branch", wantErr: false},
		{name: "branch with hyphen", ref: "my-branch", refType: "branch", wantErr: false},
		{name: "remote name", ref: "origin", refType: "remote", wantErr: false},
		{name: "monodev persist branch", ref: "monodev/persist", refType: "branch", wantErr: false},

		// Invalid refs
		{name: "empty", ref: "", refType: "branch", wantErr: true},
		{name: "starts with hyphen", ref: "-branch", refType: "branch", wantErr: true},
		{name: "contains space", ref: "my branch", refType: "branch", wantErr: true},
		{name: "contains semicolon", ref: "branch;rm -rf /", refType: "branch", wantErr: true},
		{name: "contains pipe", ref: "branch|cat /etc/passwd", refType: "branch", wantErr: true},
		{name: "contains backtick", ref: "branch`whoami`", refType: "branch", wantErr: true},
		{name: "contains dollar", ref: "branch$HOME", refType: "branch", wantErr: true},
		{name: "contains ampersand", ref: "branch&&echo", refType: "branch", wantErr: true},
		{name: "starts with dot", ref: ".hidden", refType: "branch", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateGitRef(tt.ref, tt.refType)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateGitRef(%q, %q) error = %v, wantErr %v", tt.ref, tt.refType, err, tt.wantErr)
			}
		})
	}
}

func TestRealGitPersistenceRunGitHonorsContextCancellation(t *testing.T) {
	binDir := filepath.Join(t.TempDir(), "bin")
	if err := os.MkdirAll(binDir, 0755); err != nil {
		t.Fatalf("failed to create fake git dir: %v", err)
	}

	fakeGit := filepath.Join(binDir, "git")
	if err := os.WriteFile(fakeGit, []byte("#!/bin/sh\nsleep 1\n"), 0755); err != nil {
		t.Fatalf("failed to write fake git: %v", err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	start := time.Now()
	_, err := NewRealGitPersistence().runGit(ctx, t.TempDir(), "status")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("runGit error = %v, want context deadline exceeded", err)
	}
	if elapsed := time.Since(start); elapsed > 750*time.Millisecond {
		t.Fatalf("runGit returned after %s, want prompt cancellation", elapsed)
	}
}

func TestRealGitPersistenceCheckoutFetchedRefusesDirtyWorkTree(t *testing.T) {
	git, _, clientA, clientB := setupPersistenceTransport(t)
	ctx := context.Background()

	writePersistenceVersion(t, clientA, "v2")
	if err := git.Commit(ctx, clientA, "v2", []string{filepath.Join(clientA, ".monodev", "persist")}); err != nil {
		t.Fatalf("commit v2 failed: %v", err)
	}
	if err := git.Push(ctx, clientA, "origin", testPersistenceBranch, false); err != nil {
		t.Fatalf("push v2 failed: %v", err)
	}

	writePersistenceVersion(t, clientB, "dirty local content")
	before, err := git.runGit(ctx, clientB, "rev-parse", "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	if err := git.Fetch(ctx, clientB, "origin", testPersistenceBranch); err != nil {
		t.Fatalf("fetch v2 failed: %v", err)
	}
	err = git.CheckoutFetched(ctx, clientB, testPersistenceBranch)
	if !errors.Is(err, ErrPersistenceWorkTreeDirty) {
		t.Fatalf("CheckoutFetched error = %v, want ErrPersistenceWorkTreeDirty", err)
	}
	after, revErr := git.runGit(ctx, clientB, "rev-parse", "HEAD")
	if revErr != nil {
		t.Fatal(revErr)
	}
	if after != before {
		t.Fatalf("HEAD changed from %s to %s after refused dirty checkout", before, after)
	}
	if got := readPersistenceVersion(t, clientB); got != "dirty local content" {
		t.Fatalf("dirty work tree was overwritten: got %q", got)
	}
}

func TestRealGitPersistenceCheckoutFetchedRefusesDivergedBranch(t *testing.T) {
	git, _, clientA, clientB := setupPersistenceTransport(t)
	ctx := context.Background()

	writePersistenceVersion(t, clientB, "local v2")
	if err := git.Commit(ctx, clientB, "local v2", []string{filepath.Join(clientB, ".monodev", "persist")}); err != nil {
		t.Fatalf("client B divergent commit failed: %v", err)
	}
	localHead, err := git.runGit(ctx, clientB, "rev-parse", "HEAD")
	if err != nil {
		t.Fatal(err)
	}

	writePersistenceVersion(t, clientA, "remote v2")
	if err := git.Commit(ctx, clientA, "remote v2", []string{filepath.Join(clientA, ".monodev", "persist")}); err != nil {
		t.Fatalf("client A v2 commit failed: %v", err)
	}
	if err := git.Push(ctx, clientA, "origin", testPersistenceBranch, false); err != nil {
		t.Fatalf("client A v2 push failed: %v", err)
	}
	if err := git.Fetch(ctx, clientB, "origin", testPersistenceBranch); err != nil {
		t.Fatalf("client B fetch v2 failed: %v", err)
	}

	err = git.CheckoutFetched(ctx, clientB, testPersistenceBranch)
	if !errors.Is(err, ErrPersistenceBranchDiverged) {
		t.Fatalf("CheckoutFetched error = %v, want ErrPersistenceBranchDiverged", err)
	}
	after, revErr := git.runGit(ctx, clientB, "rev-parse", "HEAD")
	if revErr != nil {
		t.Fatal(revErr)
	}
	if after != localHead {
		t.Fatalf("HEAD changed from %s to %s after refused divergent checkout", localHead, after)
	}
	if got := readPersistenceVersion(t, clientB); got != "local v2" {
		t.Fatalf("divergent work tree was overwritten: got %q", got)
	}
}
