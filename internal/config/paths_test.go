package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDefaultPaths(t *testing.T) {
	t.Run("returns paths based on home directory", func(t *testing.T) {
		// Clear MONODEV_ROOT env var
		oldRoot := os.Getenv("MONODEV_ROOT")
		defer func() {
			if err := os.Setenv("MONODEV_ROOT", oldRoot); err != nil {
				t.Errorf("failed to restore MONODEV_ROOT: %v", err)
			}
		}()
		if err := os.Unsetenv("MONODEV_ROOT"); err != nil {
			t.Fatalf("failed to unset MONODEV_ROOT: %v", err)
		}

		paths, err := DefaultPaths()
		if err != nil {
			t.Fatalf("DefaultPaths failed: %v", err)
		}

		if paths.Root == "" {
			t.Error("Root should not be empty")
		}

		// Verify paths are constructed correctly
		if paths.Stores != filepath.Join(paths.Root, "stores") {
			t.Errorf("Stores path incorrect: got %s", paths.Stores)
		}
		if paths.Workspaces != filepath.Join(paths.Root, "workspaces") {
			t.Errorf("Workspaces path incorrect: got %s", paths.Workspaces)
		}
		if paths.Config != filepath.Join(paths.Root, "config.yaml") {
			t.Errorf("Config path incorrect: got %s", paths.Config)
		}

		// Verify root ends with .monodev
		if filepath.Base(paths.Root) != ".monodev" {
			t.Errorf("Root should end with .monodev, got: %s", paths.Root)
		}
	})

	t.Run("respects MONODEV_ROOT environment variable (highest priority)", func(t *testing.T) {
		customRoot := "/custom/monodev/path"

		oldRoot := os.Getenv("MONODEV_ROOT")
		defer func() {
			if err := os.Setenv("MONODEV_ROOT", oldRoot); err != nil {
				t.Errorf("failed to restore MONODEV_ROOT: %v", err)
			}
		}()

		if err := os.Setenv("MONODEV_ROOT", customRoot); err != nil {
			t.Fatalf("failed to set MONODEV_ROOT: %v", err)
		}

		paths, err := DefaultPaths()
		if err != nil {
			t.Fatalf("DefaultPaths failed: %v", err)
		}

		if paths.Root != customRoot {
			t.Errorf("Expected root %s, got %s", customRoot, paths.Root)
		}

		// Verify other paths use the custom root
		if paths.Stores != filepath.Join(customRoot, "stores") {
			t.Errorf("Stores should be under custom root, got: %s", paths.Stores)
		}
		if paths.Workspaces != filepath.Join(customRoot, "workspaces") {
			t.Errorf("Workspaces should be under custom root, got: %s", paths.Workspaces)
		}
	})

	t.Run("uses repo-local .monodev when it exists", func(t *testing.T) {
		// Clear MONODEV_ROOT env var
		oldRoot := os.Getenv("MONODEV_ROOT")
		defer func() {
			if err := os.Setenv("MONODEV_ROOT", oldRoot); err != nil {
				t.Errorf("failed to restore MONODEV_ROOT: %v", err)
			}
		}()
		if err := os.Unsetenv("MONODEV_ROOT"); err != nil {
			t.Fatalf("failed to unset MONODEV_ROOT: %v", err)
		}

		// Create a temporary git repo with .monodev
		tmpDir, err := os.MkdirTemp("", "config-test-*")
		if err != nil {
			t.Fatalf("failed to create temp dir: %v", err)
		}
		defer func() {
			if err := os.RemoveAll(tmpDir); err != nil {
				t.Errorf("failed to remove temp dir: %v", err)
			}
		}()

		// Create .git directory
		gitDir := filepath.Join(tmpDir, ".git")
		if err := os.Mkdir(gitDir, 0755); err != nil {
			t.Fatalf("failed to create .git: %v", err)
		}

		// Create .monodev directory
		monodevDir := filepath.Join(tmpDir, ".monodev")
		if err := os.Mkdir(monodevDir, 0755); err != nil {
			t.Fatalf("failed to create .monodev: %v", err)
		}

		// Change to the temp directory
		oldWd, err := os.Getwd()
		if err != nil {
			t.Fatalf("failed to get cwd: %v", err)
		}
		defer func() {
			if err := os.Chdir(oldWd); err != nil {
				t.Errorf("failed to restore working directory: %v", err)
			}
		}()

		if err := os.Chdir(tmpDir); err != nil {
			t.Fatalf("failed to chdir: %v", err)
		}

		paths, err := DefaultPaths()
		if err != nil {
			t.Fatalf("DefaultPaths failed: %v", err)
		}

		// Should use repo-local .monodev
		// Use filepath.EvalSymlinks to handle /private prefix on macOS
		expectedRoot, _ := filepath.EvalSymlinks(monodevDir)
		actualRoot, _ := filepath.EvalSymlinks(paths.Root)
		if actualRoot != expectedRoot {
			t.Errorf("Expected repo-local .monodev at %s, got %s", expectedRoot, actualRoot)
		}
	})

	t.Run("prefers repo-local .monodev even when it does not exist yet", func(t *testing.T) {
		// Clear MONODEV_ROOT env var
		oldRoot := os.Getenv("MONODEV_ROOT")
		defer func() {
			if err := os.Setenv("MONODEV_ROOT", oldRoot); err != nil {
				t.Errorf("failed to restore MONODEV_ROOT: %v", err)
			}
		}()
		if err := os.Unsetenv("MONODEV_ROOT"); err != nil {
			t.Fatalf("failed to unset MONODEV_ROOT: %v", err)
		}

		// Create a temporary git repo WITHOUT .monodev
		tmpDir, err := os.MkdirTemp("", "config-test-*")
		if err != nil {
			t.Fatalf("failed to create temp dir: %v", err)
		}
		defer func() {
			if err := os.RemoveAll(tmpDir); err != nil {
				t.Errorf("failed to remove temp dir: %v", err)
			}
		}()

		// Create .git directory
		gitDir := filepath.Join(tmpDir, ".git")
		if err := os.Mkdir(gitDir, 0755); err != nil {
			t.Fatalf("failed to create .git: %v", err)
		}

		// Change to the temp directory
		oldWd, err := os.Getwd()
		if err != nil {
			t.Fatalf("failed to get cwd: %v", err)
		}
		defer func() {
			if err := os.Chdir(oldWd); err != nil {
				t.Errorf("failed to restore working directory: %v", err)
			}
		}()

		if err := os.Chdir(tmpDir); err != nil {
			t.Fatalf("failed to chdir: %v", err)
		}

		paths, err := DefaultPaths()
		if err != nil {
			t.Fatalf("DefaultPaths failed: %v", err)
		}

		wd, err := os.Getwd()
		if err != nil {
			t.Fatalf("getwd: %v", err)
		}
		expectedRoot := filepath.Join(wd, RepoLocalDirName)
		if paths.Root != expectedRoot {
			t.Errorf("Expected repo-local .monodev at %s, got %s", expectedRoot, paths.Root)
		}
	})

	t.Run("MONODEV_ROOT takes precedence over repo-local", func(t *testing.T) {
		customRoot := "/custom/monodev/path"

		oldRoot := os.Getenv("MONODEV_ROOT")
		defer func() {
			if err := os.Setenv("MONODEV_ROOT", oldRoot); err != nil {
				t.Errorf("failed to restore MONODEV_ROOT: %v", err)
			}
		}()
		if err := os.Setenv("MONODEV_ROOT", customRoot); err != nil {
			t.Fatalf("failed to set MONODEV_ROOT: %v", err)
		}

		// Create a temporary git repo with .monodev
		tmpDir, err := os.MkdirTemp("", "config-test-*")
		if err != nil {
			t.Fatalf("failed to create temp dir: %v", err)
		}
		defer func() {
			if err := os.RemoveAll(tmpDir); err != nil {
				t.Errorf("failed to remove temp dir: %v", err)
			}
		}()

		// Create .git directory
		gitDir := filepath.Join(tmpDir, ".git")
		if err := os.Mkdir(gitDir, 0755); err != nil {
			t.Fatalf("failed to create .git: %v", err)
		}

		// Create .monodev directory
		monodevDir := filepath.Join(tmpDir, ".monodev")
		if err := os.Mkdir(monodevDir, 0755); err != nil {
			t.Fatalf("failed to create .monodev: %v", err)
		}

		// Change to the temp directory
		oldWd, err := os.Getwd()
		if err != nil {
			t.Fatalf("failed to get cwd: %v", err)
		}
		defer func() {
			if err := os.Chdir(oldWd); err != nil {
				t.Errorf("failed to restore working directory: %v", err)
			}
		}()

		if err := os.Chdir(tmpDir); err != nil {
			t.Fatalf("failed to chdir: %v", err)
		}

		paths, err := DefaultPaths()
		if err != nil {
			t.Fatalf("DefaultPaths failed: %v", err)
		}

		// MONODEV_ROOT should take precedence
		if paths.Root != customRoot {
			t.Errorf("Expected MONODEV_ROOT %s to take precedence, got %s", customRoot, paths.Root)
		}
	})
}

