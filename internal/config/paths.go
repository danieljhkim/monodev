// Package config manages monodev configuration and filesystem paths.
//
// The default state root is repo-local `.monodev/` (auto-created on first use).
// `MONODEV_ROOT` opts into a custom root, including `$HOME/.monodev` for
// cross-repo stores. Existing `~/.monodev` stores remain visible through the
// global scope so they are not silently stranded.
package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/unix"
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
// 0700 and writes the `*`-content .gitignore. It is idempotent. The repository
// must already exist; state paths must not be symlinks. Directory handles keep
// the operations anchored even if a checked path is replaced concurrently.
func EnsureRepoLocalRoot(repoRoot string) (string, error) {
	monodevPath := filepath.Join(repoRoot, RepoLocalDirName)
	repoFD, err := openRepoLocalRepository(repoRoot)
	if err != nil {
		return "", fmt.Errorf("failed to open repository root without following symlinks: %w", err)
	}
	defer func() { _ = unix.Close(repoFD) }()

	// Preflight existing paths before creating anything: a bad ignore leaf or
	// child directory must not leave a partially initialized state root.
	rootFD, err := openRepoLocalRoot(repoFD)
	if err != nil {
		return "", err
	}
	if rootFD < 0 {
		rootFD, err = createRepoLocalDirectory(repoFD, RepoLocalDirName)
	}
	if err != nil {
		return "", fmt.Errorf("failed to open repo-local root without following symlinks: %w", err)
	}
	defer func() { _ = unix.Close(rootFD) }()

	for _, name := range []string{"stores", "workspaces", ".gitignore"} {
		if err := checkRepoLocalEntry(rootFD, name, name != ".gitignore"); err != nil {
			return "", err
		}
	}
	for _, name := range []string{"stores", "workspaces"} {
		fd, err := createRepoLocalDirectory(rootFD, name)
		if err != nil {
			return "", fmt.Errorf("failed to create repo-local directory %s: %w", name, err)
		}
		_ = unix.Close(fd)
	}

	// Do not truncate until the opened inode is known to be a regular file
	// with no hard-link aliases. O_NONBLOCK avoids blocking on a raced FIFO.
	fd, err := unix.Openat(rootFD, ".gitignore", unix.O_WRONLY|unix.O_CREAT|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0600)
	if err != nil {
		return "", fmt.Errorf("failed to open .gitignore without following symlinks: %w", err)
	}
	f := os.NewFile(uintptr(fd), filepath.Join(monodevPath, ".gitignore"))
	defer func() { _ = f.Close() }()
	var stat unix.Stat_t
	if err := unix.Fstat(fd, &stat); err != nil {
		return "", fmt.Errorf("failed to inspect .gitignore: %w", err)
	}
	if stat.Mode&unix.S_IFMT != unix.S_IFREG || stat.Nlink != 1 {
		return "", fmt.Errorf("unsafe repo-local .gitignore: expected a regular file without hard-link aliases")
	}
	if err := f.Chmod(0600); err != nil {
		return "", fmt.Errorf("failed to set .gitignore permissions: %w", err)
	}
	if err := f.Truncate(0); err != nil {
		return "", fmt.Errorf("failed to truncate .gitignore: %w", err)
	}
	if _, err := f.WriteString(RepoLocalGitignore); err != nil {
		return "", fmt.Errorf("failed to write .gitignore: %w", err)
	}
	if err := f.Close(); err != nil {
		return "", fmt.Errorf("failed to close .gitignore: %w", err)
	}
	return monodevPath, nil
}

// ValidateRepoLocalRoot checks existing repo-local paths without creating or
// modifying them. It is used by `monodev init` before its already-initialized
// check so unsafe symlinks are reported directly, even without --force.
func ValidateRepoLocalRoot(repoRoot string) error {
	repoFD, err := openRepoLocalRepository(repoRoot)
	if err != nil {
		return fmt.Errorf("failed to open repository root without following symlinks: %w", err)
	}
	defer func() { _ = unix.Close(repoFD) }()

	rootFD, err := openRepoLocalRoot(repoFD)
	if err != nil {
		return err
	}
	if rootFD >= 0 {
		_ = unix.Close(rootFD)
	}
	return nil
}

// openRepoLocalRoot opens the existing .monodev directory and validates its
// children. A missing root is reported as fd -1; no files are created.
func openRepoLocalRoot(repoFD int) (int, error) {
	if err := checkRepoLocalEntry(repoFD, RepoLocalDirName, true); err != nil {
		return -1, err
	}
	rootFD, err := unix.Openat(repoFD, RepoLocalDirName, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_DIRECTORY|unix.O_NOFOLLOW, 0)
	if errors.Is(err, unix.ENOENT) {
		return -1, nil
	}
	if err != nil {
		if errors.Is(err, unix.ELOOP) {
			return -1, fmt.Errorf("unsafe symlinked repo-local path %s", RepoLocalDirName)
		}
		if errors.Is(err, unix.ENOTDIR) {
			return -1, fmt.Errorf("unsafe repo-local path %s: expected a directory", RepoLocalDirName)
		}
		return -1, fmt.Errorf("failed to open repo-local root %s without following symlinks: %w", RepoLocalDirName, err)
	}
	for _, name := range []string{"stores", "workspaces", ".gitignore"} {
		if err := checkRepoLocalEntry(rootFD, name, name != ".gitignore"); err != nil {
			_ = unix.Close(rootFD)
			return -1, err
		}
	}
	return rootFD, nil
}

