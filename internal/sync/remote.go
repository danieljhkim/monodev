package sync

import (
	"context"
	"fmt"
	"time"

	"github.com/danieljhkim/monodev/internal/remote"
)

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

func (s *Syncer) loadPullConfig(repoRoot, requestedRemote string) (*remote.RemoteConfig, string, error) {
	config, err := s.configStore.Load(repoRoot)
	if err != nil {
		return nil, "", fmt.Errorf("failed to load remote config: %w", err)
	}

	remoteName := config.Remote
	if requestedRemote != "" {
		remoteName = requestedRemote
	}

	return config, remoteName, nil
}

func (s *Syncer) ensurePersistenceRemote(ctx context.Context, repoRoot, remoteName, branch string) error {
	if err := checkContext(ctx); err != nil {
		return err
	}
	if err := s.git.EnsureRepo(ctx, repoRoot, branch); err != nil {
		return fmt.Errorf("failed to ensure persistence repo: %w", err)
	}

	if err := checkContext(ctx); err != nil {
		return err
	}
	remoteURL, err := s.git.GetRemoteURL(ctx, repoRoot, remoteName)
	if err != nil {
		return fmt.Errorf("failed to get remote URL: %w", err)
	}

	if err := checkContext(ctx); err != nil {
		return err
	}
	if err := s.git.SetRemote(ctx, repoRoot, remoteName, remoteURL); err != nil {
		return fmt.Errorf("failed to set remote: %w", err)
	}

	return nil
}
