package engine

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"

	"github.com/danieljhkim/monodev/internal/lockfile"
	"github.com/danieljhkim/monodev/internal/state"
)

// Unapply removes paths owned by the requested stores from the workspace.
// With no StoreIDs, it removes paths owned by the active store.
//
// Algorithm:
//  1. Discover repo and load workspace state (must exist)
//  2. Collect paths owned by the requested stores
//  3. Remove paths in deepest-first order
//  4. Delete workspace state when no managed paths and no active store remain
func (e *Engine) Unapply(ctx context.Context, req *UnapplyRequest) (*UnapplyResult, error) {
	if err := checkContext(ctx); err != nil {
		return nil, err
	}

	root, repoFingerprint, workspacePath, err := e.DiscoverWorkspace(req.CWD)
	if err != nil {
		return nil, fmt.Errorf("failed to discover workspace: %w", err)
	}
	if err := checkContext(ctx); err != nil {
		return nil, err
	}

	workspaceID := state.ComputeWorkspaceID(repoFingerprint, workspacePath)
	unlockWorkspace, err := e.lockWorkspace(ctx, workspaceID, lockfile.Exclusive)
	if err != nil {
		return nil, err
	}
	defer unlockWorkspace()

	workspaceState, err := e.stateStore.LoadWorkspace(workspaceID)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("%w: workspace has no managed paths", ErrStateMissing)
		}
		return nil, fmt.Errorf("failed to load workspace state: %w", err)
	}
	workspaceState.AbsolutePath = filepath.Join(root, workspacePath)
	workspaceState.MigrateDeprecatedStack()
	if err := checkContext(ctx); err != nil {
		return nil, err
	}

	workspaceRoot := filepath.Join(root, workspacePath)
	var warnings []string
	if !req.DryRun {
		recoveryWarnings, recoverErr := e.recoverWorkspaceOverlay(ctx, workspaceID, root, workspaceRoot, workspacePath)
		if recoverErr != nil {
			return nil, recoverErr
		}
		warnings = append(warnings, recoveryWarnings...)
		reloaded, reloadErr := e.stateStore.LoadWorkspace(workspaceID)
		if reloadErr != nil {
			if os.IsNotExist(reloadErr) {
				return nil, fmt.Errorf("%w: workspace has no managed paths", ErrStateMissing)
			}
			return nil, fmt.Errorf("failed to reload workspace state: %w", reloadErr)
		}
		workspaceState = reloaded
		workspaceState.AbsolutePath = workspaceRoot
		workspaceState.MigrateDeprecatedStack()
	}

	if req.All && len(req.StoreIDs) > 0 {
		return nil, fmt.Errorf("--all cannot be combined with store IDs")
	}

	storesToRemove := req.StoreIDs
	if !req.All && len(storesToRemove) == 0 {
		if workspaceState.ActiveStore == "" {
			return &UnapplyResult{
				Removed:     []string{},
				WorkspaceID: workspaceID,
				Warnings:    warnings,
				message:     "nothing to remove",
			}, nil
		}
		storesToRemove = []string{workspaceState.ActiveStore}
	}
	storeSet := make(map[string]bool, len(storesToRemove))
	for _, storeID := range storesToRemove {
		storeSet[storeID] = true
	}

	ownedPaths := []string{}
	for relPath, ownership := range workspaceState.Paths {
		if req.All || storeSet[ownership.Store] {
			ownedPaths = append(ownedPaths, relPath)
		}
	}
	sortDeepestFirst(ownedPaths)

	if len(ownedPaths) == 0 {
		return &UnapplyResult{
			Removed:     []string{},
			WorkspaceID: workspaceID,
			Warnings:    warnings,
			message:     "nothing to remove",
		}, nil
	}

	ops, removed, err := e.planManagedPathRemoval(workspaceRoot, workspaceState, ownedPaths, req.Force)
	if err != nil {
		return nil, err
	}

	if req.DryRun {
		return &UnapplyResult{
			Removed:     removed,
			WorkspaceID: workspaceID,
			Warnings:    warnings,
			message:     "dry run",
		}, nil
	}

	if err := checkContext(ctx); err != nil {
		return nil, err
	}

	final := state.CloneWorkspaceState(workspaceState)
	for _, relPath := range removed {
		delete(final.Paths, relPath)
	}
	pruneManifestsUnderRemoved(final.Paths, removed)
	// Keep the state file while it still records an active store, so a bare
	// `apply` after a full unapply re-applies the same store.
	deleteState := len(final.Paths) == 0 && final.ActiveStore == ""
	if !deleteState {
		final.Applied = len(final.Paths) > 0
		final.PruneAppliedStores()
	}

	if err := e.runOverlayTxn(ctx, overlayTxnRequest{
		kind:          overlayTxnUnapply,
		workspaceID:   workspaceID,
		workspaceRoot: workspaceRoot,
		ops:           ops,
		finalize: func() (*state.WorkspaceState, bool, error) {
			if deleteState {
				return nil, true, nil
			}
			return final, false, nil
		},
	}); err != nil {
		return nil, err
	}
	currentExcludeState := final
	if deleteState {
		currentExcludeState = nil
	}
	warnings = appendExcludeWarning(warnings, e.syncManagedExcludes(ctx, root, workspaceID, workspacePath, currentExcludeState))

	return &UnapplyResult{
		Removed:     removed,
		WorkspaceID: workspaceID,
		Warnings:    warnings,
	}, nil
}

