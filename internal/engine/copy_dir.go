package engine

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/danieljhkim/monodev/internal/hash"
	"github.com/danieljhkim/monodev/internal/planner"
	"github.com/danieljhkim/monodev/internal/state"
)

const forceUnapplyHint = "inspect the listed paths or re-run with --force to remove the directory including local changes"

// ownershipForAppliedPath records store ownership after an overlay operation.
// Copy-mode files get a checksum; copy-mode directories get a leaf manifest.
// owners is the plan's final path-to-store map. Nested paths owned by another
// store are omitted from a directory manifest so unapply of the directory does
// not treat them as this store's files.
func (e *Engine) ownershipForAppliedPath(op planner.Operation, mode string, owners map[string]string) state.PathOwnership {
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
			filterForeignLeaves(op.RelPath, op.Store, files, owners)
			ownership.Contents = &state.DirContents{Files: files}
		}
		return ownership
	}

	checksum, err := hash.HashManagedFile(e.hasher, op.DestPath)
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
		checksum, err := hash.HashManagedFile(e.hasher, pathName)
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
			if err := e.validateManagedPath(absPath, relPath, workspaceState.Paths[relPath], workspaceState.Paths); err != nil {
				return nil, nil, fmt.Errorf("validation failed for %s: %w", relPath, err)
			}
		}
	}

	removing := make(map[string]bool, len(relPaths))
	for _, relPath := range relPaths {
		removing[planner.NormalizeTrackedPath(relPath)] = true
	}

	ops := make([]planner.Operation, 0, len(relPaths))
	removed := make([]string, 0, len(relPaths))
	for _, relPath := range relPaths {
		if err := e.fs.ValidateRelPath(relPath); err != nil {
			return nil, nil, fmt.Errorf("invalid path %q in workspace state: %w", relPath, err)
		}
		preserved := preservedDescendants(relPath, workspaceState.Paths, removing)
		if len(preserved) == 0 {
			ops = append(ops, planner.Operation{
				Type:     planner.OpRemove,
				DestPath: filepath.Join(workspaceRoot, relPath),
				RelPath:  relPath,
			})
		} else {
			partial, err := e.removalOpsPreserving(workspaceRoot, relPath, preserved, workspaceState.Paths)
			if err != nil {
				return nil, nil, err
			}
			ops = append(ops, partial...)
		}
		removed = append(removed, relPath)
	}
	return ops, removed, nil
}

// preservedDescendants lists ledger paths inside relPath that this removal
// must leave on disk because another operation is not deleting them.
func preservedDescendants(relPath string, paths map[string]state.PathOwnership, removing map[string]bool) []string {
	var preserved []string
	for rel := range paths {
		normalized := planner.NormalizeTrackedPath(rel)
		if removing[normalized] || normalized == planner.NormalizeTrackedPath(relPath) {
			continue
		}
		if planner.IsStrictDescendant(normalized, relPath) {
			preserved = append(preserved, normalized)
		}
	}
	sort.Strings(preserved)
	return preserved
}

// removalOpsPreserving deletes files owned by relPath while keeping preserved
// ledger paths and the directories that contain them. A non-directory cannot
// be trimmed around a nested path, so that overlap conflicts before mutation.
func (e *Engine) removalOpsPreserving(workspaceRoot, relPath string, preserved []string, paths map[string]state.PathOwnership) ([]planner.Operation, error) {
	abs := filepath.Join(workspaceRoot, filepath.FromSlash(relPath))
	info, err := os.Lstat(abs)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to inspect %s: %w", relPath, err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		other := preserved[0]
		owner := paths[other].Store
		if owner == "" {
			owner = "another store"
		}
		return nil, fmt.Errorf("%w: cannot unapply %s while %s is still owned by %s", ErrConflict, relPath, other, owner)
	}

	var doomed []string
	walkErr := filepath.WalkDir(abs, func(pathName string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if pathName == abs {
			return nil
		}
		rel, relErr := filepath.Rel(workspaceRoot, pathName)
		if relErr != nil {
			return relErr
		}
		rel = planner.NormalizeTrackedPath(rel)
		if keepsPreservedPath(rel, preserved) {
			return nil
		}
		doomed = append(doomed, rel)
		if entry.IsDir() {
			return filepath.SkipDir
		}
		return nil
	})
	if walkErr != nil {
		return nil, fmt.Errorf("failed to plan removal of %s: %w", relPath, walkErr)
	}
	sortDeepestFirst(doomed)

	ops := make([]planner.Operation, 0, len(doomed))
	for _, rel := range doomed {
		ops = append(ops, planner.Operation{
			Type:     planner.OpRemove,
			DestPath: filepath.Join(workspaceRoot, filepath.FromSlash(rel)),
			RelPath:  rel,
		})
	}
	return ops, nil
}

