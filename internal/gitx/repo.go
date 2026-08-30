package gitx

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// GitRepo provides an abstraction for git repository operations.
type GitRepo interface {
	// Discover finds the git repository root starting from cwd.
	Discover(cwd string) (root string, err error)

	// CommonGitDir resolves the shared git directory, including when root is a
	// linked worktree whose .git entry is a file.
	CommonGitDir(root string) (string, error)

	// Fingerprint computes a stable fingerprint for the repository.
	Fingerprint(root string) (string, error)

	// RelPath computes the relative path from repo root to the given absolute path.
	RelPath(root, absPath string) (string, error)

	// GetFingerprintComponents returns the absolute path and git URL used to compute the fingerprint.
	GetFingerprintComponents(root string) (absPath string, gitURL string, err error)

	// Username returns the GitHub username derived from the remote origin URL,
	// or falls back to git config user.name. Returns "user" if neither is available.
	Username(root string) string

	// IsIgnored reports which of the given paths (relative to cwd) are
	// excluded by standard git ignore rules (.gitignore, .git/info/exclude,
	// global excludes). Paths not present in the result are not ignored.
	IsIgnored(cwd string, relPaths []string) (map[string]bool, error)
}

// RealGitRepo implements GitRepo using actual git commands.
type RealGitRepo struct{}

// NewRealGitRepo creates a new RealGitRepo.
func NewRealGitRepo() *RealGitRepo {
	return &RealGitRepo{}
}

// Discover finds the git repository root by walking up from cwd looking for .git directory.
func (g *RealGitRepo) Discover(cwd string) (string, error) {
	absPath, err := filepath.Abs(cwd)
	if err != nil {
		return "", fmt.Errorf("failed to get absolute path: %w", err)
	}

	current := absPath
	for {
		gitDir := filepath.Join(current, ".git")
		if info, err := os.Stat(gitDir); err == nil {
			// .git can be a directory or a file (for worktrees/submodules)
			if info.IsDir() || info.Mode().IsRegular() {
				return current, nil
			}
		}

		parent := filepath.Dir(current)
		if parent == current {
			// Reached root directory
			return "", fmt.Errorf("not in a git repository")
		}
		current = parent
	}
}

// CommonGitDir returns the repository's shared git directory. Git owns the
// indirection for linked worktrees, so do not infer this from root/.git.
func (g *RealGitRepo) CommonGitDir(root string) (string, error) {
	cmd := exec.Command("git", "rev-parse", "--path-format=absolute", "--git-common-dir")
	cmd.Dir = root
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("failed to resolve common git directory: %w", err)
	}

	gitDir := strings.TrimSpace(string(output))
	if gitDir == "" {
		return "", fmt.Errorf("git returned an empty common git directory")
	}
	if !filepath.IsAbs(gitDir) {
		gitDir = filepath.Join(root, gitDir)
	}
	return filepath.Clean(gitDir), nil
}

func execGit(dir string, args ...string) *exec.Cmd {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	return cmd
}

func execGitConfig(dir, key string) *exec.Cmd {
	return execGit(dir, "config", "--get", key)
}

// Fingerprint computes a stable fingerprint for the repository.
//
// Identity precedence:
//  1. Durable repo ID (`.monodev/repo-id`, else git-common-dir `monodev/repo-id`)
//  2. Normalized remote URL (origin, otherwise the first `git remote`)
//  3. Newly persisted durable ID for a git repo with no remotes
//  4. Absolute path for non-git directories
//
// The clone's absolute path is not part of (1)–(3), so moving a clone does not
// change the fingerprint. A linked git worktree shares the repo-identity
// material with the main checkout (same durable ID / remote), but a
// worktree-specific suffix is mixed in afterward so the two never collide —
// see worktreeDiscriminator.
func (g *RealGitRepo) Fingerprint(root string) (string, error) {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return "", fmt.Errorf("failed to get absolute path: %w", err)
	}

	material, err := g.fingerprintMaterial(absRoot)
	if err != nil {
		return "", err
	}

	discriminator, err := g.worktreeDiscriminator(absRoot)
	if err != nil {
		return "", err
	}
	if discriminator != "" {
		material += "|worktree:" + discriminator
	}

	return HashFingerprint(material), nil
}

// fingerprintMaterial returns the un-hashed repo-identity material, shared by
// every linked worktree of the same repository.
func (g *RealGitRepo) fingerprintMaterial(absRoot string) (string, error) {
	if id, err := readDurableRepoID(absRoot); err != nil {
		return "", err
	} else if id != "" {
		return fingerprintIDPrefix + id, nil
	}

	remoteURL, err := selectRemoteURL(absRoot)
	if err == nil && remoteURL != "" {
		normalized := NormalizeRemoteURL(remoteURL)
		if normalized != "" {
			return fingerprintRemotePrefix + normalized, nil
		}
	}

	if _, gitErr := g.CommonGitDir(absRoot); gitErr == nil {
		id, err := EnsureDurableRepoID(absRoot)
		if err != nil {
			return "", fmt.Errorf("failed to persist durable repo id: %w", err)
		}
		return fingerprintIDPrefix + id, nil
	}

	return fingerprintPathPrefix + absRoot, nil
}

