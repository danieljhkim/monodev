package remote

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

// validGitRefPattern matches safe git ref names (branch names, remote names).
// Allows alphanumeric characters, dots, underscores, hyphens, and forward slashes.
// This prevents command injection via malicious branch/remote names.
var validGitRefPattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._/-]*$`)

func contextOrBackground(ctx context.Context) context.Context {
	if ctx == nil {
		return context.Background()
	}
	return ctx
}

// validateGitRef validates a git ref name (branch or remote) for safety.
// Returns an error if the ref contains potentially dangerous characters.
func validateGitRef(ref, refType string) error {
	if ref == "" {
		return fmt.Errorf("invalid %s: empty", refType)
	}
	if !validGitRefPattern.MatchString(ref) {
		return fmt.Errorf("invalid %s %q: contains invalid characters", refType, ref)
	}
	// Reject refs that could be interpreted as command-line options
	if strings.HasPrefix(ref, "-") {
		return fmt.Errorf("invalid %s %q: cannot start with hyphen", refType, ref)
	}
	return nil
}

// GitPersistence provides operations for the separate Git persistence repository.
// This repository lives at .monodev/.git with work tree at .monodev.
type GitPersistence interface {
	// EnsureRepo initializes the persistence Git repository if it doesn't exist.
	// Creates a separate git repository with GIT_DIR=.monodev/.git and GIT_WORK_TREE=.monodev.
	// Also creates and checks out the orphan branch if needed.
	EnsureRepo(ctx context.Context, repoRoot, branch string) error

	// Commit stages the specified paths and creates a commit with the given message.
	Commit(ctx context.Context, repoRoot, message string, paths []string) error

	// Push pushes the specified branch to the remote.
	Push(ctx context.Context, repoRoot, remote, branch string, force bool) error

	// Fetch fetches the specified branch from the remote.
	Fetch(ctx context.Context, repoRoot, remote, branch string) error

	// Checkout checks out the specified branch to the .monodev work tree.
	Checkout(ctx context.Context, repoRoot, branch string) error

	// GetRemoteURL retrieves the URL of the specified remote from the main repository.
	GetRemoteURL(ctx context.Context, repoRoot, remoteName string) (string, error)

	// SetRemote configures a remote in the persistence repository.
	SetRemote(ctx context.Context, repoRoot, remoteName, url string) error
}

// RealGitPersistence is the production implementation using exec.CommandContext.
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
func (g *RealGitPersistence) runGit(ctx context.Context, repoRoot string, args ...string) (string, error) {
	ctx = contextOrBackground(ctx)
	if err := ctx.Err(); err != nil {
		return "", err
	}

	cmd := exec.CommandContext(ctx, "git", args...)
	configureGitCommandForContext(cmd)
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
		if ctxErr := ctx.Err(); ctxErr != nil {
			return "", ctxErr
		}
		return "", fmt.Errorf("git command failed: %w\nstderr: %s", err, stderr.String())
	}

	return strings.TrimSpace(stdout.String()), nil
}

// EnsureRepo initializes the persistence repository.
func (g *RealGitPersistence) EnsureRepo(ctx context.Context, repoRoot, branch string) error {
	ctx = contextOrBackground(ctx)
	if err := validateGitRef(branch, "branch"); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	gitDirPath := g.gitDir(repoRoot)
	workTreePath := g.workTree(repoRoot)

	// Check if git dir already exists
	if _, err := os.Stat(gitDirPath); err == nil {
		// Repository exists, ensure we're on the correct branch
		return g.ensureBranch(ctx, repoRoot, branch)
	}

	// Create the work tree directory if it doesn't exist
	if err := os.MkdirAll(workTreePath, 0755); err != nil {
		return fmt.Errorf("failed to create work tree directory: %w", err)
	}

	// Initialize the git repository
	if _, err := g.runGit(ctx, repoRoot, "init"); err != nil {
		return fmt.Errorf("failed to initialize git repository: %w", err)
	}

	// Create and checkout the orphan branch
	if err := g.ensureBranch(ctx, repoRoot, branch); err != nil {
		return fmt.Errorf("failed to create orphan branch: %w", err)
	}

	return nil
}

// ensureBranch ensures the specified branch exists and is checked out.
func (g *RealGitPersistence) ensureBranch(ctx context.Context, repoRoot, branch string) error {
	// Check if branch exists
	_, err := g.runGit(ctx, repoRoot, "rev-parse", "--verify", branch)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return err
		}
		// Branch doesn't exist, create it as orphan
		if _, err := g.runGit(ctx, repoRoot, "checkout", "--orphan", branch); err != nil {
			return fmt.Errorf("failed to create orphan branch: %w", err)
		}
		// Remove any files from index (orphan checkout may copy from HEAD)
		_, _ = g.runGit(ctx, repoRoot, "rm", "-rf", "--ignore-unmatch", ".")
	} else {
		// Branch exists, just check it out
		if _, err := g.runGit(ctx, repoRoot, "checkout", branch); err != nil {
			return fmt.Errorf("failed to checkout branch: %w", err)
		}
	}

	return nil
}

// Commit stages paths and creates a commit.
func (g *RealGitPersistence) Commit(ctx context.Context, repoRoot, message string, paths []string) error {
	ctx = contextOrBackground(ctx)
	// Stage the specified paths
	// Use -f to bypass .gitignore rules in the persistence repo
	args := append([]string{"add", "-f"}, paths...)
	if _, err := g.runGit(ctx, repoRoot, args...); err != nil {
		return fmt.Errorf("failed to stage files: %w", err)
	}

	// Check if there are staged changes to commit
	// Use --name-only to get list of staged files (empty if nothing staged)
	stagedFiles, err := g.runGit(ctx, repoRoot, "diff", "--cached", "--name-only")
	if err != nil {
		return fmt.Errorf("failed to check staged changes: %w", err)
	}

	if stagedFiles == "" {
		// No staged changes to commit
		return nil
	}

	// Create the commit
	if _, err := g.runGit(ctx, repoRoot, "commit", "-m", message); err != nil {
		return fmt.Errorf("failed to commit: %w", err)
	}

	return nil
}

// Push pushes the branch to the remote.
func (g *RealGitPersistence) Push(ctx context.Context, repoRoot, remote, branch string, force bool) error {
	ctx = contextOrBackground(ctx)
	if err := validateGitRef(remote, "remote"); err != nil {
		return err
	}
	if err := validateGitRef(branch, "branch"); err != nil {
		return err
	}

	args := []string{"push", remote, branch}
	if force {
		args = append(args, "--force")
	}

	if _, err := g.runGit(ctx, repoRoot, args...); err != nil {
		return fmt.Errorf("failed to push: %w", err)
	}

	return nil
}

// Fetch fetches the branch from the remote.
func (g *RealGitPersistence) Fetch(ctx context.Context, repoRoot, remote, branch string) error {
	ctx = contextOrBackground(ctx)
	if err := validateGitRef(remote, "remote"); err != nil {
		return err
	}
	if err := validateGitRef(branch, "branch"); err != nil {
		return err
	}

	if _, err := g.runGit(ctx, repoRoot, "fetch", remote, branch); err != nil {
		return fmt.Errorf("failed to fetch: %w", err)
	}

	return nil
}

// Checkout checks out the specified branch.
func (g *RealGitPersistence) Checkout(ctx context.Context, repoRoot, branch string) error {
	ctx = contextOrBackground(ctx)
	if err := validateGitRef(branch, "branch"); err != nil {
		return err
	}

	if _, err := g.runGit(ctx, repoRoot, "checkout", branch); err != nil {
		return fmt.Errorf("failed to checkout: %w", err)
	}

	return nil
}

// GetRemoteURL retrieves the URL of a remote from the main repository.
func (g *RealGitPersistence) GetRemoteURL(ctx context.Context, repoRoot, remoteName string) (string, error) {
	ctx = contextOrBackground(ctx)
	if err := validateGitRef(remoteName, "remote"); err != nil {
		return "", err
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}

	// Run git command in the main repository (not the persistence repo)
	cmd := exec.CommandContext(ctx, "git", "remote", "get-url", remoteName)
	configureGitCommandForContext(cmd)
	cmd.Dir = repoRoot

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return "", ctxErr
		}
		return "", ErrRemoteNotFound
	}

	url := strings.TrimSpace(stdout.String())
	if url == "" {
		return "", ErrRemoteNotFound
	}

	return url, nil
}

// SetRemote configures a remote in the persistence repository.
func (g *RealGitPersistence) SetRemote(ctx context.Context, repoRoot, remoteName, url string) error {
	ctx = contextOrBackground(ctx)
	if err := validateGitRef(remoteName, "remote"); err != nil {
		return err
	}

	// Check if remote exists
	_, err := g.runGit(ctx, repoRoot, "remote", "get-url", remoteName)
	if err == nil {
		// Remote exists, update it
		if _, err := g.runGit(ctx, repoRoot, "remote", "set-url", remoteName, url); err != nil {
			return fmt.Errorf("failed to update remote: %w", err)
		}
	} else {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return err
		}
		// Remote doesn't exist, add it
		if _, err := g.runGit(ctx, repoRoot, "remote", "add", remoteName, url); err != nil {
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

	EnsureRepoHook func(context.Context, EnsureRepoCall) error
	CommitHook     func(context.Context, CommitCall) error
	PushHook       func(context.Context, PushCall) error
	FetchHook      func(context.Context, FetchCall) error
	CheckoutHook   func(context.Context, CheckoutCall) error
	GetRemoteHook  func(context.Context, GetRemoteCall) error
	SetRemoteHook  func(context.Context, SetRemoteCall) error
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

func (f *FakeGitPersistence) EnsureRepo(ctx context.Context, repoRoot, branch string) error {
	ctx = contextOrBackground(ctx)
	if err := ctx.Err(); err != nil {
		return err
	}
	call := EnsureRepoCall{
		RepoRoot: repoRoot,
		Branch:   branch,
	}
	f.EnsureRepoCalls = append(f.EnsureRepoCalls, call)
	if f.EnsureRepoHook != nil {
		if err := f.EnsureRepoHook(ctx, call); err != nil {
			return err
		}
	}
	return f.EnsureRepoErr
}

func (f *FakeGitPersistence) Commit(ctx context.Context, repoRoot, message string, paths []string) error {
	ctx = contextOrBackground(ctx)
	if err := ctx.Err(); err != nil {
		return err
	}
	call := CommitCall{
		RepoRoot: repoRoot,
		Message:  message,
		Paths:    paths,
	}
	f.CommitCalls = append(f.CommitCalls, call)
	if f.CommitHook != nil {
		if err := f.CommitHook(ctx, call); err != nil {
			return err
		}
	}
	return f.CommitErr
}

func (f *FakeGitPersistence) Push(ctx context.Context, repoRoot, remote, branch string, force bool) error {
	ctx = contextOrBackground(ctx)
	if err := ctx.Err(); err != nil {
		return err
	}
	call := PushCall{
		RepoRoot: repoRoot,
		Remote:   remote,
		Branch:   branch,
		Force:    force,
	}
	f.PushCalls = append(f.PushCalls, call)
	if f.PushHook != nil {
		if err := f.PushHook(ctx, call); err != nil {
			return err
		}
	}
	return f.PushErr
}

func (f *FakeGitPersistence) Fetch(ctx context.Context, repoRoot, remote, branch string) error {
	ctx = contextOrBackground(ctx)
	if err := ctx.Err(); err != nil {
		return err
	}
	call := FetchCall{
		RepoRoot: repoRoot,
		Remote:   remote,
		Branch:   branch,
	}
	f.FetchCalls = append(f.FetchCalls, call)
	if f.FetchHook != nil {
		if err := f.FetchHook(ctx, call); err != nil {
			return err
		}
	}
	return f.FetchErr
}

func (f *FakeGitPersistence) Checkout(ctx context.Context, repoRoot, branch string) error {
	ctx = contextOrBackground(ctx)
	if err := ctx.Err(); err != nil {
		return err
	}
	call := CheckoutCall{
		RepoRoot: repoRoot,
		Branch:   branch,
	}
	f.CheckoutCalls = append(f.CheckoutCalls, call)
	if f.CheckoutHook != nil {
		if err := f.CheckoutHook(ctx, call); err != nil {
			return err
		}
	}
	return f.CheckoutErr
}

func (f *FakeGitPersistence) GetRemoteURL(ctx context.Context, repoRoot, remoteName string) (string, error) {
	ctx = contextOrBackground(ctx)
	if err := ctx.Err(); err != nil {
		return "", err
	}
	call := GetRemoteCall{
		RepoRoot:   repoRoot,
		RemoteName: remoteName,
	}
	f.GetRemoteCalls = append(f.GetRemoteCalls, call)
	if f.GetRemoteHook != nil {
		if err := f.GetRemoteHook(ctx, call); err != nil {
			return "", err
		}
	}
	return f.RemoteURL, f.GetRemoteErr
}

func (f *FakeGitPersistence) SetRemote(ctx context.Context, repoRoot, remoteName, url string) error {
	ctx = contextOrBackground(ctx)
	if err := ctx.Err(); err != nil {
		return err
	}
	call := SetRemoteCall{
		RepoRoot:   repoRoot,
		RemoteName: remoteName,
		URL:        url,
	}
	f.SetRemoteCalls = append(f.SetRemoteCalls, call)
	if f.SetRemoteHook != nil {
		if err := f.SetRemoteHook(ctx, call); err != nil {
			return err
		}
	}
	return f.SetRemoteErr
}
