package gitx

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// setupGitRepo creates a temporary git repository for testing.
func setupGitRepo(t *testing.T) string {
	t.Helper()

	tmpDir, err := os.MkdirTemp("", "gitx-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}

	// Initialize git repo
	cmd := exec.Command("git", "init")
	cmd.Dir = tmpDir
	if err := cmd.Run(); err != nil {
		_ = os.RemoveAll(tmpDir)
		t.Fatalf("failed to init git repo: %v", err)
	}

	// Configure git user for commits
	cmd = exec.Command("git", "config", "user.email", "test@example.com")
	cmd.Dir = tmpDir
	if err := cmd.Run(); err != nil {
		_ = os.RemoveAll(tmpDir)
		t.Fatalf("failed to configure git email: %v", err)
	}

	cmd = exec.Command("git", "config", "user.name", "Test User")
	cmd.Dir = tmpDir
	if err := cmd.Run(); err != nil {
		_ = os.RemoveAll(tmpDir)
		t.Fatalf("failed to configure git name: %v", err)
	}

	return tmpDir
}

// setupGitRepoWithRemote creates a git repo with a remote origin URL.
func setupGitRepoWithRemote(t *testing.T, remoteURL string) string {
	t.Helper()

	repoDir := setupGitRepo(t)

	// Add remote origin
	cmd := exec.Command("git", "remote", "add", "origin", remoteURL)
	cmd.Dir = repoDir
	if err := cmd.Run(); err != nil {
		_ = os.RemoveAll(repoDir)
		t.Fatalf("failed to add remote: %v", err)
	}

	return repoDir
}

func TestRealGitRepo_Discover(t *testing.T) {
	repo := NewRealGitRepo()

	t.Run("finds git repo from root", func(t *testing.T) {
		gitDir := setupGitRepo(t)
		defer func() { _ = os.RemoveAll(gitDir) }()

		root, err := repo.Discover(gitDir)
		if err != nil {
			t.Fatalf("Discover failed: %v", err)
		}

		if root != gitDir {
			t.Errorf("Discover returned wrong root: got %s, want %s", root, gitDir)
		}
	})

	t.Run("finds git repo from subdirectory", func(t *testing.T) {
		gitDir := setupGitRepo(t)
		defer func() { _ = os.RemoveAll(gitDir) }()

		// Create nested subdirectories
		subDir := filepath.Join(gitDir, "a", "b", "c")
		if err := os.MkdirAll(subDir, 0755); err != nil {
			t.Fatalf("failed to create subdirectories: %v", err)
		}

		root, err := repo.Discover(subDir)
		if err != nil {
			t.Fatalf("Discover from subdirectory failed: %v", err)
		}

		if root != gitDir {
			t.Errorf("Discover returned wrong root: got %s, want %s", root, gitDir)
		}
	})

	t.Run("returns error when not in git repo", func(t *testing.T) {
		tmpDir, err := os.MkdirTemp("", "non-git-*")
		if err != nil {
			t.Fatalf("failed to create temp dir: %v", err)
		}
		defer func() { _ = os.RemoveAll(tmpDir) }()

		_, err = repo.Discover(tmpDir)
		if err == nil {
			t.Error("Expected error when not in git repo, got nil")
		}
		if !strings.Contains(err.Error(), "not in a git repository") {
			t.Errorf("Expected 'not in a git repository' error, got: %v", err)
		}
	})

	t.Run("handles invalid path", func(t *testing.T) {
		_, err := repo.Discover("/nonexistent/path/that/does/not/exist")
		if err == nil {
			t.Error("Expected error for invalid path, got nil")
		}
	})
}

func TestRealGitRepo_CommonGitDir(t *testing.T) {
	repo := NewRealGitRepo()
	gitRoot := setupGitRepo(t)
	defer func() { _ = os.RemoveAll(gitRoot) }()

	if err := os.WriteFile(filepath.Join(gitRoot, "README.md"), []byte("initial\n"), 0600); err != nil {
		t.Fatalf("write initial file: %v", err)
	}
	for _, args := range [][]string{{"add", "README.md"}, {"commit", "-m", "initial"}} {
		cmd := exec.Command("git", args...)
		cmd.Dir = gitRoot
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, output)
		}
	}

	worktree := t.TempDir()
	cmd := exec.Command("git", "worktree", "add", "-b", "linked", worktree)
	cmd.Dir = gitRoot
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git worktree add: %v\n%s", err, output)
	}

	gitDir, err := repo.CommonGitDir(worktree)
	if err != nil {
		t.Fatalf("CommonGitDir: %v", err)
	}
	want := filepath.Join(gitRoot, ".git")
	gotInfo, err := os.Stat(gitDir)
	if err != nil {
		t.Fatalf("stat CommonGitDir result: %v", err)
	}
	wantInfo, err := os.Stat(want)
	if err != nil {
		t.Fatalf("stat expected git directory: %v", err)
	}
	if !os.SameFile(gotInfo, wantInfo) {
		t.Fatalf("CommonGitDir = %q, want %q", gitDir, want)
	}
}

