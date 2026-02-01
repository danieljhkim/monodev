package remote

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// GitPersistence provides operations for the separate Git persistence repository.
// This repository lives at .monodev/.git with work tree at .monodev.
type GitPersistence interface {
	// EnsureRepo initializes the persistence Git repository if it doesn't exist.
	// Creates a separate git repository with GIT_DIR=.monodev/.git and GIT_WORK_TREE=.monodev.
	// Also creates and checks out the orphan branch if needed.
	EnsureRepo(repoRoot, branch string) error

	// Commit stages the specified paths and creates a commit with the given message.
	Commit(repoRoot, message string, paths []string) error

	// Push pushes the specified branch to the remote.
	Push(repoRoot, remote, branch string, force bool) error

	// Fetch fetches the specified branch from the remote.
	Fetch(repoRoot, remote, branch string) error

	// Checkout checks out the specified branch to the .monodev work tree.
	Checkout(repoRoot, branch string) error

	// GetRemoteURL retrieves the URL of the specified remote from the main repository.
	GetRemoteURL(repoRoot, remoteName string) (string, error)

	// SetRemote configures a remote in the persistence repository.
	SetRemote(repoRoot, remoteName, url string) error
}

// RealGitPersistence is the production implementation using exec.Command.
type RealGitPersistence struct{}

// NewRealGitPersistence creates a new RealGitPersistence.
func NewRealGitPersistence() *RealGitPersistence {
	return &RealGitPersistence{}
}

// gitDir returns the path to the persistence git directory.
func (g *RealGitPersistence) gitDir(repoRoot string) string {
	return filepath.Join(repoRoot, ".monodev", ".git")
}

// workTree returns the path to the persistence work tree.
func (g *RealGitPersistence) workTree(repoRoot string) string {
	return filepath.Join(repoRoot, ".monodev")
}

// runGit executes a git command with GIT_DIR and GIT_WORK_TREE set.
func (g *RealGitPersistence) runGit(repoRoot string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = repoRoot
	cmd.Env = append(os.Environ(),
		fmt.Sprintf("GIT_DIR=%s", g.gitDir(repoRoot)),
		fmt.Sprintf("GIT_WORK_TREE=%s", g.workTree(repoRoot)),
	)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	if err != nil {
		return "", fmt.Errorf("git command failed: %w\nstderr: %s", err, stderr.String())
	}

	return strings.TrimSpace(stdout.String()), nil
}

// EnsureRepo initializes the persistence repository.
func (g *RealGitPersistence) EnsureRepo(repoRoot, branch string) error {
	gitDirPath := g.gitDir(repoRoot)
	workTreePath := g.workTree(repoRoot)

	// Check if git dir already exists
	if _, err := os.Stat(gitDirPath); err == nil {
		// Repository exists, ensure we're on the correct branch
		return g.ensureBranch(repoRoot, branch)
	}

	// Create the work tree directory if it doesn't exist
	if err := os.MkdirAll(workTreePath, 0755); err != nil {
		return fmt.Errorf("failed to create work tree directory: %w", err)
	}

	// Initialize the git repository
	if _, err := g.runGit(repoRoot, "init"); err != nil {
		return fmt.Errorf("failed to initialize git repository: %w", err)
	}

	// Create and checkout the orphan branch
	if err := g.ensureBranch(repoRoot, branch); err != nil {
		return fmt.Errorf("failed to create orphan branch: %w", err)
	}

	return nil
}

// ensureBranch ensures the specified branch exists and is checked out.
func (g *RealGitPersistence) ensureBranch(repoRoot, branch string) error {
	// Check if branch exists
	_, err := g.runGit(repoRoot, "rev-parse", "--verify", branch)
	if err != nil {
		// Branch doesn't exist, create it as orphan
		if _, err := g.runGit(repoRoot, "checkout", "--orphan", branch); err != nil {
			return fmt.Errorf("failed to create orphan branch: %w", err)
		}
		// Remove any files from index (orphan checkout may copy from HEAD)
		_, _ = g.runGit(repoRoot, "rm", "-rf", "--ignore-unmatch", ".")
	} else {
		// Branch exists, just check it out
		if _, err := g.runGit(repoRoot, "checkout", branch); err != nil {
			return fmt.Errorf("failed to checkout branch: %w", err)
		}
	}

	return nil
}

// Commit stages paths and creates a commit.
func (g *RealGitPersistence) Commit(repoRoot, message string, paths []string) error {
	// Stage the specified paths
	// Use -f to bypass .gitignore rules in the persistence repo
	args := append([]string{"add", "-f"}, paths...)
	if _, err := g.runGit(repoRoot, args...); err != nil {
		return fmt.Errorf("failed to stage files: %w", err)
	}

	// Check if there are staged changes to commit
	// Use --name-only to get list of staged files (empty if nothing staged)
	stagedFiles, err := g.runGit(repoRoot, "diff", "--cached", "--name-only")
	if err != nil {
		return fmt.Errorf("failed to check staged changes: %w", err)
	}

	if stagedFiles == "" {
		// No staged changes to commit
		return nil
	}

	// Create the commit
	if _, err := g.runGit(repoRoot, "commit", "-m", message); err != nil {
		return fmt.Errorf("failed to commit: %w", err)
	}

	return nil
}

// Push pushes the branch to the remote.
func (g *RealGitPersistence) Push(repoRoot, remote, branch string, force bool) error {
	args := []string{"push", remote, branch}
	if force {
		args = append(args, "--force")
	}

	if _, err := g.runGit(repoRoot, args...); err != nil {
		return fmt.Errorf("failed to push: %w", err)
	}

	return nil
}

