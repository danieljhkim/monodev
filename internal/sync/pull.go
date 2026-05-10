package sync

import (
	"context"
	"errors"
	"fmt"

	"github.com/danieljhkim/monodev/internal/persist"
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

	config, remoteName, err := s.loadPullConfig(req.RepoRoot, req.Remote)
	if err != nil {
		return nil, err
	}

	// Ensure persistence repo exists
	if err := s.ensurePersistenceRemote(ctx, req.RepoRoot, remoteName, config.Branch); err != nil {
		return nil, err
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
				Verified:        false,
				Remote:          remoteName,
				Branch:          config.Branch,
			}, nil
		}
		storeIDs = persistedStores
	}

	var pulledStores []string
	verifiedStores := 0
	for _, storeID := range storeIDs {
		if err := checkContext(ctx); err != nil {
			return nil, err
		}

		// Verify persisted content before copying it into the local store. Legacy
		// persisted stores without manifests remain pullable, but they do not
		// count as verified because there is no checksum metadata to check.
		if req.Verify {
			if err := s.snapshotMgr.Verify(storeID, req.RepoRoot, s.hasher); err != nil {
				if !errors.Is(err, persist.ErrVerificationManifestMissing) {
					return nil, fmt.Errorf("verification failed for store %q: %w", storeID, err)
				}
			} else {
				verifiedStores++
			}
		}

		if err := s.snapshotMgr.Dematerialize(storeID, req.RepoRoot, s.storeRepo); err != nil {
			return nil, fmt.Errorf("failed to dematerialize store %q: %w", storeID, err)
		}
		pulledStores = append(pulledStores, storeID)
	}

	return &PullResult{
		PulledStores:    pulledStores,
		PulledWorkspace: false, // Not implemented yet
		Verified:        req.Verify && len(pulledStores) > 0 && verifiedStores == len(pulledStores),
		Remote:          remoteName,
		Branch:          config.Branch,
	}, nil
}
