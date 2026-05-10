package sync

import (
	"context"
	"fmt"
)

// pullStore implements the pull operation for stores.
func (s *Syncer) pullStore(ctx context.Context, req *PullRequest) (*PullResult, error) {
	// Validate request
	if req.RepoRoot == "" {
		return nil, fmt.Errorf("repo root is required")
	}
	if err := checkContext(ctx); err != nil {
		return nil, err
	}

	// Load remote config
	config, err := s.configStore.Load(req.RepoRoot)
	if err != nil {
		return nil, fmt.Errorf("failed to load remote config: %w", err)
	}

	// Use request remote if specified, otherwise use config
	remoteName := config.Remote
	if req.Remote != "" {
		remoteName = req.Remote
	}

	// Ensure persistence repo exists
	if err := checkContext(ctx); err != nil {
		return nil, err
	}
	if err := s.git.EnsureRepo(ctx, req.RepoRoot, config.Branch); err != nil {
		return nil, fmt.Errorf("failed to ensure persistence repo: %w", err)
	}

	// Get the remote URL from the main repository
	if err := checkContext(ctx); err != nil {
		return nil, err
	}
	remoteURL, err := s.git.GetRemoteURL(ctx, req.RepoRoot, remoteName)
	if err != nil {
		return nil, fmt.Errorf("failed to get remote URL: %w", err)
	}

	// Configure the remote in the persistence repository
	if err := checkContext(ctx); err != nil {
		return nil, err
	}
	if err := s.git.SetRemote(ctx, req.RepoRoot, remoteName, remoteURL); err != nil {
		return nil, fmt.Errorf("failed to set remote: %w", err)
	}

	// Fetch the persistence branch
	if err := checkContext(ctx); err != nil {
		return nil, err
	}
	if err := s.git.Fetch(ctx, req.RepoRoot, remoteName, config.Branch); err != nil {
		return nil, fmt.Errorf("failed to fetch: %w", err)
	}

	// Checkout to work tree
	if err := checkContext(ctx); err != nil {
		return nil, err
	}
	if err := s.git.Checkout(ctx, req.RepoRoot, config.Branch); err != nil {
		return nil, fmt.Errorf("failed to checkout: %w", err)
	}

	// If no store IDs specified, pull all stores from the persist directory
	storeIDs := req.StoreIDs
	if len(storeIDs) == 0 {
		if err := checkContext(ctx); err != nil {
			return nil, err
		}
		persistedStores, err := s.snapshotMgr.ListPersistedStores(req.RepoRoot)
		if err != nil {
			return nil, fmt.Errorf("failed to list persisted stores: %w", err)
		}
		if len(persistedStores) == 0 {
			return &PullResult{
				PulledStores:    []string{},
				PulledWorkspace: false,
				Verified:        req.Verify,
				Remote:          remoteName,
				Branch:          config.Branch,
			}, nil
		}
		storeIDs = persistedStores
	}

	// Dematerialize stores from .monodev/persist/stores/ to ~/.monodev/stores/
	var pulledStores []string
	for _, storeID := range storeIDs {
		if err := checkContext(ctx); err != nil {
			return nil, err
		}
		if err := s.snapshotMgr.Dematerialize(storeID, req.RepoRoot, s.storeRepo); err != nil {
			return nil, fmt.Errorf("failed to dematerialize store %q: %w", storeID, err)
		}
		pulledStores = append(pulledStores, storeID)

		// Optionally verify checksums
		if req.Verify {
			if err := checkContext(ctx); err != nil {
				return nil, err
			}
			if err := s.snapshotMgr.Verify(storeID, req.RepoRoot, s.hasher); err != nil {
				return nil, fmt.Errorf("verification failed for store %q: %w", storeID, err)
			}
		}
	}

	return &PullResult{
		PulledStores:    pulledStores,
		PulledWorkspace: false, // Not implemented yet
		Verified:        req.Verify,
		Remote:          remoteName,
		Branch:          config.Branch,
	}, nil
}