// worktreeDiscriminator returns a stable, non-empty suffix when root is a
// linked git worktree (as opposed to the main checkout or a bare repo), so
// that each worktree's fingerprint — and therefore its workspace ID and
// applied-overlay ledger — is independent of every other worktree's. It
// returns "" for the main checkout, keeping its fingerprint unchanged.
//
// A linked worktree's private git-dir lives at
// "<git-common-dir>/worktrees/<name>", which differs from the main
// checkout's git-dir (equal to the common dir itself). <name> is used as the
// discriminator: it is stable across `monodev` invocations and does not
// depend on the worktree's absolute path, so relocating the whole repository
// (main checkout plus worktrees) together does not change it.
func (g *RealGitRepo) worktreeDiscriminator(root string) (string, error) {
	gitDir, err := gitDirAbsolute(root)
	if err != nil {
		// Not a git repository (or git is unavailable): no discriminator.
		return "", nil
	}

	commonDir, err := g.CommonGitDir(root)
	if err != nil {
		return "", nil
	}

	if filepath.Clean(gitDir) == filepath.Clean(commonDir) {
		return "", nil
	}

	return filepath.Base(filepath.Clean(gitDir)), nil
}

// gitDirAbsolute resolves the repository's per-worktree git directory
// ("--git-dir"), which differs from the common git dir for linked worktrees.
func gitDirAbsolute(root string) (string, error) {
	cmd := exec.Command("git", "rev-parse", "--path-format=absolute", "--git-dir")
	cmd.Dir = root
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("failed to resolve git directory: %w", err)
	}

	gitDir := strings.TrimSpace(string(output))
	if gitDir == "" {
		return "", fmt.Errorf("git returned an empty git directory")
	}
	if !filepath.IsAbs(gitDir) {
		gitDir = filepath.Join(root, gitDir)
	}
	return filepath.Clean(gitDir), nil
}

// RelPath computes the relative path from repo root to the given absolute path.
func (g *RealGitRepo) RelPath(root, absPath string) (string, error) {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return "", fmt.Errorf("failed to get absolute root: %w", err)
	}

	absTarget, err := filepath.Abs(absPath)
	if err != nil {
		return "", fmt.Errorf("failed to get absolute target: %w", err)
	}

	relPath, err := filepath.Rel(absRoot, absTarget)
	if err != nil {
		return "", fmt.Errorf("failed to compute relative path: %w", err)
	}

	// Check if the path is outside the repo
	if strings.HasPrefix(relPath, "..") {
		return "", fmt.Errorf("path is outside repository")
	}

	return relPath, nil
}

// GetFingerprintComponents returns the absolute repository path and the raw
// origin URL (empty when origin is unset). The fingerprint no longer hashes
// these values directly; callers use them for display and legacy migration.
func (g *RealGitRepo) GetFingerprintComponents(root string) (string, string, error) {
	// Get the absolute path of the root
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return "", "", fmt.Errorf("failed to get absolute path: %w", err)
	}

	cmd := execGitConfig(root, "remote.origin.url")
	output, err := cmd.Output()

	gitURL := ""
	if err == nil {
		gitURL = strings.TrimSpace(string(output))
	}

	return absRoot, gitURL, nil
}

// Username returns the GitHub username from the remote origin URL,
// falling back to git config user.name, then "user".
func (g *RealGitRepo) Username(root string) string {
	// Try to extract username from remote origin URL
	cmd := execGitConfig(root, "remote.origin.url")
	output, err := cmd.Output()
	if err == nil {
		url := strings.TrimSpace(string(output))
		if username := extractGitHubUsername(url); username != "" {
			return username
		}
	}

	// Fall back to git config user.name
	cmd = exec.Command("git", "config", "--get", "user.name")
	cmd.Dir = root
	output, err = cmd.Output()
	if err == nil {
		name := strings.TrimSpace(string(output))
		if name != "" {
			return name
		}
	}

	return "user"
}

// extractGitHubUsername extracts the username from a GitHub remote URL.
// Supports SSH (git@github.com:user/repo.git) and HTTPS (https://github.com/user/repo.git).
func extractGitHubUsername(url string) string {
	// SSH format: git@github.com:user/repo.git
	if strings.HasPrefix(url, "git@github.com:") {
		parts := strings.SplitN(strings.TrimPrefix(url, "git@github.com:"), "/", 2)
		if len(parts) >= 1 && parts[0] != "" {
			return parts[0]
		}
	}

	// HTTPS format: https://github.com/user/repo.git
	if strings.Contains(url, "github.com/") {
		idx := strings.Index(url, "github.com/")
		rest := url[idx+len("github.com/"):]
		parts := strings.SplitN(rest, "/", 2)
		if len(parts) >= 1 && parts[0] != "" {
			return parts[0]
		}
	}

	return ""
}