func TestNewScopedPaths(t *testing.T) {
	t.Run("always resolves global paths", func(t *testing.T) {
		oldRoot := os.Getenv("MONODEV_ROOT")
		defer func() {
			if oldRoot != "" {
				if err := os.Setenv("MONODEV_ROOT", oldRoot); err != nil {
					t.Errorf("failed to restore MONODEV_ROOT: %v", err)
				}
			} else {
				if err := os.Unsetenv("MONODEV_ROOT"); err != nil {
					t.Errorf("failed to clear MONODEV_ROOT: %v", err)
				}
			}
		}()
		if err := os.Unsetenv("MONODEV_ROOT"); err != nil {
			t.Fatalf("failed to unset MONODEV_ROOT: %v", err)
		}

		sp, err := NewScopedPaths()
		if err != nil {
			t.Fatalf("NewScopedPaths failed: %v", err)
		}

		if sp.Global == nil {
			t.Fatal("Global paths should always be set")
		}

		home, _ := os.UserHomeDir()
		expected := filepath.Join(home, ".monodev")
		if sp.Global.Root != expected {
			t.Errorf("expected global root %s, got %s", expected, sp.Global.Root)
		}
	})

	t.Run("respects MONODEV_ROOT for global", func(t *testing.T) {
		customRoot := "/custom/monodev/root"
		oldRoot := os.Getenv("MONODEV_ROOT")
		defer func() {
			if oldRoot != "" {
				if err := os.Setenv("MONODEV_ROOT", oldRoot); err != nil {
					t.Errorf("failed to restore MONODEV_ROOT: %v", err)
				}
			} else {
				if err := os.Unsetenv("MONODEV_ROOT"); err != nil {
					t.Errorf("failed to clear MONODEV_ROOT: %v", err)
				}
			}
		}()
		if err := os.Setenv("MONODEV_ROOT", customRoot); err != nil {
			t.Fatalf("failed to set MONODEV_ROOT: %v", err)
		}

		sp, err := NewScopedPaths()
		if err != nil {
			t.Fatalf("NewScopedPaths failed: %v", err)
		}

		if sp.Global.Root != customRoot {
			t.Errorf("expected global root %s, got %s", customRoot, sp.Global.Root)
		}
	})

	t.Run("sets component when in repo with .monodev", func(t *testing.T) {
		oldRoot := os.Getenv("MONODEV_ROOT")
		defer func() {
			if oldRoot != "" {
				if err := os.Setenv("MONODEV_ROOT", oldRoot); err != nil {
					t.Errorf("failed to restore MONODEV_ROOT: %v", err)
				}
			} else {
				if err := os.Unsetenv("MONODEV_ROOT"); err != nil {
					t.Errorf("failed to clear MONODEV_ROOT: %v", err)
				}
			}
		}()
		if err := os.Unsetenv("MONODEV_ROOT"); err != nil {
			t.Fatalf("failed to unset MONODEV_ROOT: %v", err)
		}

		tmpDir, err := os.MkdirTemp("", "scoped-paths-test-*")
		if err != nil {
			t.Fatalf("failed to create temp dir: %v", err)
		}
		defer func() {
			if err := os.RemoveAll(tmpDir); err != nil {
				t.Errorf("failed to remove temp dir: %v", err)
			}
		}()

		if err := os.Mkdir(filepath.Join(tmpDir, ".git"), 0755); err != nil {
			t.Fatalf("failed to create .git: %v", err)
		}
		if err := os.Mkdir(filepath.Join(tmpDir, ".monodev"), 0755); err != nil {
			t.Fatalf("failed to create .monodev: %v", err)
		}

		oldWd, err := os.Getwd()
		if err != nil {
			t.Fatalf("failed to get cwd: %v", err)
		}
		defer func() {
			if err := os.Chdir(oldWd); err != nil {
				t.Errorf("failed to restore working directory: %v", err)
			}
		}()
		if err := os.Chdir(tmpDir); err != nil {
			t.Fatalf("failed to chdir: %v", err)
		}

		sp, err := NewScopedPaths()
		if err != nil {
			t.Fatalf("NewScopedPaths failed: %v", err)
		}

		if !sp.HasRepoContext {
			t.Error("expected HasRepoContext to be true")
		}
		if sp.Component == nil {
			t.Fatal("expected Component paths to be set")
		}

		expectedRoot, _ := filepath.EvalSymlinks(filepath.Join(tmpDir, ".monodev"))
		actualRoot, _ := filepath.EvalSymlinks(sp.Component.Root)
		if actualRoot != expectedRoot {
			t.Errorf("expected component root %s, got %s", expectedRoot, actualRoot)
		}
	})

	t.Run("does not duplicate component when MONODEV_ROOT is repo-local .monodev", func(t *testing.T) {
		oldRoot := os.Getenv("MONODEV_ROOT")
		defer func() {
			if oldRoot != "" {
				if err := os.Setenv("MONODEV_ROOT", oldRoot); err != nil {
					t.Errorf("failed to restore MONODEV_ROOT: %v", err)
				}
			} else {
				if err := os.Unsetenv("MONODEV_ROOT"); err != nil {
					t.Errorf("failed to clear MONODEV_ROOT: %v", err)
				}
			}
		}()

		tmpDir, err := os.MkdirTemp("", "scoped-paths-test-*")
		if err != nil {
			t.Fatalf("failed to create temp dir: %v", err)
		}
		defer func() {
			if err := os.RemoveAll(tmpDir); err != nil {
				t.Errorf("failed to remove temp dir: %v", err)
			}
		}()

		if err := os.Mkdir(filepath.Join(tmpDir, ".git"), 0755); err != nil {
			t.Fatalf("failed to create .git: %v", err)
		}
		monodevDir := filepath.Join(tmpDir, ".monodev")
		if err := os.Mkdir(monodevDir, 0755); err != nil {
			t.Fatalf("failed to create .monodev: %v", err)
		}
		if err := os.Setenv("MONODEV_ROOT", monodevDir); err != nil {
			t.Fatalf("failed to set MONODEV_ROOT: %v", err)
		}

		oldWd, err := os.Getwd()
		if err != nil {
			t.Fatalf("failed to get cwd: %v", err)
		}
		defer func() {
			if err := os.Chdir(oldWd); err != nil {
				t.Errorf("failed to restore working directory: %v", err)
			}
		}()
		if err := os.Chdir(tmpDir); err != nil {
			t.Fatalf("failed to chdir: %v", err)
		}

		sp, err := NewScopedPaths()
		if err != nil {
			t.Fatalf("NewScopedPaths failed: %v", err)
		}

		if sp.Component != nil {
			t.Fatalf("expected Component to be nil when global and component roots match, got %s", sp.Component.Root)
		}
		if !sp.HasRepoContext {
			t.Error("expected HasRepoContext to stay true when repo-local .monodev exists")
		}
	})

	t.Run("no component when repo has no .monodev", func(t *testing.T) {
		oldRoot := os.Getenv("MONODEV_ROOT")
		defer func() {
			if oldRoot != "" {
				if err := os.Setenv("MONODEV_ROOT", oldRoot); err != nil {
					t.Errorf("failed to restore MONODEV_ROOT: %v", err)
				}
			} else {
				if err := os.Unsetenv("MONODEV_ROOT"); err != nil {
					t.Errorf("failed to clear MONODEV_ROOT: %v", err)
				}
			}
		}()
		if err := os.Unsetenv("MONODEV_ROOT"); err != nil {
			t.Fatalf("failed to unset MONODEV_ROOT: %v", err)
		}

		tmpDir, err := os.MkdirTemp("", "scoped-paths-test-*")
		if err != nil {
			t.Fatalf("failed to create temp dir: %v", err)
		}
		defer func() {
			if err := os.RemoveAll(tmpDir); err != nil {
				t.Errorf("failed to remove temp dir: %v", err)
			}
		}()

		if err := os.Mkdir(filepath.Join(tmpDir, ".git"), 0755); err != nil {
			t.Fatalf("failed to create .git: %v", err)
		}

		oldWd, err := os.Getwd()
		if err != nil {
			t.Fatalf("failed to get cwd: %v", err)
		}
		defer func() {
			if err := os.Chdir(oldWd); err != nil {
				t.Errorf("failed to restore working directory: %v", err)
			}
		}()
		if err := os.Chdir(tmpDir); err != nil {
			t.Fatalf("failed to chdir: %v", err)
		}

		sp, err := NewScopedPaths()
		if err != nil {
			t.Fatalf("NewScopedPaths failed: %v", err)
		}

		if sp.HasRepoContext {
			t.Error("expected HasRepoContext to be false")
		}
		if sp.Component != nil {
			t.Error("expected Component to be nil")
		}
	})
}

