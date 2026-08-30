// Package config manages monodev configuration and filesystem paths.
//
// The default state root is repo-local `.monodev/` (auto-created on first use).
// `MONODEV_ROOT` opts into a custom root, including `$HOME/.monodev` for
// cross-repo stores. Existing `~/.monodev` stores remain visible through the
// global scope so they are not silently stranded.
package config

import (
	"fmt"
	"os"
	"path/filepath"
)

const (
	// EnvRoot is the environment variable that overrides the state root.
	// Set it to `$HOME/.monodev` to keep using the home-directory root.
	EnvRoot = "MONODEV_ROOT"

	// RepoLocalDirName is the repo-local state directory name.
	RepoLocalDirName = ".monodev"

	// RepoLocalGitignore is written inside `.monodev/` so artifacts never
	// leak into `git status`.
	RepoLocalGitignore = "# monodev artifacts (local-only)\n*\n"
)

// Paths contains all the filesystem paths used by monodev.
type Paths struct {
	// Root is the base directory for all monodev data.
	// Default: <repo>/.monodev. Override: MONODEV_ROOT. Home: ~/.monodev.
	Root string

	// Stores is the directory containing all store data
	Stores string

	// Workspaces is the directory containing workspace state files
	Workspaces string

	// Config is the path to the global config file
	Config string
}

// NotInGitRepositoryError matches the historical `monodev init` message.
func NotInGitRepositoryError(err error) error {
	return fmt.Errorf("not in a git repository: %w\nmonodev init must be run inside a git repository", err)
}

// DefaultPaths returns the default paths for monodev.
// Path resolution priority:
// 1. MONODEV_ROOT environment variable (highest priority)
// 2. Repo-local .monodev when inside a git repository
// 3. ~/.monodev (home-directory / non-git fallback)
func DefaultPaths() (*Paths, error) {
	// Priority 1: MONODEV_ROOT env var
	if root := os.Getenv(EnvRoot); root != "" {
		return buildPaths(root), nil
	}

	// Priority 2: Repo-local .monodev (default inside a git repository)
	if cwd, err := os.Getwd(); err == nil {
		if repoRoot, err := discoverGitRoot(cwd); err == nil {
			return buildPaths(filepath.Join(repoRoot, RepoLocalDirName)), nil
		}
	}

	// Priority 3: Global ~/.monodev (home-directory fallback)
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("failed to get user home directory: %w", err)
	}
	return buildPaths(filepath.Join(home, RepoLocalDirName)), nil
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

// sameFilesystemPath reports whether two path strings refer to the same
// location. It prefers os.SameFile when both paths exist, and falls back to
// normalized absolute paths for callers comparing paths that may not exist yet.
func sameFilesystemPath(a, b string) bool {
	aAbs, aErr := filepath.Abs(a)
	if aErr != nil {
		aAbs = filepath.Clean(a)
	}
	bAbs, bErr := filepath.Abs(b)
	if bErr != nil {
		bAbs = filepath.Clean(b)
	}

	aInfo, aStatErr := os.Stat(aAbs)
	bInfo, bStatErr := os.Stat(bAbs)
	if aStatErr == nil && bStatErr == nil {
		return os.SameFile(aInfo, bInfo)
	}

	if resolved, err := filepath.EvalSymlinks(aAbs); err == nil {
		aAbs = resolved
	}
	if resolved, err := filepath.EvalSymlinks(bAbs); err == nil {
		bAbs = resolved
	}
	return filepath.Clean(aAbs) == filepath.Clean(bAbs)
}

// ScopedPaths provides dual-scope path resolution for global and component stores.
type ScopedPaths struct {
	// Global points to ~/.monodev (or MONODEV_ROOT). Existing home stores stay
	// reachable here so they are not silently stranded after repo-local became
	// the default.
	Global *Paths

	// Component points to repo_root/.monodev (nil if no separate component scope)
	Component *Paths

	// HasRepoContext is true when a git repo with .monodev was found.
	HasRepoContext bool

	// RepoRoot is the git repository root (empty if no repo context)
	RepoRoot string
}

// NewScopedPaths resolves both global and component paths without creating
// directories. Global always resolves to ~/.monodev (or MONODEV_ROOT).
// Component resolves to repo_root/.monodev if we're in a git repo that has it.
func NewScopedPaths() (*ScopedPaths, error) {
	sp := &ScopedPaths{}

	// Global: MONODEV_ROOT or ~/.monodev
	if root := os.Getenv(EnvRoot); root != "" {
		sp.Global = buildPaths(root)
	} else {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("failed to get user home directory: %w", err)
		}
		sp.Global = buildPaths(filepath.Join(home, RepoLocalDirName))
	}

	// Component: repo_root/.monodev (if in a git repo)
	if cwd, err := os.Getwd(); err == nil {
		if repoRoot, err := discoverGitRoot(cwd); err == nil {
			sp.RepoRoot = repoRoot
			repoLocalPath := filepath.Join(repoRoot, RepoLocalDirName)
			if pathExists(repoLocalPath) {
				sp.HasRepoContext = true
				if !sameFilesystemPath(sp.Global.Root, repoLocalPath) {
					sp.Component = buildPaths(repoLocalPath)
				}
			}
		}
	}

	return sp, nil
}

// EnsureRepoLocalRoot creates <repoRoot>/.monodev/{stores,workspaces} at mode
// 0700 and writes the `*`-content .gitignore. It is idempotent.
func EnsureRepoLocalRoot(repoRoot string) (string, error) {
	monodevPath := filepath.Join(repoRoot, RepoLocalDirName)
	dirs := []string{
		monodevPath,
		filepath.Join(monodevPath, "stores"),
		filepath.Join(monodevPath, "workspaces"),
	}
	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0700); err != nil {
			return "", fmt.Errorf("failed to create directory %s: %w", dir, err)
		}
	}

	gitignorePath := filepath.Join(monodevPath, ".gitignore")
	if err := os.WriteFile(gitignorePath, []byte(RepoLocalGitignore), 0600); err != nil {
		return "", fmt.Errorf("failed to create .gitignore: %w", err)
	}
	return monodevPath, nil
}

// EnsureScopedPaths resolves scoped paths and auto-creates the repo-local
// `.monodev` root when MONODEV_ROOT is unset. When MONODEV_ROOT is unset and
// the process is not inside a git repository, it returns the same error
// `monodev init` produces.
func EnsureScopedPaths() (*ScopedPaths, error) {
	if os.Getenv(EnvRoot) == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return nil, fmt.Errorf("failed to get current directory: %w", err)
		}
		repoRoot, err := discoverGitRoot(cwd)
		if err != nil {
			return nil, NotInGitRepositoryError(err)
		}
		if _, err := EnsureRepoLocalRoot(repoRoot); err != nil {
			return nil, err
		}
	}

	return NewScopedPaths()
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
		if err := os.MkdirAll(dir, 0700); err != nil {
			return fmt.Errorf("failed to create directory %s: %w", dir, err)
		}
	}

	return nil
}
