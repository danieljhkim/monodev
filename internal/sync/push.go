package sync

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/danieljhkim/monodev/internal/lockfile"
	"github.com/danieljhkim/monodev/internal/stores"
)

// listRepoLocalStores lists the stores a bare push publishes. A store repo
// without scopes is a single repo-local root, so all of its stores count.
func (s *Syncer) listRepoLocalStores() ([]string, error) {
	if lister, ok := s.storeRepo.(stores.RepoLocalLister); ok {
		return lister.ListRepoLocal()
	}
	return s.storeRepo.List()
}

// materializeStore snapshots one store under its shared transaction lock, so
// the persisted metadata, track file, and overlay come from one committed
// state while compatible readers keep running.
func (s *Syncer) materializeStore(ctx context.Context, storeID, repoRoot string) error {
	unlock, err := s.lockStores(ctx, lockfile.Shared, storeID)
	if err != nil {
		return err
	}
	defer unlock()
	return s.snapshotMgr.Materialize(storeID, s.storeRepo, repoRoot)
}

// pushStore implements the push operation for stores.
func (s *Syncer) pushStore(ctx context.Context, req *PushRequest) (*PushResult, error) {
	// Validate request
	if req.RepoRoot == "" {
		return nil, fmt.Errorf("repo root is required")
	}
	if err := checkContext(ctx); err != nil {
		return nil, err
	}

	// With no store IDs, push only this repository's stores. Stores in a
	// shared root are visible to every repository and must be named.
	storeIDs := req.StoreIDs
	if len(storeIDs) == 0 && !req.WithWorkspace {
		if err := checkContext(ctx); err != nil {
			return nil, err
		}
		localStores, err := s.listRepoLocalStores()
		if err != nil {
			return nil, fmt.Errorf("failed to list repo-local stores: %w", err)
		}
		if len(localStores) == 0 {
			return nil, fmt.Errorf("no repo-local stores to push; stores in ~/.monodev or MONODEV_ROOT are shared across repositories, so name them: monodev push <store-id>")
		}
		storeIDs = localStores
	}

	// Load or create remote config
	if err := checkContext(ctx); err != nil {
		return nil, err
	}
	config, err := s.loadOrCreateConfig(req.RepoRoot, req.Remote)
	if err != nil {
		return nil, err
	}

	// Ensure persistence repo exists
	if !req.DryRun {
		if err := s.ensurePersistenceRemote(ctx, req.RepoRoot, config.Remote, config.Branch); err != nil {
			return nil, err
		}
	}

	var workspaceRefPath string
	var workspaceRefData []byte
	var pushedWorkspace bool
	if req.WithWorkspace {
		if err := checkContext(ctx); err != nil {
			return nil, err
		}
		refPath, refData, err := s.prepareWorkspaceReference(ctx, req)
		if err != nil {
			return nil, err
		}
		workspaceRefPath = refPath
		workspaceRefData = refData
		pushedWorkspace = true
	}

	// Materialize stores to .monodev/persist/stores/
	var pushedStores []string
	for _, storeID := range storeIDs {
		if err := checkContext(ctx); err != nil {
			return nil, err
		}
		if !req.DryRun {
			if err := s.materializeStore(ctx, storeID, req.RepoRoot); err != nil {
				return nil, fmt.Errorf("failed to materialize store %q: %w", storeID, err)
			}
		}
		pushedStores = append(pushedStores, storeID)
	}

	if !req.DryRun && pushedWorkspace {
		if err := checkContext(ctx); err != nil {
			return nil, err
		}
		if err := s.fs.AtomicWrite(workspaceRefPath, workspaceRefData, 0600); err != nil {
			return nil, fmt.Errorf("failed to write workspace reference: %w", err)
		}
	}

	// The persistence branch is plaintext. Scan the complete materialized
	// payload immediately before committing so accidental credentials never
	// become part of its history. This is a guardrail, not confidentiality.
	if !req.DryRun && !req.AllowSecrets {
		if err := checkContext(ctx); err != nil {
			return nil, err
		}
		if finding, err := scanPersistedStores(filepath.Join(req.RepoRoot, ".monodev", "persist", "stores")); err != nil {
			return nil, fmt.Errorf("failed to scan persistence payload: %w", err)
		} else if finding != nil {
			return nil, newSecretScanError(*finding)
		}
	}

	// Build commit message
	commitMessage := s.buildPushCommitMessage(pushedStores, pushedWorkspace)

	// Stage and commit changes
	if !req.DryRun {
		persistDir := filepath.Join(req.RepoRoot, ".monodev", "persist")
		if err := checkContext(ctx); err != nil {
			return nil, err
		}
		if err := s.git.Commit(ctx, req.RepoRoot, commitMessage, []string{persistDir}); err != nil {
			return nil, fmt.Errorf("failed to commit: %w", err)
		}

		// Push to remote
		if err := checkContext(ctx); err != nil {
			return nil, err
		}
		if err := s.git.Push(ctx, req.RepoRoot, config.Remote, config.Branch, req.Force); err != nil {
			return nil, fmt.Errorf("failed to push: %w", err)
		}
	}

	return &PushResult{
		PushedStores:     pushedStores,
		PushedWorkspace:  pushedWorkspace,
		WorkspaceID:      req.WorkspaceID,
		WorkspaceRefPath: workspaceRefPath,
		CommitMessage:    commitMessage,
		Remote:           config.Remote,
		Branch:           config.Branch,
		DryRun:           req.DryRun,
	}, nil
}