func keepsPreservedPath(rel string, preserved []string) bool {
	rel = planner.NormalizeTrackedPath(rel)
	for _, preservedPath := range preserved {
		preservedPath = planner.NormalizeTrackedPath(preservedPath)
		if rel == preservedPath || planner.IsStrictDescendant(rel, preservedPath) || planner.IsStrictDescendant(preservedPath, rel) {
			return true
		}
	}
	return false
}

func (e *Engine) validateCopiedDirectory(absPath, relPath string, ownership state.PathOwnership, ledger map[string]state.PathOwnership) error {
	if ownership.Contents == nil {
		return fmt.Errorf("%w: %w: copied directory %s has no ownership manifest; %s", ErrValidation, ErrDrift, relPath, forceUnapplyHint)
	}

	current, err := e.copyDirFileChecksums(absPath)
	if err != nil {
		return fmt.Errorf("%w: failed to verify copied directory checksums: %w", ErrValidation, err)
	}

	owners := ownersFromLedger(ledger)
	expectedSrc := ownership.Contents.Files
	if expectedSrc == nil {
		expectedSrc = map[string]string{}
	}
	expected := make(map[string]string, len(expectedSrc))
	for rel, checksum := range expectedSrc {
		if leafCoveredByOtherStore(relPath, rel, ownership.Store, owners) {
			continue
		}
		expected[rel] = checksum
	}

	var added, modified, deleted []string
	for rel, checksum := range current {
		if leafCoveredByOtherStore(relPath, rel, ownership.Store, owners) {
			continue
		}
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

func ownersFromLedger(paths map[string]state.PathOwnership) map[string]string {
	owners := make(map[string]string, len(paths))
	for rel, ownership := range paths {
		owners[rel] = ownership.Store
	}
	return owners
}

// filterForeignLeaves drops files owned by a store other than dirStore.
func filterForeignLeaves(dirRel, dirStore string, files map[string]string, owners map[string]string) {
	for leaf := range files {
		if leafCoveredByOtherStore(dirRel, leaf, dirStore, owners) {
			delete(files, leaf)
		}
	}
}

// leafCoveredByOtherStore reports whether the file at leaf inside dirRel is
// covered by a different store's ledger path that is itself inside the directory.
func leafCoveredByOtherStore(dirRel, leaf, dirStore string, owners map[string]string) bool {
	if len(owners) == 0 {
		return false
	}
	full := planner.NormalizeTrackedPath(dirLeafPath(dirRel, leaf))
	dirRel = planner.NormalizeTrackedPath(dirRel)
	for ownedRel, store := range owners {
		if store == "" || store == dirStore {
			continue
		}
		ownedRel = planner.NormalizeTrackedPath(ownedRel)
		if ownedRel == dirRel {
			continue
		}
		if ownedRel != full && !planner.IsStrictDescendant(ownedRel, dirRel) {
			continue
		}
		if ownedRel == full || planner.IsStrictDescendant(full, ownedRel) {
			return true
		}
	}
	return false
}

// pruneManifestsUnderRemoved drops directory-manifest leaves that belonged to
// paths just unapplied, so a remaining ancestor does not report them as deleted.
func pruneManifestsUnderRemoved(paths map[string]state.PathOwnership, removed []string) {
	if len(removed) == 0 {
		return
	}
	for rel, ownership := range paths {
		if ownership.Contents == nil || len(ownership.Contents.Files) == 0 {
			continue
		}
		changed := false
		for leaf := range ownership.Contents.Files {
			full := planner.NormalizeTrackedPath(dirLeafPath(rel, leaf))
			if manifestLeafRemoved(full, removed) {
				delete(ownership.Contents.Files, leaf)
				changed = true
			}
		}
		if changed {
			paths[rel] = ownership
		}
	}
}

func manifestLeafRemoved(full string, removed []string) bool {
	full = planner.NormalizeTrackedPath(full)
	for _, rel := range removed {
		rel = planner.NormalizeTrackedPath(rel)
		if full == rel || planner.IsStrictDescendant(full, rel) {
			return true
		}
	}
	return false
}

func appendDriftList(b *strings.Builder, label string, paths []string) {
	if len(paths) == 0 {
		return
	}
	fmt.Fprintf(b, "\n  %s: %s", label, strings.Join(paths, ", "))
}
