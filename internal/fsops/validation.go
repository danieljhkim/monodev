package fsops

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Exists checks if a path exists.
func (fs *RealFS) Exists(path string) (bool, error) {
	_, err := os.Lstat(path)
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, err
}

// validateRelPath validates a relative path for safety.
// Returns an error if the path is invalid or unsafe.
func (fs *RealFS) ValidateRelPath(relPath string) error {
	// Clean the path first
	cleaned := filepath.Clean(relPath)

	// Reject empty or current directory
	if cleaned == "" || cleaned == "." {
		return fmt.Errorf("invalid path: empty or current directory")
	}

	// Reject absolute paths
	if filepath.IsAbs(cleaned) {
		return fmt.Errorf("invalid path: must be relative, got absolute path %q", cleaned)
	}

	// Reject path traversal attempts
	if strings.HasPrefix(cleaned, "..") || strings.Contains(cleaned, string(filepath.Separator)+"..") {
		return fmt.Errorf("invalid path: path traversal not allowed in %q", cleaned)
	}

	return nil
}

// ValidatePathOutsideGitDir rejects paths targeting the repository's Git metadata.
// The target path is expected to be absolute or relative to repoRoot consistently.
//
// The lexical comparison is followed by an identity check on the first
// component below repoRoot, so a name that the filesystem resolves to the real
// .git directory (for example ".GIT" on a case-insensitive volume) is rejected
// too, while ordinary names such as ".github" or ".git-hooks" are not.
func ValidatePathOutsideGitDir(repoRoot, targetPath string) error {
	gitDir := filepath.Join(repoRoot, ".git")
	relToGitDir, err := filepath.Rel(gitDir, targetPath)
	if err != nil {
		return fmt.Errorf("failed to compare path with repository .git directory: %w", err)
	}

	if relToGitDir == "." || (relToGitDir != ".." && !strings.HasPrefix(relToGitDir, ".."+string(filepath.Separator))) {
		return fmt.Errorf("path resolves inside repository .git directory")
	}

	relToRoot, err := filepath.Rel(repoRoot, targetPath)
	if err != nil {
		return fmt.Errorf("failed to compare path with repository root: %w", err)
	}
	if relToRoot == "." || relToRoot == ".." || strings.HasPrefix(relToRoot, ".."+string(filepath.Separator)) {
		return nil
	}
	first := strings.SplitN(relToRoot, string(filepath.Separator), 2)[0]

	gitInfo, err := os.Lstat(gitDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("failed to inspect repository .git directory: %w", err)
	}
	firstInfo, err := os.Lstat(filepath.Join(repoRoot, first))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("failed to inspect path component %q: %w", first, err)
	}
	if os.SameFile(gitInfo, firstInfo) {
		return fmt.Errorf("path resolves inside repository .git directory")
	}

	return nil
}

// ValidateIdentifier validates an identifier (e.g., store ID, workspace ID) for safety.
// Returns an error if the identifier contains invalid characters or path traversal attempts.
func (fs *RealFS) ValidateIdentifier(id string) error {
	// Reject empty identifiers
	if id == "" {
		return fmt.Errorf("invalid identifier: empty")
	}

	// Reject identifiers that look like paths
	if strings.Contains(id, string(filepath.Separator)) || strings.Contains(id, "/") || strings.Contains(id, "\\") {
		return fmt.Errorf("invalid identifier: must not contain path separators")
	}

	// Reject path traversal attempts
	// Note: explicit parentheses for clarity, even though && has higher precedence than ||
	if id == "." || id == ".." || (strings.HasPrefix(id, ".") && len(id) > 1 && id[1] == '.') {
		return fmt.Errorf("invalid identifier: path traversal not allowed")
	}

	return nil
}
