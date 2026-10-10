package engine

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/danieljhkim/monodev/internal/state"
)

const (
	managedExcludeStart = "# >>> monodev managed block — do not edit <<<"
	managedExcludeEnd   = "# <<< monodev managed block <<<"
)

// syncManagedExcludes makes the common Git exclusion block reflect every
// workspace ledger that shares its Git directory. It preserves all bytes
// outside monodev's delimiters.
func (e *Engine) syncManagedExcludes(ctx context.Context, repoRoot, workspaceID, workspacePath string, current *state.WorkspaceState) error {
	gitDir, err := e.gitRepo.CommonGitDir(repoRoot)
	if err != nil {
		return err
	}
	unlock, err := lockManagedExcludes(ctx, gitDir)
	if err != nil {
		return err
	}
	defer unlock()
	excludePath := filepath.Join(gitDir, "info", "exclude")

	entries, err := e.managedExcludeEntriesForGitDir(repoRoot, gitDir, workspaceID, workspacePath, current)
	if err != nil {
		return err
	}

	contents, err := e.fs.ReadFile(excludePath)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to read .git/info/exclude: %w", err)
	}
	if os.IsNotExist(err) {
		contents = nil
	}

	replacement := managedExcludeBlock(entries)
	updated, changed, err := replaceManagedExcludeBlock(contents, replacement)
	if err != nil {
		return err
	}
	if !changed {
		return nil
	}

	mode := os.FileMode(0644)
	info, statErr := e.fs.Lstat(excludePath)
	if statErr == nil && info != nil {
		if info.IsDir() {
			return fmt.Errorf(".git/info/exclude is a directory")
		}
		mode = info.Mode().Perm()
		if mode&0222 == 0 {
			return fmt.Errorf(".git/info/exclude is read-only")
		}
	} else if statErr != nil && !os.IsNotExist(statErr) {
		return fmt.Errorf("failed to stat .git/info/exclude: %w", statErr)
	}
	if err := e.fs.AtomicWrite(excludePath, updated, mode); err != nil {
		return fmt.Errorf("failed to write .git/info/exclude: %w", err)
	}
	return nil
}

// managedExcludeEntriesForGitDir collects every persisted workspace whose
// checkout resolves to gitDir. Comparing Git's resolved common directory,
// rather than repository fingerprints or paths, keeps linked worktrees and
// sibling workspaces correct without conflating distinct clones of a remote.
func (e *Engine) managedExcludeEntriesForGitDir(repoRoot, gitDir, currentWorkspaceID, currentWorkspacePath string, current *state.WorkspaceState) ([]string, error) {
	entries := make(map[string]struct{})
	wantGitDir := filepath.Clean(gitDir)
	currentWorkspaceRoot, err := filepath.Abs(filepath.Join(repoRoot, currentWorkspacePath))
	if err != nil {
		return nil, fmt.Errorf("resolve current workspace path: %w", err)
	}
	currentRepoRoot, err := filepath.Abs(repoRoot)
	if err != nil {
		return nil, fmt.Errorf("resolve current repository path: %w", err)
	}
	addEntries := func(workspacePath string, ws *state.WorkspaceState) error {
		workspaceEntries, err := e.managedExcludeEntries(workspacePath, ws)
		if err != nil {
			return err
		}
		for _, entry := range workspaceEntries {
			entries[entry] = struct{}{}
		}
		return nil
	}
	// Include the caller's post-transaction ledger explicitly. Older ledgers
	// may not have AbsolutePath yet, so relying only on the scan would briefly
	// drop their contribution during the operation that updates them.
	if current != nil {
		if err := addEntries(currentWorkspacePath, current); err != nil {
			return nil, err
		}
	}
	err = e.forEachWorkspaceState(func(workspaceID string, ws *state.WorkspaceState) error {
		if ws == nil || ws.AbsolutePath == "" || len(ws.Paths) == 0 {
			return nil
		}
		if workspaceID == currentWorkspaceID {
			return nil
		}
		workspaceRoot, err := filepath.Abs(ws.AbsolutePath)
		if err != nil {
			return nil
		}
		if filepath.Clean(workspaceRoot) == filepath.Clean(currentWorkspaceRoot) ||
			(filepath.Clean(workspaceRoot) == filepath.Clean(currentRepoRoot) && ws.WorkspacePath == currentWorkspacePath) {
			return nil
		}
		workspaceGitDir, err := e.gitRepo.CommonGitDir(workspaceRoot)
		if err != nil || filepath.Clean(workspaceGitDir) != wantGitDir {
			return nil
		}
		return addEntries(ws.WorkspacePath, ws)
	})
	if err != nil {
		return nil, err
	}

	ordered := make([]string, 0, len(entries))
	for entry := range entries {
		ordered = append(ordered, entry)
	}
	sort.Strings(ordered)
	return ordered, nil
}

