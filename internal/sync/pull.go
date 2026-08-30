package sync

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/danieljhkim/monodev/internal/persist"
)

// ErrPulledContentChanged is wrapped by the error pullStore returns when a
// store already present locally would be overwritten with content that
// differs from that local copy. The persist branch's checksum manifest
// cannot rule this out on its own: an actor with push access to the branch
// can rewrite the manifest alongside the content it certifies. Comparing
// against the developer's pre-existing local copy is what surfaces the
// change so it is not silently applied to the working tree. Callers must
// pass PullRequest.Force to proceed anyway.
var ErrPulledContentChanged = errors.New("pulled store content differs from local copy")

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

	// Fast-forward the local persistence branch to the exact fetched commit
	// and materialize that commit in the persistence work tree.
	if err := checkContext(ctx); err != nil {
		return nil, err
	}
	if err := s.git.CheckoutFetched(ctx, req.RepoRoot, config.Branch); err != nil {
		return nil, fmt.Errorf("failed to materialize fetched persistence branch: %w", err)
	}

	workspaceRef, workspaceFound, err := s.loadWorkspaceReference(req)
	if err != nil {
		return nil, err
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
		if len(persistedStores) == 0 && workspaceRef == nil {
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
	if workspaceRef != nil && req.WithStores {
		storeIDs = appendUniqueStores(storeIDs, workspaceReferenceStoreIDs(workspaceRef), "")
	}

	var pulledStores []string
	var warnings []string
	verifiedStores := 0
	for _, storeID := range storeIDs {
		if err := checkContext(ctx); err != nil {
			return nil, err
		}

		// Compare the incoming content against the developer's pre-existing
		// local copy, if any, before touching the working tree. This catches
		// a remote-side change (tampering or otherwise) that a manifest-based
		// check alone cannot: the manifest travels with the content it
		// certifies, so an actor who can push to the persist branch can
		// rewrite both together and still pass Verify.
		changedFiles, err := s.snapshotMgr.DiffAgainstLocalCopy(storeID, req.RepoRoot, s.storeRepo, s.hasher)
		if err != nil {
			return nil, fmt.Errorf("failed to compare store %q against local copy: %w", storeID, err)
		}
		if len(changedFiles) > 0 && !req.Force {
			return nil, fmt.Errorf("%w: store %q (changed: %s); rerun with --force to overwrite the local copy", ErrPulledContentChanged, storeID, strings.Join(changedFiles, ", "))
		}

		// Verify persisted content before copying it into the local store.
		// Verification always runs; a missing manifest is an explicit,
		// reported warning rather than a silent pass. Legacy persisted
		// stores without manifests remain pullable, but they must never be
		// reported as verified.
		if err := s.snapshotMgr.Verify(storeID, req.RepoRoot, s.hasher); err != nil {
			if !errors.Is(err, persist.ErrVerificationManifestMissing) {
				return nil, fmt.Errorf("verification failed for store %q: %w", storeID, err)
			}
			warnings = append(warnings, fmt.Sprintf("store %q has no verification manifest; content authenticity was not checked", storeID))
		} else {
			verifiedStores++
		}

		if err := s.snapshotMgr.Dematerialize(storeID, req.RepoRoot, s.storeRepo); err != nil {
			return nil, fmt.Errorf("failed to dematerialize store %q: %w", storeID, err)
		}
		pulledStores = append(pulledStores, storeID)
	}

	result := &PullResult{
		PulledStores:                pulledStores,
		PulledWorkspace:             false,
		WorkspaceReferenceFound:     workspaceFound,
		WorkspaceReferenceValidated: workspaceRef != nil,
		Verified:                    len(pulledStores) > 0 && verifiedStores == len(pulledStores),
		Remote:                      remoteName,
		Branch:                      config.Branch,
		Warnings:                    warnings,
	}
	if workspaceRef != nil {
		if err := s.restoreWorkspaceReference(req, workspaceRef); err != nil {
			return nil, err
		}
		result.PulledWorkspace = true
		result.WorkspaceID = req.LocalWorkspaceID
	}
	return result, nil
}

func appendUniqueStores(storeIDs []string, additional []string, activeStore string) []string {
	seen := make(map[string]struct{}, len(storeIDs)+len(additional)+1)
	for _, storeID := range storeIDs {
		seen[storeID] = struct{}{}
	}
	for _, storeID := range append(additional, activeStore) {
		if storeID == "" {
			continue
		}
		if _, exists := seen[storeID]; exists {
			continue
		}
		seen[storeID] = struct{}{}
		storeIDs = append(storeIDs, storeID)
	}
	return storeIDs
}