// validateManagedPath validates that a path is still managed by monodev.
// ledger is the full ownership map. Files owned by another store inside a
// copied directory are not drift in this path.
func (e *Engine) validateManagedPath(absPath, relPath string, ownership state.PathOwnership, ledger map[string]state.PathOwnership) error {
	exists, err := e.fs.Exists(absPath)
	if err != nil {
		return fmt.Errorf("failed to check if path exists: %w", err)
	}
	if !exists {
		return nil
	}

	info, err := e.fs.Lstat(absPath)
	if err != nil {
		return fmt.Errorf("failed to stat path: %w", err)
	}

	if ownership.Type != "copy" {
		if ownership.Type == "symlink" {
			return e.validateManagedSymlink(absPath, relPath, ownership, info)
		}
		return nil
	}
	if info.IsDir() || ownership.Contents != nil {
		if !info.IsDir() {
			return fmt.Errorf("%w: %w: copied directory %s is no longer a directory; %s", ErrValidation, ErrDrift, relPath, forceUnapplyHint)
		}
		return e.validateCopiedDirectory(absPath, relPath, ownership, ledger)
	}

	if ownership.Checksum == "" {
		return nil
	}
	currentHash, err := e.hasher.HashFile(absPath)
	if err != nil {
		return fmt.Errorf("%w: failed to verify copy checksum: %w", ErrValidation, err)
	}
	if currentHash != ownership.Checksum {
		return fmt.Errorf("%w: %w: local modifications detected", ErrValidation, ErrDrift)
	}

	return nil
}

// validateManagedSymlink checks that a symlink-owned path is still the link
// that apply created. Content changes behind an intact link are not drift, but
// replacing the link with a file, directory or a link to somewhere else is.
// The expected target is the owning store's overlay path; when the store can no
// longer be resolved only the "still a symlink" check applies.
func (e *Engine) validateManagedSymlink(absPath, relPath string, ownership state.PathOwnership, info os.FileInfo) error {
	if info.Mode()&os.ModeSymlink == 0 {
		return fmt.Errorf("%w: %w: managed symlink %s was replaced by user content; %s", ErrValidation, ErrDrift, relPath, forceUnapplyHint)
	}

	expected := e.expectedSymlinkTargets(ownership.Store, relPath)
	if len(expected) == 0 {
		return nil
	}
	target, err := e.fs.Readlink(absPath)
	if err != nil {
		return fmt.Errorf("failed to read symlink %s: %w", relPath, err)
	}
	if !slices.Contains(expected, filepath.Clean(target)) {
		return fmt.Errorf("%w: %w: managed symlink %s now points to %s; %s", ErrValidation, ErrDrift, relPath, target, forceUnapplyHint)
	}
	return nil
}

// expectedSymlinkTargets returns the overlay paths a symlink-mode apply of
// storeID could have linked relPath to, one per location of the store.
func (e *Engine) expectedSymlinkTargets(storeID, relPath string) []string {
	if e.storeResolver == nil {
		return nil
	}
	locations, err := e.storeResolver.findStore(storeID)
	if err != nil {
		return nil
	}
	targets := make([]string, 0, len(locations))
	for _, loc := range locations {
		targets = append(targets, filepath.Clean(filepath.Join(loc.Repo.OverlayRoot(storeID), relPath)))
	}
	return targets
}

// countPathSeparators counts the number of path separators in a path.
func countPathSeparators(path string) int {
	count := 0
	for _, ch := range path {
		if ch == '/' || ch == '\\' {
			count++
		}
	}
	return count
}