func (e *Engine) managedExcludeEntries(workspacePath string, ws *state.WorkspaceState) ([]string, error) {
	if ws == nil || len(ws.Paths) == 0 {
		return nil, nil
	}
	// Store selection can clear Applied without removing installed overlays.
	// Paths also contains committed intent, so only owners in AppliedStores
	// contribute exclusions. Ledgers predating that list use Applied instead;
	// an explicitly empty list means no stores remain applied.
	legacyApplied := ws.AppliedStores == nil && ws.Applied
	appliedStores := make(map[string]bool, len(ws.AppliedStores))
	for _, applied := range ws.AppliedStores {
		appliedStores[applied.Store] = true
	}

	workspacePath = filepath.Clean(workspacePath)
	if workspacePath == "" {
		workspacePath = "."
	}
	if workspacePath != "." {
		if err := e.fs.ValidateRelPath(workspacePath); err != nil {
			return nil, fmt.Errorf("invalid workspace path in ledger: %w", err)
		}
	}

	entries := make([]string, 0, len(ws.Paths))
	for relPath, ownership := range ws.Paths {
		if !legacyApplied && !appliedStores[ownership.Store] {
			continue
		}
		if err := e.fs.ValidateRelPath(relPath); err != nil {
			return nil, fmt.Errorf("invalid managed path %q in ledger: %w", relPath, err)
		}

		repoRelative, err := repoRelativeManagedPath(workspacePath, relPath)
		if err != nil {
			return nil, err
		}
		// Contents marks a copy-mode directory. The trailing slash keeps the
		// rule directory-only and preserves a carriage return that would
		// otherwise end the exclude line.
		entry, err := managedExcludePattern(repoRelative, ownership.Contents != nil)
		if err != nil {
			return nil, err
		}
		entries = append(entries, entry)
	}
	sort.Strings(entries)
	return entries, nil
}

// repoRelativeManagedPath is the path an exclude rule matches, relative to
// the repository root. workspacePath is that workspace's directory (".", at
// the root). relPath is workspace-relative.
func repoRelativeManagedPath(workspacePath, relPath string) (string, error) {
	workspacePath = filepath.Clean(workspacePath)
	if workspacePath == "" {
		workspacePath = "."
	}
	repoRelative := relPath
	if workspacePath != "." {
		repoRelative = filepath.Join(workspacePath, relPath)
	}
	repoRelative = filepath.ToSlash(repoRelative)
	if repoRelative == "." || strings.HasPrefix(repoRelative, "../") {
		return "", fmt.Errorf("invalid repository-relative managed path %q", repoRelative)
	}
	return repoRelative, nil
}

// managedExcludePattern returns the gitignore line for one managed path.
// directory is true when the entry ends in a slash, which is how copy-mode
// directories are recorded. The line selects that path and no other. A
// newline or NUL cannot be stored in one line. A carriage return that would
// end the line is stripped by Git before escapes are read, so the rule would
// name a different path; a directory slash keeps that byte in the middle,
// where Git preserves it.
func managedExcludePattern(repoRelative string, directory bool) (string, error) {
	if strings.Contains(repoRelative, "\n") || strings.ContainsRune(repoRelative, 0) {
		return "", fmt.Errorf("%w: managed path %q cannot be represented as a git exclusion", ErrValidation, repoRelative)
	}
	entry := "/" + escapeExcludePattern(repoRelative)
	if directory {
		entry += "/"
	}
	if strings.HasSuffix(entry, "\r") {
		return "", fmt.Errorf("%w: managed path %q cannot be represented as a git exclusion", ErrValidation, repoRelative)
	}
	return entry, nil
}