func TestRealGitRepo_Fingerprint(t *testing.T) {
	repo := NewRealGitRepo()

	t.Run("generates fingerprint for repo with remote", func(t *testing.T) {
		remoteURL := "git@github.com:test/repo.git"
		gitDir := setupGitRepoWithRemote(t, remoteURL)
		defer func() { _ = os.RemoveAll(gitDir) }()

		fingerprint, err := repo.Fingerprint(gitDir)
		if err != nil {
			t.Fatalf("Fingerprint failed: %v", err)
		}

		if fingerprint == "" {
			t.Error("Fingerprint returned empty string")
		}

		// Fingerprint should be consistent
		fingerprint2, err := repo.Fingerprint(gitDir)
		if err != nil {
			t.Fatalf("Second Fingerprint failed: %v", err)
		}

		if fingerprint != fingerprint2 {
			t.Errorf("Fingerprint not consistent: %s vs %s", fingerprint, fingerprint2)
		}
	})

	t.Run("generates fingerprint for repo without remote", func(t *testing.T) {
		gitDir := setupGitRepo(t)
		defer func() { _ = os.RemoveAll(gitDir) }()

		fingerprint, err := repo.Fingerprint(gitDir)
		if err != nil {
			t.Fatalf("Fingerprint failed: %v", err)
		}

		if fingerprint == "" {
			t.Error("Fingerprint returned empty string for repo without remote")
		}
	})

	t.Run("different repos have different fingerprints", func(t *testing.T) {
		repo1 := setupGitRepoWithRemote(t, "git@github.com:test/repo1.git")
		defer func() { _ = os.RemoveAll(repo1) }()

		repo2 := setupGitRepoWithRemote(t, "git@github.com:test/repo2.git")
		defer func() { _ = os.RemoveAll(repo2) }()

		fp1, err := repo.Fingerprint(repo1)
		if err != nil {
			t.Fatalf("Fingerprint for repo1 failed: %v", err)
		}

		fp2, err := repo.Fingerprint(repo2)
		if err != nil {
			t.Fatalf("Fingerprint for repo2 failed: %v", err)
		}

		if fp1 == fp2 {
			t.Error("Different repos should have different fingerprints")
		}
	})

	t.Run("ssh and https remotes produce the same fingerprint", func(t *testing.T) {
		sshRepo := setupGitRepoWithRemote(t, "git@github.com:org/repo.git")
		defer func() { _ = os.RemoveAll(sshRepo) }()
		httpsRepo := setupGitRepoWithRemote(t, "https://github.com/org/repo.git")
		defer func() { _ = os.RemoveAll(httpsRepo) }()

		fpSSH, err := repo.Fingerprint(sshRepo)
		if err != nil {
			t.Fatalf("Fingerprint ssh: %v", err)
		}
		fpHTTPS, err := repo.Fingerprint(httpsRepo)
		if err != nil {
			t.Fatalf("Fingerprint https: %v", err)
		}
		if fpSSH != fpHTTPS {
			t.Errorf("SSH and HTTPS fingerprints differ: %s vs %s", fpSSH, fpHTTPS)
		}
	})

	t.Run("rewriting origin ssh to https keeps fingerprint", func(t *testing.T) {
		gitDir := setupGitRepoWithRemote(t, "git@github.com:org/repo.git")
		defer func() { _ = os.RemoveAll(gitDir) }()

		before, err := repo.Fingerprint(gitDir)
		if err != nil {
			t.Fatalf("Fingerprint before rewrite: %v", err)
		}
		cmd := exec.Command("git", "remote", "set-url", "origin", "https://github.com/org/repo.git")
		cmd.Dir = gitDir
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git remote set-url: %v\n%s", err, output)
		}
		after, err := repo.Fingerprint(gitDir)
		if err != nil {
			t.Fatalf("Fingerprint after rewrite: %v", err)
		}
		if before != after {
			t.Errorf("fingerprint changed after remote rewrite: %s vs %s", before, after)
		}
	})

	t.Run("clone path is not part of fingerprint", func(t *testing.T) {
		repo1 := setupGitRepoWithRemote(t, "https://github.com/org/same.git")
		defer func() { _ = os.RemoveAll(repo1) }()
		repo2 := setupGitRepoWithRemote(t, "https://github.com/org/same.git")
		defer func() { _ = os.RemoveAll(repo2) }()

		fp1, err := repo.Fingerprint(repo1)
		if err != nil {
			t.Fatalf("Fingerprint repo1: %v", err)
		}
		fp2, err := repo.Fingerprint(repo2)
		if err != nil {
			t.Fatalf("Fingerprint repo2: %v", err)
		}
		if fp1 != fp2 {
			t.Errorf("same remote at different paths produced %s and %s", fp1, fp2)
		}
	})

	t.Run("remote-less repo is stable after adding a remote", func(t *testing.T) {
		gitDir := setupGitRepo(t)
		defer func() { _ = os.RemoveAll(gitDir) }()

		first, err := repo.Fingerprint(gitDir)
		if err != nil {
			t.Fatalf("Fingerprint first: %v", err)
		}
		second, err := repo.Fingerprint(gitDir)
		if err != nil {
			t.Fatalf("Fingerprint second: %v", err)
		}
		if first != second {
			t.Errorf("remote-less fingerprint not stable: %s vs %s", first, second)
		}

		cmd := exec.Command("git", "remote", "add", "origin", "git@github.com:org/later.git")
		cmd.Dir = gitDir
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git remote add: %v\n%s", err, output)
		}
		afterRemote, err := repo.Fingerprint(gitDir)
		if err != nil {
			t.Fatalf("Fingerprint after remote add: %v", err)
		}
		if afterRemote != first {
			t.Errorf("adding a remote changed fingerprint: %s vs %s", first, afterRemote)
		}
	})

	t.Run("origin is preferred over other remotes", func(t *testing.T) {
		gitDir := setupGitRepo(t)
		defer func() { _ = os.RemoveAll(gitDir) }()

		cmd := exec.Command("git", "remote", "add", "upstream", "https://github.com/other/upstream.git")
		cmd.Dir = gitDir
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git remote add upstream: %v\n%s", err, output)
		}
		cmd = exec.Command("git", "remote", "add", "origin", "https://github.com/org/origin.git")
		cmd.Dir = gitDir
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git remote add origin: %v\n%s", err, output)
		}

		fp, err := repo.Fingerprint(gitDir)
		if err != nil {
			t.Fatalf("Fingerprint: %v", err)
		}
		want := HashFingerprint(fingerprintRemotePrefix + NormalizeRemoteURL("https://github.com/org/origin.git"))
		if fp != want {
			t.Errorf("Fingerprint = %s, want origin-based %s", fp, want)
		}
	})
}

