package engine

import (
	"fmt"
	"path/filepath"
	"strings"
)

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