// Walk every repository ancestor without following links, rather than relying
// on O_NOFOLLOW on only the final component of a pathname.
func openRepoLocalRepository(repoRoot string) (int, error) {
	// Repository roots come from the user's current directory and may be
	// reached through normal symlinked ancestors such as /tmp on macOS. Resolve
	// that trusted path first, then keep the descriptor walk no-follow so the
	// checkout-controlled .monodev subtree remains protected from symlinks.
	resolvedRoot, err := filepath.EvalSymlinks(repoRoot)
	if err != nil {
		component := repoRoot
		var pathErr *os.PathError
		if errors.As(err, &pathErr) && pathErr.Path != "" {
			component = pathErr.Path
		}
		if errors.Is(err, unix.ELOOP) {
			return -1, fmt.Errorf("unsafe symlinked repository path %s", component)
		}
		if errors.Is(err, unix.ENOTDIR) {
			return -1, fmt.Errorf("unsafe repository path component %s: expected a directory", component)
		}
		return -1, fmt.Errorf("failed to resolve repository path %s: %w", component, err)
	}
	absRoot, err := filepath.Abs(resolvedRoot)
	if err != nil {
		return -1, err
	}
	fd, err := unix.Open(string(filepath.Separator), unix.O_RDONLY|unix.O_CLOEXEC|unix.O_DIRECTORY, 0)
	if err != nil {
		return -1, err
	}
	currentPath := string(filepath.Separator)
	for _, part := range strings.Split(strings.TrimPrefix(absRoot, string(filepath.Separator)), string(filepath.Separator)) {
		if part == "" {
			continue
		}
		nextFD, err := unix.Openat(fd, part, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_DIRECTORY|unix.O_NOFOLLOW, 0)
		if err != nil {
			componentPath := filepath.Join(currentPath, part)
			var stat unix.Stat_t
			if statErr := unix.Fstatat(fd, part, &stat, unix.AT_SYMLINK_NOFOLLOW); statErr == nil && stat.Mode&unix.S_IFMT == unix.S_IFLNK {
				_ = unix.Close(fd)
				return -1, fmt.Errorf("unsafe symlinked repository ancestor %s", componentPath)
			}
			_ = unix.Close(fd)
			if errors.Is(err, unix.ELOOP) {
				return -1, fmt.Errorf("unsafe symlinked repository ancestor %s", componentPath)
			}
			if errors.Is(err, unix.ENOTDIR) {
				return -1, fmt.Errorf("unsafe repository ancestor %s: expected a directory", componentPath)
			}
			return -1, fmt.Errorf("failed to open repository ancestor %s: %w", componentPath, err)
		}
		_ = unix.Close(fd)
		fd = nextFD
		currentPath = filepath.Join(currentPath, part)
	}
	return fd, nil
}

func checkRepoLocalEntry(parentFD int, name string, directory bool) error {
	var stat unix.Stat_t
	if err := unix.Fstatat(parentFD, name, &stat, unix.AT_SYMLINK_NOFOLLOW); err != nil {
		if errors.Is(err, unix.ENOENT) {
			return nil
		}
		return fmt.Errorf("failed to inspect repo-local %s: %w", name, err)
	}
	if stat.Mode&unix.S_IFMT == unix.S_IFLNK {
		return fmt.Errorf("unsafe symlinked repo-local path %s", name)
	}
	if directory {
		if stat.Mode&unix.S_IFMT != unix.S_IFDIR {
			return fmt.Errorf("unsafe repo-local path %s: expected a directory", name)
		}
	} else if stat.Mode&unix.S_IFMT != unix.S_IFREG || stat.Nlink != 1 {
		return fmt.Errorf("unsafe repo-local path %s: expected a regular file without hard-link aliases", name)
	}
	return nil
}

func createRepoLocalDirectory(parentFD int, name string) (int, error) {
	if err := unix.Mkdirat(parentFD, name, 0700); err != nil && !errors.Is(err, unix.EEXIST) {
		return -1, err
	}
	return unix.Openat(parentFD, name, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_DIRECTORY|unix.O_NOFOLLOW, 0)
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
		// Getwd can retain a PWD alias (notably /var on macOS). Resolve the
		// current repository location before walking it without following links;
		// never resolve the checkout-controlled .monodev paths themselves.
		cwd, err = filepath.EvalSymlinks(cwd)
		if err != nil {
			return nil, fmt.Errorf("failed to resolve current directory: %w", err)
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