func TestLegacyFingerprint(t *testing.T) {
	got := LegacyFingerprint("/abs/repo", "git@github.com:org/repo.git")
	want := HashFingerprint("/abs/repo|git@github.com:org/repo.git")
	if got != want {
		t.Errorf("LegacyFingerprint = %s, want %s", got, want)
	}
	if LegacyFingerprint("/abs/repo", "") != HashFingerprint("/abs/repo|unknown") {
		t.Error("empty remote should use the legacy unknown token")
	}
}

func TestEnsureDurableRepoID(t *testing.T) {
	gitDir := setupGitRepo(t)
	defer func() { _ = os.RemoveAll(gitDir) }()

	id1, err := EnsureDurableRepoID(gitDir)
	if err != nil {
		t.Fatalf("EnsureDurableRepoID: %v", err)
	}
	if id1 == "" {
		t.Fatal("expected non-empty repo id")
	}
	id2, err := EnsureDurableRepoID(gitDir)
	if err != nil {
		t.Fatalf("EnsureDurableRepoID second: %v", err)
	}
	if id1 != id2 {
		t.Errorf("durable id not stable: %s vs %s", id1, id2)
	}

	if err := os.MkdirAll(filepath.Join(gitDir, ".monodev"), 0700); err != nil {
		t.Fatalf("mkdir .monodev: %v", err)
	}
	id3, err := EnsureDurableRepoID(gitDir)
	if err != nil {
		t.Fatalf("EnsureDurableRepoID with .monodev: %v", err)
	}
	if id3 != id1 {
		t.Errorf("init copy changed id: %s vs %s", id1, id3)
	}
	data, err := os.ReadFile(filepath.Join(gitDir, ".monodev", "repo-id"))
	if err != nil {
		t.Fatalf("read .monodev/repo-id: %v", err)
	}
	if strings.TrimSpace(string(data)) != id1 {
		t.Errorf(".monodev/repo-id = %q, want %q", strings.TrimSpace(string(data)), id1)
	}
}

