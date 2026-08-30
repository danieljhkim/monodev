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
	tmpDir := t.TempDir()

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

	if _, err := EnsureRepoLocalRoot(tmpDir); err != nil {
		t.Fatalf("EnsureRepoLocalRoot should be idempotent: %v", err)
	}
}

func TestEnsureScopedPaths(t *testing.T) {
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
