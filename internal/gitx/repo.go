package gitx

import (
	"crypto/sha256"
	"encoding/hex"
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

	// Fingerprint computes a stable fingerprint for the repository.
	Fingerprint(root string) (string, error)

	// RelPath computes the relative path from repo root to the given absolute path.
	RelPath(root, absPath string) (string, error)

	// GetFingerprintComponents returns the absolute path and git URL used to compute the fingerprint.
	GetFingerprintComponents(root string) (absPath string, gitURL string, err error)

	// Username returns the GitHub username derived from the remote origin URL,
	// or falls back to git config user.name. Returns "user" if neither is available.
	Username(root string) string
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

// Fingerprint computes a stable fingerprint for the repository.
// It uses the repo root path and remote origin URL (if available).
func (g *RealGitRepo) Fingerprint(root string) (string, error) {
	// Get the absolute path of the root
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return "", fmt.Errorf("failed to get absolute path: %w", err)
	}

	// Try to get the remote origin URL
	cmd := exec.Command("git", "config", "--get", "remote.origin.url")
	cmd.Dir = root
	output, err := cmd.Output()

	remoteURL := "unknown"
	if err == nil {
		remoteURL = strings.TrimSpace(string(output))
	}

	// Compute fingerprint from root path + remote URL
	data := absRoot + "|" + remoteURL

	hash := sha256.Sum256([]byte(data))
	return hex.EncodeToString(hash[:]), nil
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

// GetFingerprintComponents returns the absolute path and git URL used to compute the fingerprint.
func (g *RealGitRepo) GetFingerprintComponents(root string) (string, string, error) {
	// Get the absolute path of the root
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return "", "", fmt.Errorf("failed to get absolute path: %w", err)
	}

	// Try to get the remote origin URL
	cmd := exec.Command("git", "config", "--get", "remote.origin.url")
	cmd.Dir = root
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
	cmd := exec.Command("git", "config", "--get", "remote.origin.url")
	cmd.Dir = root
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

// FakeGitRepo implements GitRepo with predetermined values for testing.
type FakeGitRepo struct {
	root        string
	fingerprint string
	absPath     string
	gitURL      string
	username    string
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