func validateManagedExcludePath(workspacePath, relPath string, directory bool) error {
	repoRelative, err := repoRelativeManagedPath(workspacePath, relPath)
	if err != nil {
		return err
	}
	_, err = managedExcludePattern(repoRelative, directory)
	return err
}

// escapeExcludePattern quotes characters that would make a gitignore rule
// select a different path. Git strips unescaped spaces at the end of a
// pattern line, so each trailing space is quoted. Interior spaces are kept.
func escapeExcludePattern(path string) string {
	var quoted strings.Builder
	quoted.Grow(len(path))
	for i := 0; i < len(path); i++ {
		switch path[i] {
		case '\\', '*', '?', '[', ']':
			quoted.WriteByte('\\')
			quoted.WriteByte(path[i])
		default:
			quoted.WriteByte(path[i])
		}
	}
	escaped := quoted.String()
	end := len(escaped)
	for end > 0 && escaped[end-1] == ' ' {
		end--
	}
	if end == len(escaped) {
		return escaped
	}
	var out strings.Builder
	out.Grow(len(escaped) + (len(escaped) - end))
	out.WriteString(escaped[:end])
	for i := end; i < len(escaped); i++ {
		out.WriteString(`\ `)
	}
	return out.String()
}

func managedExcludeBlock(entries []string) []byte {
	if len(entries) == 0 {
		return nil
	}

	var block strings.Builder
	block.WriteString(managedExcludeStart)
	block.WriteByte('\n')
	for _, entry := range entries {
		block.WriteString(entry)
		block.WriteByte('\n')
	}
	block.WriteString(managedExcludeEnd)
	block.WriteByte('\n')
	return []byte(block.String())
}

func replaceManagedExcludeBlock(contents, replacement []byte) ([]byte, bool, error) {
	start, end, found, err := managedExcludeBounds(contents)
	if err != nil {
		return nil, false, err
	}

	if found {
		// The existing contents length is already a valid allocation size. Avoid
		// calculating a replacement size from multiple attacker-controlled lengths
		// before allocating, since that integer arithmetic can overflow.
		updated := make([]byte, 0, len(contents))
		updated = append(updated, contents[:start]...)
		updated = append(updated, replacement...)
		updated = append(updated, contents[end:]...)
		return updated, !bytes.Equal(updated, contents), nil
	}

	if len(replacement) == 0 {
		return contents, false, nil
	}

	if len(contents) == 0 || contents[len(contents)-1] == '\n' {
		updated := append(append([]byte{}, contents...), replacement...)
		return updated, true, nil
	}

	// A user file without a final newline must remain byte-identical outside
	// the delimiters. Put the new block before it instead of adding a newline.
	updated := append(append([]byte{}, replacement...), contents...)
	return updated, true, nil
}

func managedExcludeBounds(contents []byte) (start, end int, found bool, err error) {
	start = -1
	for offset := 0; offset < len(contents); {
		next := len(contents)
		if newline := bytes.IndexByte(contents[offset:], '\n'); newline >= 0 {
			next = offset + newline + 1
		}
		line := contents[offset:next]
		line = bytes.TrimSuffix(line, []byte("\n"))
		line = bytes.TrimSuffix(line, []byte("\r"))

		switch string(line) {
		case managedExcludeStart:
			if start >= 0 {
				return 0, 0, false, fmt.Errorf(".git/info/exclude has nested monodev managed blocks")
			}
			start = offset
		case managedExcludeEnd:
			if start >= 0 {
				return start, next, true, nil
			}
		}
		offset = next
	}
	if start >= 0 {
		return 0, 0, false, fmt.Errorf(".git/info/exclude has an unterminated monodev managed block")
	}
	return 0, 0, false, nil
}

func appendExcludeWarning(warnings []string, err error) []string {
	if err == nil {
		return warnings
	}
	warning := fmt.Sprintf("could not update .git/info/exclude: %v", err)
	for _, existing := range warnings {
		if existing == warning {
			return warnings
		}
	}
	return append(warnings, warning)
}