// Fetch fetches the branch from the remote.
func (g *RealGitPersistence) Fetch(repoRoot, remote, branch string) error {
	if _, err := g.runGit(repoRoot, "fetch", remote, branch); err != nil {
		return fmt.Errorf("failed to fetch: %w", err)
	}

	return nil
}

// Checkout checks out the specified branch.
func (g *RealGitPersistence) Checkout(repoRoot, branch string) error {
	if _, err := g.runGit(repoRoot, "checkout", branch); err != nil {
		return fmt.Errorf("failed to checkout: %w", err)
	}

	return nil
}

// GetRemoteURL retrieves the URL of a remote from the main repository.
func (g *RealGitPersistence) GetRemoteURL(repoRoot, remoteName string) (string, error) {
	// Run git command in the main repository (not the persistence repo)
	cmd := exec.Command("git", "remote", "get-url", remoteName)
	cmd.Dir = repoRoot

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	if err != nil {
		return "", ErrRemoteNotFound
	}

	url := strings.TrimSpace(stdout.String())
	if url == "" {
		return "", ErrRemoteNotFound
	}

	return url, nil
}

// SetRemote configures a remote in the persistence repository.
func (g *RealGitPersistence) SetRemote(repoRoot, remoteName, url string) error {
	// Check if remote exists
	_, err := g.runGit(repoRoot, "remote", "get-url", remoteName)
	if err == nil {
		// Remote exists, update it
		if _, err := g.runGit(repoRoot, "remote", "set-url", remoteName, url); err != nil {
			return fmt.Errorf("failed to update remote: %w", err)
		}
	} else {
		// Remote doesn't exist, add it
		if _, err := g.runGit(repoRoot, "remote", "add", remoteName, url); err != nil {
			return fmt.Errorf("failed to add remote: %w", err)
		}
	}

	return nil
}

// FakeGitPersistence is a test double that tracks operations without executing them.
type FakeGitPersistence struct {
	EnsureRepoCalls []EnsureRepoCall
	CommitCalls     []CommitCall
	PushCalls       []PushCall
	FetchCalls      []FetchCall
	CheckoutCalls   []CheckoutCall
	GetRemoteCalls  []GetRemoteCall
	SetRemoteCalls  []SetRemoteCall

	// Configurable responses
	EnsureRepoErr error
	CommitErr     error
	PushErr       error
	FetchErr      error
	CheckoutErr   error
	RemoteURL     string
	GetRemoteErr  error
	SetRemoteErr  error
}

type EnsureRepoCall struct {
	RepoRoot string
	Branch   string
}

type CommitCall struct {
	RepoRoot string
	Message  string
	Paths    []string
}

type PushCall struct {
	RepoRoot string
	Remote   string
	Branch   string
	Force    bool
}

type FetchCall struct {
	RepoRoot string
	Remote   string
	Branch   string
}

type CheckoutCall struct {
	RepoRoot string
	Branch   string
}

type GetRemoteCall struct {
	RepoRoot   string
	RemoteName string
}

type SetRemoteCall struct {
	RepoRoot   string
	RemoteName string
	URL        string
}

// NewFakeGitPersistence creates a new FakeGitPersistence.
func NewFakeGitPersistence() *FakeGitPersistence {
	return &FakeGitPersistence{
		RemoteURL: "https://github.com/example/repo.git",
	}
}

func (f *FakeGitPersistence) EnsureRepo(repoRoot, branch string) error {
	f.EnsureRepoCalls = append(f.EnsureRepoCalls, EnsureRepoCall{
		RepoRoot: repoRoot,
		Branch:   branch,
	})
	return f.EnsureRepoErr
}

func (f *FakeGitPersistence) Commit(repoRoot, message string, paths []string) error {
	f.CommitCalls = append(f.CommitCalls, CommitCall{
		RepoRoot: repoRoot,
		Message:  message,
		Paths:    paths,
	})
	return f.CommitErr
}

func (f *FakeGitPersistence) Push(repoRoot, remote, branch string, force bool) error {
	f.PushCalls = append(f.PushCalls, PushCall{
		RepoRoot: repoRoot,
		Remote:   remote,
		Branch:   branch,
		Force:    force,
	})
	return f.PushErr
}

func (f *FakeGitPersistence) Fetch(repoRoot, remote, branch string) error {
	f.FetchCalls = append(f.FetchCalls, FetchCall{
		RepoRoot: repoRoot,
		Remote:   remote,
		Branch:   branch,
	})
	return f.FetchErr
}

func (f *FakeGitPersistence) Checkout(repoRoot, branch string) error {
	f.CheckoutCalls = append(f.CheckoutCalls, CheckoutCall{
		RepoRoot: repoRoot,
		Branch:   branch,
	})
	return f.CheckoutErr
}

func (f *FakeGitPersistence) GetRemoteURL(repoRoot, remoteName string) (string, error) {
	f.GetRemoteCalls = append(f.GetRemoteCalls, GetRemoteCall{
		RepoRoot:   repoRoot,
		RemoteName: remoteName,
	})
	return f.RemoteURL, f.GetRemoteErr
}

func (f *FakeGitPersistence) SetRemote(repoRoot, remoteName, url string) error {
	f.SetRemoteCalls = append(f.SetRemoteCalls, SetRemoteCall{
		RepoRoot:   repoRoot,
		RemoteName: remoteName,
		URL:        url,
	})
	return f.SetRemoteErr
}
