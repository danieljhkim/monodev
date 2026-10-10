package planner

import (
	"fmt"
	"path/filepath"
	"sort"

	"github.com/danieljhkim/monodev/internal/fsops"
	"github.com/danieljhkim/monodev/internal/state"
	"github.com/danieljhkim/monodev/internal/stores"
)

// pathClaim is one tracked path already accepted into this plan.
type pathClaim struct {
	store string
	kind  string
}

// BuildApplyPlan generates a deterministic plan to apply store overlays.
func BuildApplyPlan(
	workspace *state.WorkspaceState,
	orderedStores []string,
	mode string,
	repoRoot string,
	storeRepo stores.StoreRepo,
	fs fsops.FS,
	force bool,
) (*ApplyPlan, error) {
	plan := NewApplyPlan(orderedStores)
	checker := NewConflictChecker(fs, workspace, force)

	// applyRoot is where tracked paths will be placed.
	// For subdirectory workspaces, paths are applied relative to the workspace dir.
	applyRoot := filepath.Join(repoRoot, workspace.WorkspacePath)

	// Track which paths have been claimed by which stores.
	// Identical paths use later-store precedence. A copied directory and a
	// nested path are both kept: the more specific path wins that subtree.
	pathOwners := make(map[string]pathClaim)

	// For each store in order
	for _, storeID := range orderedStores {
		// Load the track file for this store
		track, err := storeRepo.LoadTrack(storeID)
		if err != nil {
			return nil, fmt.Errorf("failed to load track file for store %s: %w", storeID, err)
		}

		// Get the overlay root for this store
		overlayRoot := storeRepo.OverlayRoot(storeID)

		// For each tracked path in this store
		for _, trackedPath := range track.Tracked {
			// trackedPath.Path is workspace-relative (relative to the workspace root)
			relPath := trackedPath.Path

			// Validate relative path for safety to prevent path traversal
			if err := fs.ValidateRelPath(relPath); err != nil {
				return nil, fmt.Errorf("invalid tracked path %q in store %s: %w", relPath, storeID, err)
			}

			// Compute absolute source and destination paths for FS operations
			sourcePath := filepath.Join(overlayRoot, relPath)
			destPath := filepath.Join(applyRoot, relPath)
			if err := fsops.ValidatePathOutsideGitDir(repoRoot, destPath); err != nil {
				return nil, fmt.Errorf("invalid tracked path %q in store %s: %w", relPath, storeID, err)
			}

			// Check if source path exists in store
			sourceExists, err := fs.Exists(sourcePath)
			if err != nil {
				return nil, fmt.Errorf("failed to check source path %s: %w", sourcePath, err)
			}
			if !sourceExists {
				// Warn and skip paths that don't exist in the store overlay
				plan.AddWarning(fmt.Sprintf("tracked path %s not found in store %s (skipping)", trackedPath.Path, storeID))
				continue
			}

			// Use the kind from the tracked path metadata
			pathType := "file"
			if trackedPath.Kind == "dir" {
				pathType = "directory"
			}

			// Check for conflicts (checker now works with relative paths)
			conflict := checker.CheckPath(relPath, destPath, pathType, mode, storeID)
			if conflict != nil {
				plan.AddConflict(*conflict)
				continue
			}

			conflicts, coversDescendants := hierarchyForPath(pathOwners, relPath, pathType, mode, storeID)
			if len(conflicts) > 0 {
				for _, conflict := range conflicts {
					plan.AddConflict(conflict)
				}
				continue
			}

			var prelude []Operation
			if previous, exists := pathOwners[relPath]; exists {
				// Later store takes the identical path. Nested paths claimed
				// under a replaced directory stay and are installed after it.
				prelude = append(prelude, Operation{
					Type:       OpRemove,
					SourcePath: "",
					DestPath:   destPath,
					RelPath:    relPath,
					Store:      previous.store,
				})
				delete(pathOwners, relPath)
			} else if force {
				// When force is enabled, check if destination exists (unmanaged or from previous apply)
				// If so, we need to remove it first before creating the new overlay
				destExists, err := fs.Exists(destPath)
				if err == nil && destExists {
					prelude = append(prelude, Operation{
						Type:       OpRemove,
						SourcePath: "",
						DestPath:   destPath,
						RelPath:    relPath,
						Store:      "", // unknown/unmanaged
					})
				}
			}

			// Add the create operation
			var op Operation
			if mode == "symlink" {
				op = Operation{
					Type:       OpCreateSymlink,
					SourcePath: sourcePath,
					DestPath:   destPath,
					RelPath:    relPath,
					Store:      storeID,
				}
			} else {
				op = Operation{
					Type:       OpCopy,
					SourcePath: sourcePath,
					DestPath:   destPath,
					RelPath:    relPath,
					Store:      storeID,
				}
			}
			if coversDescendants {
				for _, pre := range prelude {
					plan.insertBeforeDescendants(pre)
				}
				plan.insertBeforeDescendants(op)
			} else {
				for _, pre := range prelude {
					plan.AddOperation(pre)
				}
				plan.AddOperation(op)
			}

			// Mark this path as claimed by this store (use relative path)
			pathOwners[relPath] = pathClaim{store: storeID, kind: pathType}
		}
	}

	return plan, nil
}

// hierarchyForPath decides how relPath relates to paths already in the plan.
// A copied directory may contain another store's path. Every other nesting
// (a path inside a file, or any nesting in symlink mode) is a blocking
// conflict. coversDescendants is true when relPath is a copied directory that
// must be installed before those nested paths.
func hierarchyForPath(owners map[string]pathClaim, relPath, pathType, mode, storeID string) (conflicts []Conflict, coversDescendants bool) {
	keys := make([]string, 0, len(owners))
	for key := range owners {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	for _, key := range keys {
		claim := owners[key]
		if key == relPath {
			continue
		}
		switch {
		case IsStrictDescendant(relPath, key):
			if mode == "copy" && claim.kind == "directory" {
				continue
			}
			conflicts = append(conflicts, hierarchyConflict(relPath, pathType, storeID, key, claim, mode))
		case IsStrictDescendant(key, relPath):
			if mode == "copy" && pathType == "directory" {
				coversDescendants = true
				continue
			}
			conflicts = append(conflicts, hierarchyConflict(relPath, pathType, storeID, key, claim, mode))
		}
	}
	return conflicts, coversDescendants
}

func hierarchyConflict(relPath, pathType, storeID, otherRel string, claim pathClaim, mode string) Conflict {
	reason := fmt.Sprintf("path %s from store %s overlaps %s %s from store %s", relPath, storeID, claim.kind, otherRel, claim.store)
	existing := claim.kind
	incoming := pathType
	if mode == "symlink" && (pathType == "directory" || claim.kind == "directory") {
		reason += "; symlink apply cannot share a nested path between stores"
		if claim.kind == "directory" {
			existing = "directory-symlink"
		}
		if pathType == "directory" {
			incoming = "directory-symlink"
		}
	} else {
		reason += "; only a copied directory can contain another store's path"
	}
	return Conflict{
		Path:     relPath,
		Reason:   reason,
		Existing: existing,
		Incoming: incoming,
		Blocking: true,
	}
}
