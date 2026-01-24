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
			// Compute source and destination paths
			sourcePath := filepath.Join(overlayRoot, trackedPath.Path)
			destPath := filepath.Join(workspaceRoot, trackedPath.Path)

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

			// Check for conflicts
			conflict := checker.CheckPath(destPath, pathType, mode, storeID)
			if conflict != nil {
				plan.AddConflict(*conflict)
				continue
			}

			// Check if this path was already claimed by an earlier store
			if previousStore, exists := pathOwners[destPath]; exists {
				// Later store takes precedence - add remove operation first
				removeOp := Operation{
					Type:       OpRemove,
					SourcePath: "",
					DestPath:   destPath,
					Store:      previousStore,
				}
				plan.AddOperation(removeOp)
			}

			// Add the create operation
			var op Operation
			if mode == "symlink" {
				op = Operation{
					Type:       OpCreateSymlink,
					SourcePath: sourcePath,
					DestPath:   destPath,
					Store:      storeID,
				}
			} else {
				op = Operation{
					Type:       OpCopy,
					SourcePath: sourcePath,
					DestPath:   destPath,
					Store:      storeID,
				}
			}
			plan.AddOperation(op)

			// Mark this path as claimed by this store
			pathOwners[destPath] = storeID
		}
	}

	return plan, nil
}
