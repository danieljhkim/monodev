package engine

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/danieljhkim/monodev/internal/stores"
)

// Diff compares workspace files against store overlay files.
func (e *Engine) Diff(ctx context.Context, req *DiffRequest) (*DiffResult, error) {
	// Discover workspace
	root, fingerprint, workspacePath, err := e.DiscoverWorkspace(req.CWD)
	if err != nil {
		return nil, err
	}

	// Load or create workspace state
	workspaceState, workspaceID, err := e.LoadOrCreateWorkspaceState(fingerprint, workspacePath, "copy")
	if err != nil {
		return nil, err
	}

	// Determine which store to diff against
	storeID := req.StoreID
	if storeID == "" {
		storeID = workspaceState.ActiveStore
		if storeID == "" {
			return nil, ErrNoActiveStore
		}
	}

	// Resolve store repo (use active store scope or search both)
	var repo stores.StoreRepo
	if storeID == workspaceState.ActiveStore && workspaceState.ActiveStoreScope != "" {
		repo, err = e.storeRepoForScope(workspaceState.ActiveStoreScope)
		if err != nil {
			return nil, err
		}
	} else {
		repo, _, err = e.resolveStoreRepo(storeID, "")
		if err != nil {
			return nil, err
		}
	}

	// Load tracked paths from store
	trackFile, err := repo.LoadTrack(storeID)
	if err != nil {
		if os.IsNotExist(err) {
			// No track.json means no tracked paths
			trackFile = &stores.TrackFile{
				Tracked: []stores.TrackedPath{},
			}
		} else {
			return nil, fmt.Errorf("failed to load track list: %w", err)
		}
	}

	// Get overlay root path
	overlayRoot := repo.OverlayRoot(storeID)

	// Compare each tracked path
	files := make([]DiffFileInfo, 0, len(trackFile.Tracked))
	for _, tracked := range trackFile.Tracked {
		workspacePath := filepath.Join(root, tracked.Path)
		storePath := filepath.Join(overlayRoot, tracked.Path)

		if tracked.Kind == "dir" {
			// For directories, walk and compare all files within
			dirFiles, err := e.compareDirPath(root, overlayRoot, workspacePath, storePath, tracked.Path, req.ShowContent)
			if err != nil {
				return nil, fmt.Errorf("failed to compare directory %s: %w", tracked.Path, err)
			}
			files = append(files, dirFiles...)
		} else {
			fileInfo := e.comparePath(workspacePath, storePath, tracked.Path, tracked.Kind, req.ShowContent)
			files = append(files, fileInfo)
		}
	}

	return &DiffResult{
		WorkspaceID: workspaceID,
		StoreID:     storeID,
		Files:       files,
	}, nil
}

// compareDirPath walks a directory and compares all files within it.
func (e *Engine) compareDirPath(workspaceRoot, overlayRoot, workspaceDir, storeDir, trackedPath string, showContent bool) ([]DiffFileInfo, error) {
	// Collect all file paths from both workspace and store
	fileMap := make(map[string]bool)

	// Walk workspace directory
	workspaceExists, err := e.fs.Exists(workspaceDir)
	if err != nil {
		return nil, fmt.Errorf("failed to check workspace directory existence: %w", err)
	}
	if workspaceExists {
		err := filepath.Walk(workspaceDir, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if !info.IsDir() {
				relPath, err := filepath.Rel(workspaceRoot, path)
				if err != nil {
					return err
				}
				fileMap[relPath] = true
			}
			return nil
		})
		if err != nil {
			return nil, fmt.Errorf("failed to walk workspace directory: %w", err)
		}
	}

	// Walk store directory
	storeExists, err := e.fs.Exists(storeDir)
	if err != nil {
		return nil, fmt.Errorf("failed to check store directory existence: %w", err)
	}
	if storeExists {
		err := filepath.Walk(storeDir, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if !info.IsDir() {
				relPath, err := filepath.Rel(overlayRoot, path)
				if err != nil {
					return err
				}
				fileMap[relPath] = true
			}
			return nil
		})
		if err != nil {
			return nil, fmt.Errorf("failed to walk store directory: %w", err)
		}
	}

	// Compare each file found
	result := make([]DiffFileInfo, 0, len(fileMap))
	for relPath := range fileMap {
		workspacePath := filepath.Join(workspaceRoot, relPath)
		storePath := filepath.Join(overlayRoot, relPath)

		fileInfo := e.comparePath(workspacePath, storePath, relPath, "file", showContent)
		result = append(result, fileInfo)
	}

	return result, nil
}

// comparePath compares a single path between workspace and store overlay.
func (e *Engine) comparePath(workspacePath, storePath, relPath, kind string, showContent bool) DiffFileInfo {
	info := DiffFileInfo{
		Path:  relPath,
		IsDir: kind == "dir",
	}

	// Check existence
	workspaceExists, err := e.fs.Exists(workspacePath)
	if err != nil {
		// Log error but continue with comparison
		workspaceExists = false
	}
	storeExists, err := e.fs.Exists(storePath)
	if err != nil {
		// Log error but continue with comparison
		storeExists = false
	}

	// Determine status based on existence
	if !workspaceExists && !storeExists {
		info.Status = "unchanged"
		return info
	}

	if !storeExists && workspaceExists {
		info.Status = "added"
		if !info.IsDir {
			hash, err := e.hasher.HashFile(workspacePath)
			if err == nil {
				info.WorkspaceHash = hash
			}
		}
		return info
	}

	if storeExists && !workspaceExists {
		info.Status = "removed"
		if !info.IsDir {
			hash, err := e.hasher.HashFile(storePath)
			if err == nil {
				info.StoreHash = hash
			}
		}
		return info
	}

	// Both exist - compare content (for files only)
	if info.IsDir {
		// For directories, just mark as unchanged if both exist
		info.Status = "unchanged"
		return info
	}

	// Hash both files
	workspaceHash, err := e.hasher.HashFile(workspacePath)
	if err != nil {
		info.Status = "modified"
		return info
	}
	info.WorkspaceHash = workspaceHash

	storeHash, err := e.hasher.HashFile(storePath)
	if err != nil {
		info.Status = "modified"
		return info
	}
	info.StoreHash = storeHash

	// Compare hashes
	if workspaceHash != storeHash {
		info.Status = "modified"
	} else {
		info.Status = "unchanged"
	}

	return info
}
