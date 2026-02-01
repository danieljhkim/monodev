package sync

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/danieljhkim/monodev/internal/remote"
)

// pushStore implements the push operation for stores.
func (s *Syncer) pushStore(ctx context.Context, req *PushRequest) (*PushResult, error) {
	// Validate request
	if req.RepoRoot == "" {
		return nil, fmt.Errorf("repo root is required")
	}

	// If no store IDs specified, push all local stores
	storeIDs := req.StoreIDs
	if len(storeIDs) == 0 && !req.WithWorkspace {
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
	config, err := s.loadOrCreateConfig(req.RepoRoot, req.Remote)
	if err != nil {
		return nil, err
	}

	// Ensure persistence repo exists
	if !req.DryRun {
		if err := s.git.EnsureRepo(req.RepoRoot, config.Branch); err != nil {
			return nil, fmt.Errorf("failed to ensure persistence repo: %w", err)
		}

		// Get the remote URL from the main repository
		remoteURL, err := s.git.GetRemoteURL(req.RepoRoot, config.Remote)
		if err != nil {
			return nil, fmt.Errorf("failed to get remote URL: %w", err)
		}

		// Configure the remote in the persistence repository
		if err := s.git.SetRemote(req.RepoRoot, config.Remote, remoteURL); err != nil {
			return nil, fmt.Errorf("failed to set remote: %w", err)
		}
	}

	// Materialize stores to .monodev/persist/stores/
	var pushedStores []string
	for _, storeID := range storeIDs {
		if !req.DryRun {
			if err := s.snapshotMgr.Materialize(storeID, s.storeRepo, req.RepoRoot); err != nil {
				return nil, fmt.Errorf("failed to materialize store %q: %w", storeID, err)
			}
		}
		pushedStores = append(pushedStores, storeID)
	}

	// Build commit message
	commitMessage := s.buildPushCommitMessage(pushedStores, req.WithWorkspace)

	// Stage and commit changes
	if !req.DryRun {
		persistDir := filepath.Join(req.RepoRoot, ".monodev", "persist")
		if err := s.git.Commit(req.RepoRoot, commitMessage, []string{persistDir}); err != nil {
			return nil, fmt.Errorf("failed to commit: %w", err)
		}

		// Push to remote
		if err := s.git.Push(req.RepoRoot, config.Remote, config.Branch, req.Force); err != nil {
			return nil, fmt.Errorf("failed to push: %w", err)
		}
	}

	return &PushResult{
		PushedStores:    pushedStores,
		PushedWorkspace: req.WithWorkspace,
		CommitMessage:   commitMessage,
		Remote:          config.Remote,
		Branch:          config.Branch,
		DryRun:          req.DryRun,
	}, nil
}

// loadOrCreateConfig loads the remote config, or creates a default one if it doesn't exist.
func (s *Syncer) loadOrCreateConfig(repoRoot, remoteName string) (*remote.RemoteConfig, error) {
	config, err := s.configStore.Load(repoRoot)
	if err != nil {
		if err == remote.ErrRemoteNotConfigured {
			// Create default config
			config = remote.DefaultRemoteConfig()
			if remoteName != "" {
				config.Remote = remoteName
			}
			config.UpdatedAt = s.clock.Now()

			// Save the config
			if err := s.configStore.Save(repoRoot, config); err != nil {
				return nil, fmt.Errorf("failed to save default config: %w", err)
			}
		} else {
			return nil, fmt.Errorf("failed to load remote config: %w", err)
		}
	}

	// Override remote if specified in request
	if remoteName != "" && remoteName != config.Remote {
		config.Remote = remoteName
		config.UpdatedAt = time.Now()
		if err := s.configStore.Save(repoRoot, config); err != nil {
			return nil, fmt.Errorf("failed to update config: %w", err)
		}
	}

	return config, nil
}

// buildPushCommitMessage builds a commit message for a push operation.
func (s *Syncer) buildPushCommitMessage(storeIDs []string, withWorkspace bool) string {
	var parts []string

	if len(storeIDs) > 0 {
		if len(storeIDs) == 1 {
			parts = append(parts, fmt.Sprintf("store %s", storeIDs[0]))
		} else {
			parts = append(parts, fmt.Sprintf("%d stores", len(storeIDs)))
		}
	}

	if withWorkspace {
		parts = append(parts, "workspace")
	}

	return fmt.Sprintf("push: %s", strings.Join(parts, ", "))
}
