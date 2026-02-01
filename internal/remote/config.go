package remote

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/danieljhkim/monodev/internal/fsops"
)

const (
	// DefaultRemoteName is the default Git remote to use for persistence
	DefaultRemoteName = "origin"

	// DefaultBranch is the default orphan branch name for persistence
	DefaultBranch = "monodev/persist"

	// RemoteConfigFileName is the name of the remote config file
	RemoteConfigFileName = "remote.json"
)

// RemoteConfig represents the configuration for remote persistence operations.
// It's stored repo-locally at .monodev/remote.json.
type RemoteConfig struct {
	// Remote is the name of the Git remote to use (e.g., "origin")
	Remote string `json:"remote"`

	// Branch is the orphan branch name for persistence (e.g., "monodev/persist")
	Branch string `json:"branch"`

	// UpdatedAt is the last time this configuration was modified
	UpdatedAt time.Time `json:"updated_at"`
}

// DefaultRemoteConfig returns a RemoteConfig with default values.
func DefaultRemoteConfig() *RemoteConfig {
	return &RemoteConfig{
		Remote:    DefaultRemoteName,
		Branch:    DefaultBranch,
		UpdatedAt: time.Now(),
	}
}

// RemoteConfigStore is an interface for loading and saving remote configuration.
type RemoteConfigStore interface {
	// Load reads the remote configuration from the specified repo root.
	// Returns ErrRemoteNotConfigured if the config file doesn't exist.
	Load(repoRoot string) (*RemoteConfig, error)

	// Save writes the remote configuration to the specified repo root.
	Save(repoRoot string, config *RemoteConfig) error

	// Exists checks if a remote configuration exists for the specified repo root.
	Exists(repoRoot string) (bool, error)
}

// FileRemoteConfigStore is a file-based implementation of RemoteConfigStore.
type FileRemoteConfigStore struct {
	fs fsops.FS
}

// NewFileRemoteConfigStore creates a new FileRemoteConfigStore.
func NewFileRemoteConfigStore(fs fsops.FS) *FileRemoteConfigStore {
	return &FileRemoteConfigStore{fs: fs}
}

// configPath returns the path to the remote config file for a given repo root.
func (s *FileRemoteConfigStore) configPath(repoRoot string) string {
	return filepath.Join(repoRoot, ".monodev", RemoteConfigFileName)
}

// Load reads the remote configuration from disk.
func (s *FileRemoteConfigStore) Load(repoRoot string) (*RemoteConfig, error) {
	path := s.configPath(repoRoot)

	data, err := s.fs.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, ErrRemoteNotConfigured
		}
		return nil, fmt.Errorf("failed to read remote config: %w", err)
	}

	var config RemoteConfig
	if err := json.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("failed to parse remote config: %w", err)
	}

	return &config, nil
}

// Save writes the remote configuration to disk using atomic writes.
func (s *FileRemoteConfigStore) Save(repoRoot string, config *RemoteConfig) error {
	path := s.configPath(repoRoot)

	// Ensure the directory exists
	dir := filepath.Dir(path)
	if err := s.fs.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	// Marshal the config
	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal remote config: %w", err)
	}

	// Write atomically
	if err := s.fs.AtomicWrite(path, data, 0644); err != nil {
		return fmt.Errorf("failed to write remote config: %w", err)
	}

	return nil
}

// Exists checks if a remote configuration file exists.
func (s *FileRemoteConfigStore) Exists(repoRoot string) (bool, error) {
	path := s.configPath(repoRoot)
	return s.fs.Exists(path)
}