func TestPaths_EnsureDirectories(t *testing.T) {
	t.Run("creates all necessary directories", func(t *testing.T) {
		tmpDir, err := os.MkdirTemp("", "config-test-*")
		if err != nil {
			t.Fatalf("failed to create temp dir: %v", err)
		}
		defer func() {
			if err := os.RemoveAll(tmpDir); err != nil {
				t.Errorf("failed to remove temp dir: %v", err)
			}
		}()

		paths := &Paths{
			Root:       filepath.Join(tmpDir, "monodev"),
			Stores:     filepath.Join(tmpDir, "monodev", "stores"),
			Workspaces: filepath.Join(tmpDir, "monodev", "workspaces"),
			Config:     filepath.Join(tmpDir, "monodev", "config.yaml"),
		}

		err = paths.EnsureDirectories()
		if err != nil {
			t.Fatalf("EnsureDirectories failed: %v", err)
		}

		// Verify directories exist
		dirs := []string{paths.Root, paths.Stores, paths.Workspaces}
		for _, dir := range dirs {
			info, err := os.Stat(dir)
			if os.IsNotExist(err) {
				t.Errorf("Directory %s was not created", dir)
				continue
			}
			if err != nil {
				t.Fatalf("failed to stat directory %s: %v", dir, err)
			}
			if got := info.Mode().Perm(); got != 0700 {
				t.Errorf("directory %s mode = %04o, want 0700", dir, got)
			}
		}
	})

	t.Run("succeeds if directories already exist", func(t *testing.T) {
		tmpDir, err := os.MkdirTemp("", "config-test-*")
		if err != nil {
			t.Fatalf("failed to create temp dir: %v", err)
		}
		defer func() {
			if err := os.RemoveAll(tmpDir); err != nil {
				t.Errorf("failed to remove temp dir: %v", err)
			}
		}()

		paths := &Paths{
			Root:       filepath.Join(tmpDir, "monodev"),
			Stores:     filepath.Join(tmpDir, "monodev", "stores"),
			Workspaces: filepath.Join(tmpDir, "monodev", "workspaces"),
			Config:     filepath.Join(tmpDir, "monodev", "config.yaml"),
		}

		// Create directories first
		if err := os.MkdirAll(paths.Root, 0755); err != nil {
			t.Fatalf("failed to pre-create root: %v", err)
		}
		if err := os.MkdirAll(paths.Stores, 0755); err != nil {
			t.Fatalf("failed to pre-create stores: %v", err)
		}
		if err := os.MkdirAll(paths.Workspaces, 0755); err != nil {
			t.Fatalf("failed to pre-create workspaces: %v", err)
		}

		// Should not fail
		err = paths.EnsureDirectories()
		if err != nil {
			t.Errorf("EnsureDirectories should succeed with existing dirs: %v", err)
		}
	})

	t.Run("creates nested directories", func(t *testing.T) {
		tmpDir, err := os.MkdirTemp("", "config-test-*")
		if err != nil {
			t.Fatalf("failed to create temp dir: %v", err)
		}
		defer func() {
			if err := os.RemoveAll(tmpDir); err != nil {
				t.Errorf("failed to remove temp dir: %v", err)
			}
		}()

		// Use deeply nested paths
		deepRoot := filepath.Join(tmpDir, "a", "b", "c", "monodev")
		paths := &Paths{
			Root:       deepRoot,
			Stores:     filepath.Join(deepRoot, "stores"),
			Workspaces: filepath.Join(deepRoot, "workspaces"),
			Config:     filepath.Join(deepRoot, "config.yaml"),
		}

		err = paths.EnsureDirectories()
		if err != nil {
			t.Fatalf("EnsureDirectories failed for nested path: %v", err)
		}

		// Verify nested directories exist
		if _, err := os.Stat(deepRoot); os.IsNotExist(err) {
			t.Error("Nested root directory was not created")
		}
	})
}

