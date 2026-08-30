package sync

import (
	"context"
	"fmt"
	"path/filepath"
)

// pushStore implements the push operation for stores.
func (s *Syncer) pushStore(ctx context.Context, req *PushRequest) (*PushResult, error) {
	// Validate request
	if req.RepoRoot == "" {
		return nil, fmt.Errorf("repo root is required")
	}
	if err := checkContext(ctx); err != nil {
		return nil, err
	}

	// If no store IDs specified, push all local stores
	storeIDs := req.StoreIDs
	if len(storeIDs) == 0 && !req.WithWorkspace {
		if err := checkContext(ctx); err != nil {
			return nil, err
		}
		allStores, err := s.storeRepo.List()
		if err != nil {
			return nil, fmt.Errorf("failed to list local stores: %w", err)
		}
		if len(allStores) == 0 {
			return nil, fmt.Errorf("no stores found to push")
		}
		storeIDs = allStores
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
		refPath, refData, err := s.prepareWorkspaceReference(req)
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
			if err := s.snapshotMgr.Materialize(storeID, s.storeRepo, req.RepoRoot); err != nil {
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
