package engine

import (
	"fmt"
	"path/filepath"
	"strings"
)

// resolveToWorkspaceRelative resolves a user-provided path to a clean CWD-relative path.
// It validates that the path is within the repo and within the cwd (no escaping via "..").
func resolveToWorkspaceRelative(userPath, cwd, repoRoot string) (string, error) {
	var absPath string
	if filepath.IsAbs(userPath) {
		absPath = userPath
	} else {
		absPath = filepath.Join(cwd, userPath)
	}
	absPath = filepath.Clean(absPath)
	cwd = filepath.Clean(cwd)
	repoRoot = filepath.Clean(repoRoot)

	// Validate within repo
	repoRel, err := filepath.Rel(repoRoot, absPath)
	if err != nil {
		return "", fmt.Errorf("failed to compute repo-relative path for %q: %w", userPath, err)
	}
	if repoRel == ".." || strings.HasPrefix(repoRel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("path %q resolves to %q which is outside the repository", userPath, absPath)
	}

	// Compute CWD-relative path
	cwdRel, err := filepath.Rel(cwd, absPath)
	if err != nil {
		return "", fmt.Errorf("failed to compute cwd-relative path for %q: %w", userPath, err)
	}

	// Reject paths that escape the workspace
	if cwdRel == ".." || strings.HasPrefix(cwdRel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("path %q resolves outside the workspace directory", userPath)
	}

	// Reject workspace root itself
	if cwdRel == "." {
		return "", fmt.Errorf("path %q resolves to the workspace directory, which cannot be tracked", userPath)
	}

	return cwdRel, nil
}

// resolveToRepoRelative resolves a user-provided path (absolute, relative, or containing "..")
// to a clean repo-root-relative path. It rejects paths that escape the repo boundary or
// resolve to the repo root itself.
func resolveToRepoRelative(userPath, cwd, repoRoot string) (string, error) {
	var absPath string
	if filepath.IsAbs(userPath) {
		absPath = userPath
	} else {
		absPath = filepath.Join(cwd, userPath)
	}
	absPath = filepath.Clean(absPath)

	repoRoot = filepath.Clean(repoRoot)

	relPath, err := filepath.Rel(repoRoot, absPath)
	if err != nil {
		return "", fmt.Errorf("failed to compute repo-relative path for %q: %w", userPath, err)
	}

	// Reject paths outside the repo
	if relPath == ".." || strings.HasPrefix(relPath, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("path %q resolves to %q which is outside the repository", userPath, absPath)
	}

	// Reject repo root itself
	if relPath == "." {
		return "", fmt.Errorf("path %q resolves to the repository root, which cannot be tracked", userPath)
	}

	return relPath, nil
}