func TestEnsureRepoLocalRoot(t *testing.T) {
	// macOS temporary directories can use /var -> /private/var. Pass the
	// physical repository path, as automatic initialization resolves it.
	tmpDir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	path, err := EnsureRepoLocalRoot(tmpDir)
	if err != nil {
		t.Fatalf("EnsureRepoLocalRoot: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat .monodev: %v", err)
	}
	if !info.IsDir() {
		t.Fatal("expected .monodev to be a directory")
	}
	if got := info.Mode().Perm(); got != 0700 {
		t.Errorf(".monodev mode = %04o, want 0700", got)
	}

	for _, name := range []string{"stores", "workspaces"} {
		dir := filepath.Join(path, name)
		info, err := os.Stat(dir)
		if err != nil {
			t.Fatalf("stat %s: %v", name, err)
		}
		if got := info.Mode().Perm(); got != 0700 {
			t.Errorf("%s mode = %04o, want 0700", name, got)
		}
	}

	data, err := os.ReadFile(filepath.Join(path, ".gitignore"))
	if err != nil {
		t.Fatalf("read .gitignore: %v", err)
	}
	if string(data) != RepoLocalGitignore {
		t.Errorf(".gitignore = %q, want %q", data, RepoLocalGitignore)
	}
	info, err = os.Stat(filepath.Join(path, ".gitignore"))
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0600 {
		t.Errorf(".gitignore mode = %04o, want 0600", got)
	}
	// Reinitialization replaces stale contents without leaving a suffix and
	// restores private ignore-file permissions.
	if err := os.WriteFile(filepath.Join(path, ".gitignore"), []byte("stale contents longer than the expected ignore file\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(filepath.Join(path, ".gitignore"), 0644); err != nil {
		t.Fatal(err)
	}

	if _, err := EnsureRepoLocalRoot(tmpDir); err != nil {
		t.Fatalf("EnsureRepoLocalRoot should be idempotent: %v", err)
	}
	data, err = os.ReadFile(filepath.Join(path, ".gitignore"))
	if err != nil || string(data) != RepoLocalGitignore {
		t.Fatalf("idempotent .gitignore = %q, error = %v", data, err)
	}
	info, err = os.Stat(filepath.Join(path, ".gitignore"))
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("idempotent .gitignore permissions: info = %v, error = %v", info, err)
	}
}

