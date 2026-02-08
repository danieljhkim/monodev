// Package config manages monodev configuration and filesystem paths.
//
// Configuration includes the locations of monodev data directories, which can
// be customized via environment variables. The default root is ~/.monodev/
// containing stores/, workspaces/, and config files.
package config

import (
	"fmt"
	"os"
	"path/filepath"
)

// Paths contains all the filesystem paths used by monodev.
type Paths struct {
	// Root is the base directory for all monodev data (default: ~/.monodev)
	Root string

	// Stores is the directory containing all store data
	Stores string

	// Workspaces is the directory containing workspace state files
	Workspaces string

	// Config is the path to the global config file
	Config string
}

// DefaultPaths returns the default paths for monodev.
// Path resolution priority:
// 1. MONODEV_ROOT environment variable (highest priority)
// 2. Repo-local .monodev (if exists and we're in a git repo)
// 3. ~/.monodev (fallback - existing behavior)
func DefaultPaths() (*Paths, error) {
	// Priority 1: MONODEV_ROOT env var
	if root := os.Getenv("MONODEV_ROOT"); root != "" {
		return buildPaths(root), nil
	}

	// Priority 2: Repo-local .monodev
	if cwd, err := os.Getwd(); err == nil {
		if repoRoot, err := discoverGitRoot(cwd); err == nil {
			repoLocalPath := filepath.Join(repoRoot, ".monodev")
			if pathExists(repoLocalPath) {
				return buildPaths(repoLocalPath), nil
			}
		}
	}

	// Priority 3: Global ~/.monodev (fallback)
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("failed to get user home directory: %w", err)
	}
	return buildPaths(filepath.Join(home, ".monodev")), nil
}

// buildPaths constructs a Paths struct from a root directory.
func buildPaths(root string) *Paths {
	return &Paths{
		Root:       root,
		Stores:     filepath.Join(root, "stores"),
		Workspaces: filepath.Join(root, "workspaces"),
		Config:     filepath.Join(root, "config.yaml"),
	}
}

// discoverGitRoot walks up from cwd to find .git directory.
func discoverGitRoot(cwd string) (string, error) {
	absPath, err := filepath.Abs(cwd)
	if err != nil {
		return "", err
	}

	current := absPath
	for {
		gitDir := filepath.Join(current, ".git")
		if info, err := os.Stat(gitDir); err == nil {
			if info.IsDir() || info.Mode().IsRegular() {
				return current, nil
			}
		}

		parent := filepath.Dir(current)
		if parent == current {
			return "", fmt.Errorf("not in a git repository")
		}
		current = parent
	}
}

// pathExists checks if a path exists.
func pathExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// ScopedPaths provides dual-scope path resolution for global and component stores.
type ScopedPaths struct {
	// Global points to ~/.monodev (or MONODEV_ROOT)
	Global *Paths

	// Component points to repo_root/.monodev (nil if no repo context)
	Component *Paths

	// HasRepoContext is true when a git repo with .monodev was found
	HasRepoContext bool

	// RepoRoot is the git repository root (empty if no repo context)
	RepoRoot string
}

// NewScopedPaths resolves both global and component paths.
// Global always resolves to ~/.monodev (or MONODEV_ROOT).
// Component resolves to repo_root/.monodev if we're in a git repo that has it.
func NewScopedPaths() (*ScopedPaths, error) {
	sp := &ScopedPaths{}

	// Global: MONODEV_ROOT or ~/.monodev
	if root := os.Getenv("MONODEV_ROOT"); root != "" {
		sp.Global = buildPaths(root)
	} else {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("failed to get user home directory: %w", err)
		}
		sp.Global = buildPaths(filepath.Join(home, ".monodev"))
	}

	// Component: repo_root/.monodev (if in a git repo)
	if cwd, err := os.Getwd(); err == nil {
		if repoRoot, err := discoverGitRoot(cwd); err == nil {
			sp.RepoRoot = repoRoot
			repoLocalPath := filepath.Join(repoRoot, ".monodev")
			if pathExists(repoLocalPath) {
				sp.Component = buildPaths(repoLocalPath)
				sp.HasRepoContext = true
			}
		}
	}

	return sp, nil
}

// EnsureDirectories creates all necessary directories for both scopes.
func (sp *ScopedPaths) EnsureDirectories() error {
	if err := sp.Global.EnsureDirectories(); err != nil {
		return err
	}
	if sp.Component != nil {
		if err := sp.Component.EnsureDirectories(); err != nil {
			return err
		}
	}
	return nil
}

// EnsureDirectories creates all necessary directories if they don't exist.
func (p *Paths) EnsureDirectories() error {
	dirs := []string{
		p.Root,
		p.Stores,
		p.Workspaces,
	}

	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("failed to create directory %s: %w", dir, err)
		}
	}

	return nil
}
