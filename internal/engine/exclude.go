package engine

import (
	"bytes"
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

// syncManagedExcludes makes the repository-local exclusion block reflect one
// workspace ledger. It preserves all bytes outside monodev's delimiters.
func (e *Engine) syncManagedExcludes(repoRoot, workspacePath string, ws *state.WorkspaceState) error {
	gitDir, err := e.gitRepo.CommonGitDir(repoRoot)
	if err != nil {
		return err
	}
	excludePath := filepath.Join(gitDir, "info", "exclude")

	entries, err := e.managedExcludeEntries(workspacePath, ws)
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

func (e *Engine) managedExcludeEntries(workspacePath string, ws *state.WorkspaceState) ([]string, error) {
	if ws == nil || len(ws.Paths) == 0 {
		return nil, nil
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
		if err := e.fs.ValidateRelPath(relPath); err != nil {
			return nil, fmt.Errorf("invalid managed path %q in ledger: %w", relPath, err)
		}

		repoRelative := relPath
		if workspacePath != "." {
			repoRelative = filepath.Join(workspacePath, relPath)
		}
		repoRelative = filepath.ToSlash(repoRelative)
		if repoRelative == "." || strings.HasPrefix(repoRelative, "../") {
			return nil, fmt.Errorf("invalid repository-relative managed path %q", repoRelative)
		}

		entry := "/" + escapeExcludePattern(repoRelative)
		if ownership.Contents != nil {
			entry += "/"
		}
		entries = append(entries, entry)
	}
	sort.Strings(entries)
	return entries, nil
}

func escapeExcludePattern(path string) string {
	replacer := strings.NewReplacer(
		"\\", "\\\\",
		"*", "\\*",
		"?", "\\?",
		"[", "\\[",
		"]", "\\]",
	)
	return replacer.Replace(path)
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