// IsIgnored reports which of relPaths (interpreted relative to cwd) are
// excluded by git's ignore rules, using a single batched `git check-ignore`
// call. A failure to run git (e.g. cwd is not inside a repository) is
// returned as an error; callers that want best-effort behavior should treat
// that as "nothing is ignored" rather than failing discovery outright.
func (g *RealGitRepo) IsIgnored(cwd string, relPaths []string) (map[string]bool, error) {
	result := make(map[string]bool, len(relPaths))
	if len(relPaths) == 0 {
		return result, nil
	}

	cmd := exec.Command("git", "check-ignore", "--stdin", "-z")
	cmd.Dir = cwd
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("failed to open check-ignore stdin: %w", err)
	}
	var stdout bytes.Buffer
	cmd.Stdout = &stdout

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("failed to start check-ignore: %w", err)
	}

	for _, p := range relPaths {
		if _, err := stdin.Write([]byte(filepath.ToSlash(p) + "\x00")); err != nil {
			_ = stdin.Close()
			_ = cmd.Wait()
			return nil, fmt.Errorf("failed to write check-ignore input: %w", err)
		}
	}
	if err := stdin.Close(); err != nil {
		return nil, fmt.Errorf("failed to close check-ignore stdin: %w", err)
	}

	// git check-ignore exits 1 when none of the supplied paths are ignored;
	// that is a normal outcome, not a failure.
	if err := cmd.Wait(); err != nil {
		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) || exitErr.ExitCode() != 1 {
			return nil, fmt.Errorf("check-ignore failed: %w", err)
		}
	}

	for _, ignored := range strings.Split(stdout.String(), "\x00") {
		if ignored == "" {
			continue
		}
		result[filepath.FromSlash(ignored)] = true
	}
	return result, nil
}

// FakeGitRepo implements GitRepo with predetermined values for testing.
type FakeGitRepo struct {
	root        string
	fingerprint string
	absPath     string
	gitURL      string
	username    string
	ignored     map[string]bool
	err         error
}

// NewFakeGitRepo creates a new FakeGitRepo.
func NewFakeGitRepo(root, fingerprint string) *FakeGitRepo {
	return &FakeGitRepo{
		root:        root,
		fingerprint: fingerprint,
		absPath:     root,
		gitURL:      "git@github.com:test/repo.git",
	}
}

// NewFakeGitRepoWithComponents creates a new FakeGitRepo with custom components.
func NewFakeGitRepoWithComponents(root, fingerprint, absPath, gitURL string) *FakeGitRepo {
	return &FakeGitRepo{
		root:        root,
		fingerprint: fingerprint,
		absPath:     absPath,
		gitURL:      gitURL,
	}
}

// SetError sets an error to be returned by all methods.
func (g *FakeGitRepo) SetError(err error) {
	g.err = err
}

// Discover returns the predetermined root.
func (g *FakeGitRepo) Discover(cwd string) (string, error) {
	if g.err != nil {
		return "", g.err
	}
	return g.root, nil
}

// CommonGitDir returns the conventional metadata location beneath the fake
// repository root.
func (g *FakeGitRepo) CommonGitDir(root string) (string, error) {
	if g.err != nil {
		return "", g.err
	}
	return filepath.Join(root, ".git"), nil
}

// Fingerprint returns the predetermined fingerprint.
func (g *FakeGitRepo) Fingerprint(root string) (string, error) {
	if g.err != nil {
		return "", g.err
	}
	return g.fingerprint, nil
}

// RelPath computes the relative path (works like real implementation).
func (g *FakeGitRepo) RelPath(root, absPath string) (string, error) {
	if g.err != nil {
		return "", g.err
	}

	relPath, err := filepath.Rel(root, absPath)
	if err != nil {
		return "", fmt.Errorf("failed to compute relative path: %w", err)
	}

	if strings.HasPrefix(relPath, "..") {
		return "", fmt.Errorf("path is outside repository")
	}

	return relPath, nil
}

// GetFingerprintComponents returns the predetermined components.
func (g *FakeGitRepo) GetFingerprintComponents(root string) (string, string, error) {
	if g.err != nil {
		return "", "", g.err
	}
	return g.absPath, g.gitURL, nil
}

// SetUsername sets the username to return from Username().
func (g *FakeGitRepo) SetUsername(username string) {
	g.username = username
}

// Username returns the predetermined username or "user".
func (g *FakeGitRepo) Username(root string) string {
	if g.username != "" {
		return g.username
	}
	return "user"
}

// IsIgnored reports the predetermined ignored set, or nothing ignored by default.
func (g *FakeGitRepo) IsIgnored(cwd string, relPaths []string) (map[string]bool, error) {
	if g.err != nil {
		return nil, g.err
	}
	result := make(map[string]bool, len(g.ignored))
	for _, p := range relPaths {
		if g.ignored[p] {
			result[p] = true
		}
	}
	return result, nil
}

// SetIgnored configures which relative paths IsIgnored reports as ignored.
func (g *FakeGitRepo) SetIgnored(paths ...string) {
	g.ignored = make(map[string]bool, len(paths))
	for _, p := range paths {
		g.ignored[p] = true
	}
}
