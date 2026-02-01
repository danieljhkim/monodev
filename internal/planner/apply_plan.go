package planner

import (
	"fmt"
	"path/filepath"

	"github.com/danieljhkim/monodev/internal/fsops"
	"github.com/danieljhkim/monodev/internal/state"
	"github.com/danieljhkim/monodev/internal/stores"
)

// BuildApplyPlan generates a deterministic plan to apply store overlays.
func BuildApplyPlan(
	workspace *state.WorkspaceState,
	orderedStores []string,
	mode string,
	workspaceRoot string,
	storeRepo stores.StoreRepo,
	fs fsops.FS,
	force bool,
) (*ApplyPlan, error) {
	plan := NewApplyPlan(orderedStores)
	checker := NewConflictChecker(fs, workspace, force)

	// Track which paths have been claimed by which stores
	// This helps with store-to-store precedence
	pathOwners := make(map[string]string)

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
			// trackedPath.Path is already relative to workspace root
			relPath := trackedPath.Path

			// Validate relative path for safety to prevent path traversal
			if err := fs.ValidateRelPath(relPath); err != nil {
				return nil, fmt.Errorf("invalid tracked path %q in store %s: %w", relPath, storeID, err)
			}

			// Compute absolute source and destination paths for FS operations
			sourcePath := filepath.Join(overlayRoot, relPath)
			destPath := filepath.Join(workspaceRoot, relPath)

			// Check if source path exists in store
			sourceExists, err := fs.Exists(sourcePath)
			if err != nil {
				return nil, fmt.Errorf("failed to check source path %s: %w", sourcePath, err)
			}
			if !sourceExists {
				// Skip paths that don't exist in the store (unless required)
				// This can happen if a path is tracked but not yet saved
				if trackedPath.IsRequired() {
					return nil, fmt.Errorf("required path %s not found in store %s", trackedPath.Path, storeID)
				}
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

			// Check if this path was already claimed by an earlier store
			// Use relPath as the key for tracking ownership
			if previousStore, exists := pathOwners[relPath]; exists {
				// Later store takes precedence - add remove operation first
				removeOp := Operation{
					Type:       OpRemove,
					SourcePath: "",
					DestPath:   destPath,
					RelPath:    relPath,
					Store:      previousStore,
				}
				plan.AddOperation(removeOp)
			} else if force {
				// When force is enabled, check if destination exists (unmanaged or from previous apply)
				// If so, we need to remove it first before creating the new overlay
				destExists, err := fs.Exists(destPath)
				if err == nil && destExists {
					removeOp := Operation{
						Type:       OpRemove,
						SourcePath: "",
						DestPath:   destPath,
						RelPath:    relPath,
						Store:      "", // unknown/unmanaged
					}
					plan.AddOperation(removeOp)
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
			plan.AddOperation(op)

			// Mark this path as claimed by this store (use relative path)
			pathOwners[relPath] = storeID
		}
	}

	return plan, nil
}