func TestRealGitRepo_RelPath(t *testing.T) {
	repo := NewRealGitRepo()

	t.Run("computes relative path for file in repo", func(t *testing.T) {
		gitDir := setupGitRepo(t)
		defer func() { _ = os.RemoveAll(gitDir) }()

		filePath := filepath.Join(gitDir, "src", "main.go")
		relPath, err := repo.RelPath(gitDir, filePath)
		if err != nil {
			t.Fatalf("RelPath failed: %v", err)
		}

		expected := filepath.Join("src", "main.go")
		if relPath != expected {
			t.Errorf("RelPath = %s, want %s", relPath, expected)
		}
	})

	t.Run("computes relative path for root file", func(t *testing.T) {
		gitDir := setupGitRepo(t)
		defer func() { _ = os.RemoveAll(gitDir) }()

		filePath := filepath.Join(gitDir, "README.md")
		relPath, err := repo.RelPath(gitDir, filePath)
		if err != nil {
			t.Fatalf("RelPath failed: %v", err)
		}

		if relPath != "README.md" {
			t.Errorf("RelPath = %s, want README.md", relPath)
		}
	})

	t.Run("returns error for path outside repo", func(t *testing.T) {
		gitDir := setupGitRepo(t)
		defer func() { _ = os.RemoveAll(gitDir) }()

		outsidePath := filepath.Join(filepath.Dir(gitDir), "outside.txt")
		_, err := repo.RelPath(gitDir, outsidePath)
		if err == nil {
			t.Error("Expected error for path outside repo, got nil")
		}
		if !strings.Contains(err.Error(), "outside repository") {
			t.Errorf("Expected 'outside repository' error, got: %v", err)
		}
	})

	t.Run("returns error for path with parent traversal", func(t *testing.T) {
		gitDir := setupGitRepo(t)
		defer func() { _ = os.RemoveAll(gitDir) }()

		// Path that would escape the repo through ..
		escapePath := filepath.Join(gitDir, "subdir", "..", "..", "escape.txt")
		_, err := repo.RelPath(gitDir, escapePath)
		if err == nil {
			t.Error("Expected error for escaping path, got nil")
		}
	})

	t.Run("handles nested directories correctly", func(t *testing.T) {
		gitDir := setupGitRepo(t)
		defer func() { _ = os.RemoveAll(gitDir) }()

		nestedPath := filepath.Join(gitDir, "a", "b", "c", "file.txt")
		relPath, err := repo.RelPath(gitDir, nestedPath)
		if err != nil {
			t.Fatalf("RelPath failed: %v", err)
		}

		expected := filepath.Join("a", "b", "c", "file.txt")
		if relPath != expected {
			t.Errorf("RelPath = %s, want %s", relPath, expected)
		}
	})
}

