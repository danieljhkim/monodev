package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultPaths(t *testing.T) {
	t.Run("returns paths based on home directory", func(t *testing.T) {
		// Clear MONODEV_ROOT env var
		oldRoot := os.Getenv("MONODEV_ROOT")
		defer os.Setenv("MONODEV_ROOT", oldRoot)
		os.Unsetenv("MONODEV_ROOT")

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
		defer os.Setenv("MONODEV_ROOT", oldRoot)

		os.Setenv("MONODEV_ROOT", customRoot)

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
		defer os.Setenv("MONODEV_ROOT", oldRoot)
		os.Unsetenv("MONODEV_ROOT")

		// Create a temporary git repo with .monodev
		tmpDir, err := os.MkdirTemp("", "config-test-*")
		if err != nil {
			t.Fatalf("failed to create temp dir: %v", err)
		}
		defer os.RemoveAll(tmpDir)

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
		defer os.Chdir(oldWd)

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

	t.Run("falls back to global ~/.monodev when no repo-local exists", func(t *testing.T) {
		// Clear MONODEV_ROOT env var
		oldRoot := os.Getenv("MONODEV_ROOT")
		defer os.Setenv("MONODEV_ROOT", oldRoot)
		os.Unsetenv("MONODEV_ROOT")

		// Create a temporary git repo WITHOUT .monodev
		tmpDir, err := os.MkdirTemp("", "config-test-*")
		if err != nil {
			t.Fatalf("failed to create temp dir: %v", err)
		}
		defer os.RemoveAll(tmpDir)

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
		defer os.Chdir(oldWd)

		if err := os.Chdir(tmpDir); err != nil {
			t.Fatalf("failed to chdir: %v", err)
		}

		paths, err := DefaultPaths()
		if err != nil {
			t.Fatalf("DefaultPaths failed: %v", err)
		}

		// Should use global ~/.monodev
		home, _ := os.UserHomeDir()
		expectedRoot := filepath.Join(home, ".monodev")
		if paths.Root != expectedRoot {
			t.Errorf("Expected global .monodev at %s, got %s", expectedRoot, paths.Root)
		}
	})

	t.Run("MONODEV_ROOT takes precedence over repo-local", func(t *testing.T) {
		customRoot := "/custom/monodev/path"

		oldRoot := os.Getenv("MONODEV_ROOT")
		defer os.Setenv("MONODEV_ROOT", oldRoot)
		os.Setenv("MONODEV_ROOT", customRoot)

		// Create a temporary git repo with .monodev
		tmpDir, err := os.MkdirTemp("", "config-test-*")
		if err != nil {
			t.Fatalf("failed to create temp dir: %v", err)
		}
		defer os.RemoveAll(tmpDir)

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
		defer os.Chdir(oldWd)

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

func TestPaths_EnsureDirectories(t *testing.T) {
	t.Run("creates all necessary directories", func(t *testing.T) {
		tmpDir, err := os.MkdirTemp("", "config-test-*")
		if err != nil {
			t.Fatalf("failed to create temp dir: %v", err)
		}
		defer os.RemoveAll(tmpDir)

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
			if _, err := os.Stat(dir); os.IsNotExist(err) {
				t.Errorf("Directory %s was not created", dir)
			}
		}
	})

	t.Run("succeeds if directories already exist", func(t *testing.T) {
		tmpDir, err := os.MkdirTemp("", "config-test-*")
		if err != nil {
			t.Fatalf("failed to create temp dir: %v", err)
		}
		defer os.RemoveAll(tmpDir)

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
		defer os.RemoveAll(tmpDir)

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
