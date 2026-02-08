package engine

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/danieljhkim/monodev/internal/state"
	"github.com/danieljhkim/monodev/internal/stores"
)

// Status returns the current status of the workspace.
func (e *Engine) Status(ctx context.Context, req *StatusRequest) (*StatusResult, error) {
	root, repoFingerprint, workspacePath, err := e.DiscoverWorkspace(req.CWD)
	if err != nil {
		return nil, fmt.Errorf("failed to discover workspace: %w", err)
	}

	// Get fingerprint components (absolute path and git URL)
	absPath, gitURL, err := e.gitRepo.GetFingerprintComponents(root)
	if err != nil {
		return nil, fmt.Errorf("failed to get fingerprint components: %w", err)
	}

	workspaceID := state.ComputeWorkspaceID(repoFingerprint, workspacePath)

	// Load workspace state (may not exist)
	workspaceState, err := e.stateStore.LoadWorkspace(workspaceID)
	if err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("failed to load workspace state: %w", err)
	}

	// Build result from workspace state if it exists
	result := &StatusResult{
		WorkspaceID:     workspaceID,
		RepoFingerprint: repoFingerprint,
		WorkspacePath:   workspacePath,
		AbsolutePath:    absPath,
		GitURL:          gitURL,
		Applied:         false,
		Mode:            "",
		Stack:           []string{},
		ActiveStore:     "",
		Paths:           make(map[string]PathInfo),
		TrackedPaths:    []string{},
	}

	// If workspace state exists, populate from it
	if workspaceState != nil {
		result.Applied = workspaceState.Applied
		result.Mode = workspaceState.Mode
		result.Stack = workspaceState.Stack
		result.ActiveStore = workspaceState.ActiveStore

		// Convert paths to PathInfo
		for path, ownership := range workspaceState.Paths {
			result.Paths[path] = PathInfo{
				Store: ownership.Store,
				Type:  ownership.Type,
			}
		}

		// Compute AppliedStoreDetails
		result.AppliedStoreDetails = e.computeAppliedStoreDetails(workspaceState)
	}

	// Load tracked paths from active store
	if result.ActiveStore != "" {
		repo, repoErr := e.activeStoreRepo(workspaceState)
		if repoErr == nil {
			track, err := repo.LoadTrack(result.ActiveStore)
			if err == nil {
				result.TrackedPaths = track.Paths()

				// Compute TrackedPathDetails
				result.TrackedPathDetails = e.computeTrackedPathDetails(repo, result.ActiveStore, track.Paths(), workspaceState)

				// Compute ActiveStoreStatus
				result.ActiveStoreStatus = e.computeActiveStoreStatus(track.Paths(), result.TrackedPathDetails)
			} else {
				result.ActiveStoreStatus = "Not Applied"
			}
		} else {
			result.ActiveStoreStatus = "Not Applied"
		}
	} else {
		result.ActiveStoreStatus = "Not Applied"
	}

	return result, nil
}

// computeAppliedStoreDetails computes per-store applied path counts.
func (e *Engine) computeAppliedStoreDetails(workspaceState *state.WorkspaceState) []AppliedStoreInfo {
	// Build a set of all unique stores (stack + active store)
	storeSet := make(map[string]bool)
	for _, storeID := range workspaceState.Stack {
		storeSet[storeID] = true
	}
	if workspaceState.ActiveStore != "" {
		storeSet[workspaceState.ActiveStore] = true
	}

	// Count paths per store
	storeCounts := make(map[string]int)
	storeModes := make(map[string]string)

	for _, ownership := range workspaceState.Paths {
		storeCounts[ownership.Store]++
		// Capture the mode (should be consistent per store)
		if _, exists := storeModes[ownership.Store]; !exists {
			storeModes[ownership.Store] = ownership.Type
		}
	}

	// Build result in stack order, then active store
	var details []AppliedStoreInfo

	// Add stack stores first
	for _, storeID := range workspaceState.Stack {
		if count, hasCount := storeCounts[storeID]; hasCount && count > 0 {
			details = append(details, AppliedStoreInfo{
				StoreID:      storeID,
				Mode:         storeModes[storeID],
				AppliedCount: count,
			})
		}
	}

	// Add active store if not already in stack
	if workspaceState.ActiveStore != "" {
		alreadyInStack := false
		for _, storeID := range workspaceState.Stack {
			if storeID == workspaceState.ActiveStore {
				alreadyInStack = true
				break
			}
		}
		if !alreadyInStack {
			if count, hasCount := storeCounts[workspaceState.ActiveStore]; hasCount && count > 0 {
				details = append(details, AppliedStoreInfo{
					StoreID:      workspaceState.ActiveStore,
					Mode:         storeModes[workspaceState.ActiveStore],
					AppliedCount: count,
				})
			}
		}
	}

	return details
}

