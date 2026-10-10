package sync

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/danieljhkim/monodev/internal/lockfile"
	"github.com/danieljhkim/monodev/internal/persist"
	"github.com/danieljhkim/monodev/internal/remote"
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

	var (
		remoteName, branch string
		err                error
	)
	if req.SkipFetch {
		remoteName, branch, err = s.persistenceTarget(req.RepoRoot, req.Remote)
	} else {
		remoteName, branch, err = s.FetchPersistence(ctx, req.RepoRoot, req.Remote)
	}
	if err != nil {
		return nil, err
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
				Branch:          branch,
			}, nil
		}
		storeIDs = persistedStores
	}
	if workspaceRef != nil && req.WithStores {
		storeIDs = appendUniqueStores(storeIDs, workspaceReferenceStoreIDs(workspaceRef), "")
	}

	// Refuse unsupported schemas before any store is replaced. The loop
	// below publishes one store at a time, so checking inside it would
	// leave earlier stores overwritten when a later one is too new.
	// --force authorizes divergent content, not a schema this binary
	// cannot read, and a missing or valid manifest does not bypass it.
	if err := s.refuseFutureStoreSchemas(ctx, req.RepoRoot, storeIDs); err != nil {
		return nil, err
	}

	var pulledStores []string
	var warnings []string
	verifiedStores := 0
	for _, storeID := range storeIDs {
		if err := checkContext(ctx); err != nil {
			return nil, err
		}

		verified, err := s.replaceLocalStore(ctx, req, storeID)
		if err != nil {
			return nil, err
		}
		if verified {
			verifiedStores++
		} else {
			warnings = append(warnings, fmt.Sprintf("store %q has no verification manifest; content authenticity was not checked", storeID))
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
		Branch:                      branch,
		Warnings:                    warnings,
	}
	if workspaceRef != nil {
		if err := s.restoreWorkspaceReference(ctx, req, workspaceRef); err != nil {
			return nil, err
		}
		result.PulledWorkspace = true
		result.WorkspaceID = req.LocalWorkspaceID
	}
	return result, nil
}

// FetchPersistence fetches the configured persistence branch and fast-forwards
// the persistence work tree to it without restoring any store or workspace
// reference. It returns the remote and branch it fetched.
func (s *Syncer) FetchPersistence(ctx context.Context, repoRoot, requestedRemote string) (string, string, error) {
	if repoRoot == "" {
		return "", "", fmt.Errorf("repo root is required")
	}
	config, remoteName, err := s.loadPullConfig(repoRoot, requestedRemote)
	if err != nil {
		return "", "", err
	}

	// Ensure persistence repo exists
	if err := s.ensurePersistenceRemote(ctx, repoRoot, remoteName, config.Branch); err != nil {
		return "", "", err
	}

	// Fetch the persistence branch
	if err := checkContext(ctx); err != nil {
		return "", "", err
	}
	if err := s.git.Fetch(ctx, repoRoot, remoteName, config.Branch); err != nil {
		return "", "", fmt.Errorf("failed to fetch: %w", err)
	}

	// Fast-forward the local persistence branch to the exact fetched commit
	// and materialize that commit in the persistence work tree.
	if err := checkContext(ctx); err != nil {
		return "", "", err
	}
	if err := s.git.CheckoutFetched(ctx, repoRoot, config.Branch); err != nil {
		return "", "", fmt.Errorf("failed to materialize fetched persistence branch: %w", err)
	}
	return remoteName, config.Branch, nil
}

// persistenceTarget names the remote and branch a pull that reuses the
// already-materialized persistence work tree reports. A missing remote
// configuration is not an error there: nothing is fetched.
func (s *Syncer) persistenceTarget(repoRoot, requestedRemote string) (string, string, error) {
	config, remoteName, err := s.loadPullConfig(repoRoot, requestedRemote)
	if err != nil {
		if errors.Is(err, remote.ErrRemoteNotConfigured) {
			return requestedRemote, "", nil
		}
		return "", "", err
	}
	return remoteName, config.Branch, nil
}

// replaceLocalStore compares, verifies, and replaces one local store while
// holding its exclusive transaction lock. Force authorizes overwriting
// changed content, not bypassing a running store transaction or an
// unsupported store schema. Schema compatibility is refused for every
// selected store before this method runs. It reports whether a
// verification manifest certified the persisted content.
func (s *Syncer) replaceLocalStore(ctx context.Context, req *PullRequest, storeID string) (bool, error) {
	unlock, err := s.lockStores(ctx, lockfile.Exclusive, storeID)
	if err != nil {
		return false, fmt.Errorf("failed to pull store %q: %w", storeID, err)
	}
	defer unlock()

	// Compare the incoming content against the developer's pre-existing
	// local copy, if any, before touching the working tree. This catches
	// a remote-side change (tampering or otherwise) that a manifest-based
	// check alone cannot: the manifest travels with the content it
	// certifies, so an actor who can push to the persist branch can
	// rewrite both together and still pass Verify.
	changedFiles, err := s.snapshotMgr.DiffAgainstLocalCopy(storeID, req.RepoRoot, s.storeRepo, s.hasher)
	if err != nil {
		return false, fmt.Errorf("failed to compare store %q against local copy: %w", storeID, err)
	}
	if len(changedFiles) > 0 && !req.Force {
		return false, fmt.Errorf("%w: store %q (changed: %s); rerun with --force to overwrite the local copy", ErrPulledContentChanged, storeID, strings.Join(changedFiles, ", "))
	}

	// Verify persisted content before copying it into the local store.
	// Verification always runs; a missing manifest is an explicit,
	// reported warning rather than a silent pass. Legacy persisted
	// stores without manifests remain pullable, but they must never be
	// reported as verified.
	verified := true
	if err := s.snapshotMgr.Verify(storeID, req.RepoRoot, s.hasher); err != nil {
		if !errors.Is(err, persist.ErrVerificationManifestMissing) {
			return false, fmt.Errorf("verification failed for store %q: %w", storeID, err)
		}
		verified = false
	}

	if err := s.snapshotMgr.Dematerialize(storeID, req.RepoRoot, s.storeRepo); err != nil {
		return false, fmt.Errorf("failed to dematerialize store %q: %w", storeID, err)
	}
	return verified, nil
}

// refuseFutureStoreSchemas checks every selected persisted store before pull
// publishes any of them into the local store directory.
func (s *Syncer) refuseFutureStoreSchemas(ctx context.Context, repoRoot string, storeIDs []string) error {
	for _, storeID := range storeIDs {
		if err := checkContext(ctx); err != nil {
			return err
		}
		if err := s.snapshotMgr.CheckIncomingStoreSchemas(storeID, repoRoot); err != nil {
			return fmt.Errorf("failed to pull store %q: %w", storeID, err)
		}
	}
	return nil
}

func appendUniqueStores(storeIDs []string, additional []string, activeStore string) []string {
	// Do not use the combined input lengths as a map allocation hint: their
	// addition can overflow before make receives the size.
	seen := make(map[string]struct{})
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