func TestEnsureRepoLocalRootRejectsUnsafePaths(t *testing.T) {
	for _, fixture := range []string{"root", "dangling root", "stores", "workspaces", "gitignore", "dangling gitignore", "hard-linked gitignore", "directory gitignore", "repository", "repository ancestor"} {
		t.Run(fixture, func(t *testing.T) {
			base, err := filepath.EvalSymlinks(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			repo := filepath.Join(base, "repo")
			outside := filepath.Join(base, "outside")
			for _, dir := range []string{repo, outside} {
				if err := os.Mkdir(dir, 0700); err != nil {
					t.Fatal(err)
				}
			}
			const sentinel = "KEEP USER DATA\n\x00unchanged\n"
			victim := filepath.Join(outside, "user-data")
			if err := os.WriteFile(victim, []byte(sentinel), 0644); err != nil {
				t.Fatal(err)
			}
			if err := os.Chmod(victim, 0644); err != nil {
				t.Fatal(err)
			}
			var linkPath, linkTarget string
			link := func(target, path string) {
				t.Helper()
				if err := os.Symlink(target, path); err != nil {
					t.Fatal(err)
				}
				linkPath, linkTarget = path, target
			}
			root := filepath.Join(repo, RepoLocalDirName)
			if fixture != "root" && fixture != "dangling root" && fixture != "repository" && fixture != "repository ancestor" {
				if err := os.Mkdir(root, 0700); err != nil {
					t.Fatal(err)
				}
			}
			switch fixture {
			case "root":
				link(outside, root)
			case "dangling root":
				link(filepath.Join(outside, "missing"), root)
			case "stores", "workspaces":
				link(outside, filepath.Join(root, fixture))
			case "gitignore":
				link(victim, filepath.Join(root, ".gitignore"))
			case "dangling gitignore":
				link(filepath.Join(outside, "missing"), filepath.Join(root, ".gitignore"))
			case "hard-linked gitignore":
				if err := os.Link(victim, filepath.Join(root, ".gitignore")); err != nil {
					t.Fatal(err)
				}
			case "directory gitignore":
				if err := os.Mkdir(filepath.Join(root, ".gitignore"), 0700); err != nil {
					t.Fatal(err)
				}
			case "repository":
				alias := filepath.Join(base, "alias")
				link(repo, alias)
				repo = alias
			case "repository ancestor":
				alias := filepath.Join(base, "alias")
				link(base, alias)
				repo = filepath.Join(alias, "repo")
			}

			if path, err := EnsureRepoLocalRoot(repo); err == nil || path != "" {
				t.Fatalf("unsafe initialization returned path %q, error %v", path, err)
			}
			if linkPath != "" {
				if target, err := os.Readlink(linkPath); err != nil || target != linkTarget {
					t.Fatalf("unsafe link modified: target %q, error %v", target, err)
				}
			}
			if fixture == "repository" || fixture == "repository ancestor" {
				if _, err := os.Lstat(root); !os.IsNotExist(err) {
					t.Fatalf("state root created through repository link: %v", err)
				}
			}
			data, err := os.ReadFile(victim)
			if err != nil || string(data) != sentinel {
				t.Fatalf("outside sentinel changed: %q, error %v", data, err)
			}
			info, err := os.Stat(victim)
			if err != nil || info.Mode().Perm() != 0644 {
				t.Fatalf("outside sentinel permissions changed: info %v, error %v", info, err)
			}
			entries, err := os.ReadDir(outside)
			if err != nil || len(entries) != 1 || entries[0].Name() != "user-data" {
				t.Fatalf("outside directory changed: %v, error %v", entries, err)
			}
			// No sibling state paths should be created before rejecting a link.
			for _, name := range []string{"stores", "workspaces", ".gitignore"} {
				if fixture == name || (name == ".gitignore" && strings.HasSuffix(fixture, "gitignore")) {
					continue
				}
				if _, err := os.Lstat(filepath.Join(root, name)); !os.IsNotExist(err) {
					t.Fatalf("unexpected state path %s after rejection: %v", name, err)
				}
			}
		})
	}
}

func TestEnsureScopedPaths(t *testing.T) {
	t.Run("initializes from a current-directory alias", func(t *testing.T) {
		t.Setenv(EnvRoot, "")
		base := t.TempDir()
		repo := filepath.Join(base, "repo")
		if err := os.MkdirAll(filepath.Join(repo, ".git"), 0700); err != nil {
			t.Fatal(err)
		}
		alias := filepath.Join(base, "alias")
		if err := os.Symlink(repo, alias); err != nil {
			t.Fatal(err)
		}
		oldWd, err := os.Getwd()
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			if err := os.Chdir(oldWd); err != nil {
				t.Error(err)
			}
		})
		if err := os.Chdir(alias); err != nil {
			t.Fatal(err)
		}
		t.Setenv("PWD", alias)
		if _, err := EnsureScopedPaths(); err != nil {
			t.Fatalf("EnsureScopedPaths from current-directory alias: %v", err)
		}
		data, err := os.ReadFile(filepath.Join(repo, RepoLocalDirName, ".gitignore"))
		if err != nil || string(data) != RepoLocalGitignore {
			t.Fatalf("physical repo .gitignore = %q, error %v", data, err)
		}
	})

	for _, customRoot := range []bool{false, true} {
		name := "refuses automatic initialization through a leaf link"
		if customRoot {
			name = "custom root skips unsafe repo-local initialization"
		}
		t.Run(name, func(t *testing.T) {
			t.Setenv(EnvRoot, "")
			if customRoot {
				t.Setenv(EnvRoot, t.TempDir())
			}
			repo := t.TempDir()
			if err := os.Mkdir(filepath.Join(repo, ".git"), 0700); err != nil {
				t.Fatal(err)
			}
			root := filepath.Join(repo, RepoLocalDirName)
			if err := os.Mkdir(root, 0700); err != nil {
				t.Fatal(err)
			}
			victim := filepath.Join(t.TempDir(), "user-data")
			const sentinel = "KEEP USER DATA\n"
			if err := os.WriteFile(victim, []byte(sentinel), 0600); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(victim, filepath.Join(root, ".gitignore")); err != nil {
				t.Fatal(err)
			}
			oldWd, err := os.Getwd()
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if err := os.Chdir(oldWd); err != nil {
					t.Error(err)
				}
			})
			if err := os.Chdir(repo); err != nil {
				t.Fatal(err)
			}
			sp, err := EnsureScopedPaths()
			if customRoot {
				if err != nil || sp.Global.Root != os.Getenv(EnvRoot) {
					t.Fatalf("custom root resolution: paths %v, error %v", sp, err)
				}
			} else if err == nil || !strings.Contains(err.Error(), "symlink") {
				t.Fatalf("expected symlink rejection, got paths %v, error %v", sp, err)
			}
			data, err := os.ReadFile(victim)
			if err != nil || string(data) != sentinel {
				t.Fatalf("outside sentinel changed: %q, error %v", data, err)
			}
			for _, name := range []string{"stores", "workspaces"} {
				if _, err := os.Lstat(filepath.Join(root, name)); !os.IsNotExist(err) {
					t.Fatalf("unexpected %s after skipping/refusing initialization: %v", name, err)
				}
			}
		})
	}

	t.Run("auto-creates repo-local in a git repo", func(t *testing.T) {
		t.Setenv(EnvRoot, "")
		tmpDir := t.TempDir()
		if err := os.Mkdir(filepath.Join(tmpDir, ".git"), 0755); err != nil {
			t.Fatal(err)
		}
		t.Setenv("HOME", t.TempDir())

		oldWd, err := os.Getwd()
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = os.Chdir(oldWd) }()
		if err := os.Chdir(tmpDir); err != nil {
			t.Fatal(err)
		}

		sp, err := EnsureScopedPaths()
		if err != nil {
			t.Fatalf("EnsureScopedPaths: %v", err)
		}
		if !sp.HasRepoContext {
			t.Fatal("expected HasRepoContext after auto-create")
		}
		if sp.Component == nil {
			t.Fatal("expected Component paths after auto-create")
		}

		info, err := os.Stat(filepath.Join(tmpDir, RepoLocalDirName))
		if err != nil {
			t.Fatalf("expected auto-created .monodev: %v", err)
		}
		if got := info.Mode().Perm(); got != 0700 {
			t.Errorf(".monodev mode = %04o, want 0700", got)
		}
	})

	t.Run("fails outside a git repository", func(t *testing.T) {
		t.Setenv(EnvRoot, "")
		t.Setenv("HOME", t.TempDir())
		tmpDir := t.TempDir()

		oldWd, err := os.Getwd()
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = os.Chdir(oldWd) }()
		if err := os.Chdir(tmpDir); err != nil {
			t.Fatal(err)
		}

		_, err = EnsureScopedPaths()
		if err == nil {
			t.Fatal("expected error outside a git repository")
		}
		got := err.Error()
		if !strings.Contains(got, "not in a git repository") ||
			!strings.Contains(got, "monodev init must be run inside a git repository") {
			t.Errorf("error = %q, want init-style not-in-git-repository message", got)
		}
	})

	t.Run("skips auto-create when MONODEV_ROOT is set", func(t *testing.T) {
		customRoot := t.TempDir()
		t.Setenv(EnvRoot, customRoot)
		tmpDir := t.TempDir()
		if err := os.Mkdir(filepath.Join(tmpDir, ".git"), 0755); err != nil {
			t.Fatal(err)
		}

		oldWd, err := os.Getwd()
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = os.Chdir(oldWd) }()
		if err := os.Chdir(tmpDir); err != nil {
			t.Fatal(err)
		}

		sp, err := EnsureScopedPaths()
		if err != nil {
			t.Fatalf("EnsureScopedPaths: %v", err)
		}
		if sp.Global.Root != customRoot {
			t.Errorf("Global.Root = %s, want %s", sp.Global.Root, customRoot)
		}
		if sp.Component != nil {
			t.Fatalf("did not expect Component when MONODEV_ROOT is set, got %s", sp.Component.Root)
		}
		if _, err := os.Stat(filepath.Join(tmpDir, RepoLocalDirName)); !os.IsNotExist(err) {
			t.Fatal("MONODEV_ROOT should skip auto-creating repo-local .monodev")
		}
	})
}