// computeTrackedPathDetails computes detailed info for tracked paths.
func (e *Engine) computeTrackedPathDetails(repo stores.StoreRepo, activeStoreID string, trackedPaths []string, workspaceState *state.WorkspaceState) []TrackedPathInfo {
	var details []TrackedPathInfo

	overlayRoot := repo.OverlayRoot(activeStoreID)

	// Load track file to get path kinds (file vs dir)
	track, _ := repo.LoadTrack(activeStoreID)
	pathKindMap := make(map[string]string)
	if track != nil {
		for _, tp := range track.Tracked {
			pathKindMap[tp.Path] = tp.Kind
		}
	}

	for _, trackedPath := range trackedPaths {
		pathInfo := TrackedPathInfo{
			Path:       trackedPath,
			IsApplied:  false,
			IsSaved:    false,
			IsModified: false,
		}

		// Check if applied (exists in workspace.Paths)
		if workspaceState != nil {
			if _, exists := workspaceState.Paths[trackedPath]; exists {
				if workspaceState.Paths[trackedPath].Store == activeStoreID {
					pathInfo.IsApplied = true
				}
			}
		}

		// Check if saved (exists in store overlay)
		pathInOverlay := filepath.Join(overlayRoot, trackedPath)
		exists, err := e.fs.Exists(pathInOverlay)
		if err == nil && exists {
			pathInfo.IsSaved = true
		}

		// Check if modified by comparing workspace and store overlay
		pathInfo.IsModified = e.isPathModified(trackedPath, overlayRoot, pathKindMap[trackedPath])

		details = append(details, pathInfo)
	}

	return details
}

// isPathModified checks if a tracked path is modified in the workspace compared to the store overlay.
func (e *Engine) isPathModified(trackedPath, overlayRoot, kind string) bool {
	// Get workspace root
	cwd, _ := os.Getwd()
	root, _, _, err := e.DiscoverWorkspace(cwd)
	if err != nil {
		return false
	}

	workspacePath := filepath.Join(root, trackedPath)
	storePath := filepath.Join(overlayRoot, trackedPath)

	if kind == "dir" {
		// For directories, check if any files within are modified
		dirFiles, err := e.compareDirPath(root, overlayRoot, workspacePath, storePath, trackedPath, false)
		if err != nil {
			return false
		}
		for _, file := range dirFiles {
			if file.Status == "modified" || file.Status == "added" || file.Status == "removed" {
				return true
			}
		}
		return false
	}

	// For files, use comparePath
	fileInfo := e.comparePath(workspacePath, storePath, trackedPath, kind, false)
	return fileInfo.Status == "modified" || fileInfo.Status == "added" || fileInfo.Status == "removed"
}

// computeActiveStoreStatus determines the application status of the active store.
func (e *Engine) computeActiveStoreStatus(trackedPaths []string, details []TrackedPathInfo) string {
	if len(trackedPaths) == 0 {
		return "Not Applied"
	}

	appliedCount := 0
	for _, detail := range details {
		if detail.IsApplied {
			appliedCount++
		}
	}

	if appliedCount == len(trackedPaths) {
		return "Applied"
	} else if appliedCount == 0 {
		return "Not Applied"
	}

	return "Partial"
}