func TestRealGitRepo_GetFingerprintComponents(t *testing.T) {
	repo := NewRealGitRepo()

	t.Run("returns components for repo with remote", func(t *testing.T) {
		remoteURL := "git@github.com:test/repo.git"
		gitDir := setupGitRepoWithRemote(t, remoteURL)
		defer func() { _ = os.RemoveAll(gitDir) }()

		absPath, gitURL, err := repo.GetFingerprintComponents(gitDir)
		if err != nil {
			t.Fatalf("GetFingerprintComponents failed: %v", err)
		}

		if absPath == "" {
			t.Error("absPath should not be empty")
		}

		if gitURL != remoteURL {
			t.Errorf("gitURL = %s, want %s", gitURL, remoteURL)
		}

		// absPath should be absolute
		if !filepath.IsAbs(absPath) {
			t.Errorf("absPath should be absolute, got: %s", absPath)
		}
	})

	t.Run("returns components for repo without remote", func(t *testing.T) {
		gitDir := setupGitRepo(t)
		defer func() { _ = os.RemoveAll(gitDir) }()

		absPath, gitURL, err := repo.GetFingerprintComponents(gitDir)
		if err != nil {
			t.Fatalf("GetFingerprintComponents failed: %v", err)
		}

		if absPath == "" {
			t.Error("absPath should not be empty")
		}

		if gitURL != "" {
			t.Errorf("gitURL should be empty for repo without remote, got: %s", gitURL)
		}
	})
}

func TestFakeGitRepo_Discover(t *testing.T) {
	expectedRoot := "/fake/repo/root"
	expectedFingerprint := "fake-fingerprint-123"
	repo := NewFakeGitRepo(expectedRoot, expectedFingerprint)

	t.Run("returns predetermined root", func(t *testing.T) {
		root, err := repo.Discover("/any/path")
		if err != nil {
			t.Fatalf("Discover failed: %v", err)
		}

		if root != expectedRoot {
			t.Errorf("Discover = %s, want %s", root, expectedRoot)
		}
	})

	t.Run("returns error when configured", func(t *testing.T) {
		expectedErr := os.ErrNotExist
		repo.SetError(expectedErr)

		_, err := repo.Discover("/any/path")
		if err != expectedErr {
			t.Errorf("Expected error %v, got %v", expectedErr, err)
		}
	})
}

func TestFakeGitRepo_Fingerprint(t *testing.T) {
	expectedRoot := "/fake/repo/root"
	expectedFingerprint := "fake-fingerprint-abc"
	repo := NewFakeGitRepo(expectedRoot, expectedFingerprint)

	t.Run("returns predetermined fingerprint", func(t *testing.T) {
		fp, err := repo.Fingerprint("/any/root")
		if err != nil {
			t.Fatalf("Fingerprint failed: %v", err)
		}

		if fp != expectedFingerprint {
			t.Errorf("Fingerprint = %s, want %s", fp, expectedFingerprint)
		}
	})

	t.Run("returns error when configured", func(t *testing.T) {
		expectedErr := os.ErrInvalid
		repo.SetError(expectedErr)

		_, err := repo.Fingerprint("/any/root")
		if err != expectedErr {
			t.Errorf("Expected error %v, got %v", expectedErr, err)
		}
	})
}

func TestFakeGitRepo_RelPath(t *testing.T) {
	root := "/fake/repo"
	repo := NewFakeGitRepo(root, "fingerprint")

	t.Run("computes relative path correctly", func(t *testing.T) {
		absPath := filepath.Join(root, "src", "main.go")
		relPath, err := repo.RelPath(root, absPath)
		if err != nil {
			t.Fatalf("RelPath failed: %v", err)
		}

		expected := filepath.Join("src", "main.go")
		if relPath != expected {
			t.Errorf("RelPath = %s, want %s", relPath, expected)
		}
	})

	t.Run("returns error for path outside repo", func(t *testing.T) {
		outsidePath := "/other/path/file.txt"
		_, err := repo.RelPath(root, outsidePath)
		if err == nil {
			t.Error("Expected error for path outside repo, got nil")
		}
	})

	t.Run("returns error when configured", func(t *testing.T) {
		expectedErr := os.ErrPermission
		repo.SetError(expectedErr)

		_, err := repo.RelPath(root, "/any/path")
		if err != expectedErr {
			t.Errorf("Expected error %v, got %v", expectedErr, err)
		}
	})
}

