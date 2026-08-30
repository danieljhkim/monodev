package engine

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/danieljhkim/monodev/internal/lockfile"
	"github.com/danieljhkim/monodev/internal/state"
)

// DiscoverNewTrackedRequest requests discovery of files that were created
// under a tracked directory since the active store was last committed.
type DiscoverNewTrackedRequest struct {
	// CWD is the current working directory
	CWD string
}

// DiscoverNewTrackedResult reports files found by DiscoverNewTracked.
type DiscoverNewTrackedResult struct {
	// NewPaths are CWD-relative paths of files present in the workspace,
	// under a tracked directory, that are not yet present in the active
	// store's overlay. They are not yet individually tracked.
	NewPaths []string
}

// DiscoverNewTracked finds files under tracked directories that exist in the
// workspace but are missing from the active store's overlay — i.e. files
// created since the directory was last committed. It is read-only: it does
// not modify the store, track file, or workspace state. Candidates ignored
// by git (.gitignore, .git/info/exclude, global excludes) are excluded.
//
// This is the discovery primitive `save` composes with Track and Commit; it
// intentionally does not track or commit anything itself.
func (e *Engine) DiscoverNewTracked(ctx context.Context, req *DiscoverNewTrackedRequest) (*DiscoverNewTrackedResult, error) {
	root, fingerprint, workspacePath, err := e.DiscoverWorkspace(req.CWD)
	if err != nil {
		return nil, fmt.Errorf("failed to discover workspace: %w", err)
	}

	workspaceID := state.ComputeWorkspaceID(fingerprint, workspacePath)
	unlockWorkspace, err := e.lockWorkspace(ctx, workspaceID, lockfile.Shared)
	if err != nil {
		return nil, err
	}
	defer unlockWorkspace()

	workspaceState, _, err := e.LoadOrCreateWorkspaceState(root, fingerprint, workspacePath, "copy")
	if err != nil {
		return nil, fmt.Errorf("failed to load or create workspace state: %w", err)
	}
	if workspaceState.ActiveStore == "" {
		return nil, ErrNoActiveStore
	}

	repo, err := e.storeResolver.activeStoreRepo(workspaceState)
	if err != nil {
		return nil, err
	}
	unlockStore, err := e.lockStores(ctx, storeLockRequest{repo: repo, id: workspaceState.ActiveStore, mode: lockfile.Shared})
	if err != nil {
		return nil, err
	}
	defer unlockStore()

	track, err := repo.LoadTrack(workspaceState.ActiveStore)
	if err != nil {
		return nil, fmt.Errorf("failed to load track file: %w", err)
	}

	overlayRoot := repo.OverlayRoot(workspaceState.ActiveStore)
	workspaceRoot := filepath.Join(root, workspacePath)

	var candidates []string
	for _, tracked := range track.Tracked {
		if tracked.Kind != "dir" {
			continue
		}
		dirCandidates, err := e.discoverNewFilesInDir(workspaceRoot, overlayRoot, tracked.Path)
		if err != nil {
			return nil, fmt.Errorf("failed to scan tracked directory %s: %w", tracked.Path, err)
		}
		candidates = append(candidates, dirCandidates...)
	}

	if len(candidates) == 0 {
		return &DiscoverNewTrackedResult{}, nil
	}
	sort.Strings(candidates)

	ignored, err := e.gitRepo.IsIgnored(workspaceRoot, candidates)
	if err != nil {
		// Best-effort: ignore detection is a convenience, not a correctness
		// requirement. If it fails (e.g. git unavailable), fall back to
		// treating every candidate as newly discovered.
		ignored = nil
	}

	newPaths := make([]string, 0, len(candidates))
	for _, c := range candidates {
		if ignored[c] {
			continue
		}
		newPaths = append(newPaths, c)
	}

	return &DiscoverNewTrackedResult{NewPaths: newPaths}, nil
}

// discoverNewFilesInDir walks a tracked directory in the workspace and
// returns workspace-relative paths of files that do not yet exist at the
// corresponding location in the store overlay.
func (e *Engine) discoverNewFilesInDir(workspaceRoot, overlayRoot, trackedPath string) ([]string, error) {
	workspaceDir := filepath.Join(workspaceRoot, trackedPath)

	exists, err := e.fs.Exists(workspaceDir)
	if err != nil {
		return nil, fmt.Errorf("failed to check workspace directory: %w", err)
	}
	if !exists {
		return nil, nil
	}

	var candidates []string
	walkErr := filepath.Walk(workspaceDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			if info.Name() == ".git" {
				return filepath.SkipDir
			}
			return nil
		}

		relPath, err := filepath.Rel(workspaceRoot, path)
		if err != nil {
			return err
		}

		storePath := filepath.Join(overlayRoot, relPath)
		storeExists, err := e.fs.Exists(storePath)
		if err != nil {
			return fmt.Errorf("failed to check store path for %s: %w", relPath, err)
		}
		if !storeExists {
			candidates = append(candidates, relPath)
		}
		return nil
	})
	if walkErr != nil {
		return nil, fmt.Errorf("failed to walk tracked directory %s: %w", trackedPath, walkErr)
	}

	return candidates, nil
}
