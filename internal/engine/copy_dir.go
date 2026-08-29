package engine

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/danieljhkim/monodev/internal/planner"
	"github.com/danieljhkim/monodev/internal/state"
)

const forceUnapplyHint = "inspect the listed paths or re-run with --force to remove the directory including local changes"

// ownershipForAppliedPath records store ownership after an overlay operation.
// Copy-mode files get a checksum; copy-mode directories get a leaf manifest.
func (e *Engine) ownershipForAppliedPath(op planner.Operation, mode string) state.PathOwnership {
	ownership := state.PathOwnership{
		Store:     op.Store,
		Type:      mode,
		Timestamp: e.clock.Now(),
	}
	if mode != "copy" {
		return ownership
	}

	info, err := e.fs.Lstat(op.DestPath)
	if err != nil {
		return ownership
	}
	if info.IsDir() {
		files, err := e.copyDirFileChecksums(op.DestPath)
		if err == nil {
			ownership.Contents = &state.DirContents{Files: files}
		}
		return ownership
	}

	checksum, err := e.hasher.HashFile(op.DestPath)
	if err == nil {
		ownership.Checksum = checksum
	}
	return ownership
}

func (e *Engine) copyDirFileChecksums(root string) (map[string]string, error) {
	files := make(map[string]string)
	err := filepath.WalkDir(root, func(pathName string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if pathName == root {
			if !entry.IsDir() {
				return fmt.Errorf("expected directory at %s", root)
			}
			return nil
		}
		if entry.IsDir() {
			return nil
		}

		rel, err := filepath.Rel(root, pathName)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if !entry.Type().IsRegular() {
			files[rel] = "non-regular"
			return nil
		}
		checksum, err := e.hasher.HashFile(pathName)
		if err != nil {
			return err
		}
		files[rel] = checksum
		return nil
	})
	if err != nil {
		return nil, err
	}
	return files, nil
}

func sortDeepestFirst(paths []string) {
	sort.Slice(paths, func(i, j int) bool {
		depthI := countPathSeparators(paths[i])
		depthJ := countPathSeparators(paths[j])
		if depthI != depthJ {
			return depthI > depthJ
		}
		return paths[i] > paths[j]
	})
}

func (e *Engine) planManagedPathRemoval(workspaceRoot string, workspaceState *state.WorkspaceState, relPaths []string, force bool) ([]planner.Operation, []string, error) {
	sortDeepestFirst(relPaths)

	if !force {
		for _, relPath := range relPaths {
			if err := e.fs.ValidateRelPath(relPath); err != nil {
				return nil, nil, fmt.Errorf("invalid path %q in workspace state: %w", relPath, err)
			}
			absPath := filepath.Join(workspaceRoot, relPath)
			if err := e.validateManagedPath(absPath, relPath, workspaceState.Paths[relPath]); err != nil {
				return nil, nil, fmt.Errorf("validation failed for %s: %w", relPath, err)
			}
		}
	}

	ops := make([]planner.Operation, 0, len(relPaths))
	removed := make([]string, 0, len(relPaths))
	for _, relPath := range relPaths {
		if err := e.fs.ValidateRelPath(relPath); err != nil {
			return nil, nil, fmt.Errorf("invalid path %q in workspace state: %w", relPath, err)
		}
		ops = append(ops, planner.Operation{
			Type:     planner.OpRemove,
			DestPath: filepath.Join(workspaceRoot, relPath),
			RelPath:  relPath,
		})
		removed = append(removed, relPath)
	}
	return ops, removed, nil
}

func (e *Engine) validateCopiedDirectory(absPath, relPath string, ownership state.PathOwnership) error {
	if ownership.Contents == nil {
		return fmt.Errorf("%w: %w: copied directory %s has no ownership manifest; %s", ErrValidation, ErrDrift, relPath, forceUnapplyHint)
	}

	current, err := e.copyDirFileChecksums(absPath)
	if err != nil {
		return fmt.Errorf("%w: failed to verify copied directory checksums: %w", ErrValidation, err)
	}

	expected := ownership.Contents.Files
	if expected == nil {
		expected = map[string]string{}
	}

	var added, modified, deleted []string
	for rel, checksum := range current {
		workspaceRel := dirLeafPath(relPath, rel)
		want, ok := expected[rel]
		if !ok {
			added = append(added, workspaceRel)
			continue
		}
		if want != checksum {
			modified = append(modified, workspaceRel)
		}
	}
	for rel := range expected {
		if _, ok := current[rel]; !ok {
			deleted = append(deleted, dirLeafPath(relPath, rel))
		}
	}
	if len(added) == 0 && len(modified) == 0 && len(deleted) == 0 {
		return nil
	}

	sort.Strings(added)
	sort.Strings(modified)
	sort.Strings(deleted)
	return fmt.Errorf("%w: %w: %s", ErrValidation, ErrDrift, formatCopiedDirectoryDrift(relPath, added, modified, deleted))
}

func dirLeafPath(dirRel, leaf string) string {
	dirRel = filepath.ToSlash(dirRel)
	leaf = filepath.ToSlash(leaf)
	if dirRel == "" || dirRel == "." {
		return leaf
	}
	return path.Join(dirRel, leaf)
}

func formatCopiedDirectoryDrift(relPath string, added, modified, deleted []string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "copied directory %s has local changes", relPath)
	appendDriftList(&b, "added", added)
	appendDriftList(&b, "modified", modified)
	appendDriftList(&b, "deleted", deleted)
	fmt.Fprintf(&b, "\n%s", forceUnapplyHint)
	return b.String()
}

func appendDriftList(b *strings.Builder, label string, paths []string) {
	if len(paths) == 0 {
		return
	}
	fmt.Fprintf(b, "\n  %s: %s", label, strings.Join(paths, ", "))
}