func TestFakeGitRepo_GetFingerprintComponents(t *testing.T) {
	expectedAbsPath := "/fake/repo/abs/path"
	expectedGitURL := "git@github.com:fake/repo.git"
	repo := NewFakeGitRepoWithComponents("/fake/root", "fingerprint", expectedAbsPath, expectedGitURL)

	t.Run("returns predetermined components", func(t *testing.T) {
		absPath, gitURL, err := repo.GetFingerprintComponents("/any/root")
		if err != nil {
			t.Fatalf("GetFingerprintComponents failed: %v", err)
		}

		if absPath != expectedAbsPath {
			t.Errorf("absPath = %s, want %s", absPath, expectedAbsPath)
		}

		if gitURL != expectedGitURL {
			t.Errorf("gitURL = %s, want %s", gitURL, expectedGitURL)
		}
	})

	t.Run("returns error when configured", func(t *testing.T) {
		expectedErr := os.ErrClosed
		repo.SetError(expectedErr)

		_, _, err := repo.GetFingerprintComponents("/any/root")
		if err != expectedErr {
			t.Errorf("Expected error %v, got %v", expectedErr, err)
		}
	})
}

func TestNewFakeGitRepoWithComponents(t *testing.T) {
	root := "/test/root"
	fingerprint := "test-fp"
	absPath := "/test/abs"
	gitURL := "git@test.com:repo.git"

	repo := NewFakeGitRepoWithComponents(root, fingerprint, absPath, gitURL)

	t.Run("configures all components correctly", func(t *testing.T) {
		// Test Discover returns root
		discoveredRoot, err := repo.Discover("/any")
		if err != nil {
			t.Fatalf("Discover failed: %v", err)
		}
		if discoveredRoot != root {
			t.Errorf("Discover = %s, want %s", discoveredRoot, root)
		}

		// Test Fingerprint returns fingerprint
		fp, err := repo.Fingerprint(root)
		if err != nil {
			t.Fatalf("Fingerprint failed: %v", err)
		}
		if fp != fingerprint {
			t.Errorf("Fingerprint = %s, want %s", fp, fingerprint)
		}

		// Test GetFingerprintComponents returns components
		retAbsPath, retGitURL, err := repo.GetFingerprintComponents(root)
		if err != nil {
			t.Fatalf("GetFingerprintComponents failed: %v", err)
		}
		if retAbsPath != absPath {
			t.Errorf("absPath = %s, want %s", retAbsPath, absPath)
		}
		if retGitURL != gitURL {
			t.Errorf("gitURL = %s, want %s", retGitURL, gitURL)
		}
	})
}

func TestRealGitRepo_IsIgnored(t *testing.T) {
	repoDir := setupGitRepo(t)

	if err := os.WriteFile(filepath.Join(repoDir, ".gitignore"), []byte("*.log\n"), 0644); err != nil {
		t.Fatalf("failed to write .gitignore: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(repoDir, "sub"), 0755); err != nil {
		t.Fatalf("failed to create sub dir: %v", err)
	}
	for _, name := range []string{"sub/keep.txt", "sub/skip.log"} {
		if err := os.WriteFile(filepath.Join(repoDir, name), []byte("x"), 0644); err != nil {
			t.Fatalf("failed to write %s: %v", name, err)
		}
	}

	repo := NewRealGitRepo()
	ignored, err := repo.IsIgnored(repoDir, []string{
		filepath.Join("sub", "keep.txt"),
		filepath.Join("sub", "skip.log"),
	})
	if err != nil {
		t.Fatalf("IsIgnored failed: %v", err)
	}

	if ignored[filepath.Join("sub", "keep.txt")] {
		t.Errorf("keep.txt should not be ignored")
	}
	if !ignored[filepath.Join("sub", "skip.log")] {
		t.Errorf("skip.log should be ignored")
	}
}

func TestRealGitRepo_IsIgnored_EmptyInput(t *testing.T) {
	repoDir := setupGitRepo(t)

	repo := NewRealGitRepo()
	ignored, err := repo.IsIgnored(repoDir, nil)
	if err != nil {
		t.Fatalf("IsIgnored failed: %v", err)
	}
	if len(ignored) != 0 {
		t.Errorf("expected no ignored paths, got %v", ignored)
	}
}
