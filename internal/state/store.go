package state

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/danieljhkim/monodev/internal/fsops"
)

// StateStore provides an interface for persisting workspace and repository state.
type StateStore interface {
	// LoadWorkspace loads the workspace state for the given workspace ID.
	// Returns os.ErrNotExist if the state doesn't exist.
	LoadWorkspace(id string) (*WorkspaceState, error)

	// SaveWorkspace saves the workspace state atomically.
	SaveWorkspace(id string, state *WorkspaceState) error

	// DeleteWorkspace deletes the workspace state file.
	DeleteWorkspace(id string) error

	// LoadRepoState loads the repository state for the given repo fingerprint.
	// Returns os.ErrNotExist if the state doesn't exist.
	LoadRepoState(fingerprint string) (*RepoState, error)

	// SaveRepoState saves the repository state atomically.
	SaveRepoState(fingerprint string, state *RepoState) error
}

// FileStateStore implements StateStore using JSON files on disk.
type FileStateStore struct {
	fs            fsops.FS
	workspacesDir string
	reposDir      string
}

// NewFileStateStore creates a new FileStateStore.
func NewFileStateStore(fs fsops.FS, workspacesDir, reposDir string) *FileStateStore {
	return &FileStateStore{
		fs:            fs,
		workspacesDir: workspacesDir,
		reposDir:      reposDir,
	}
}

// LoadWorkspace loads the workspace state for the given workspace ID.
func (s *FileStateStore) LoadWorkspace(id string) (*WorkspaceState, error) {
	path := filepath.Join(s.workspacesDir, id+".json")

	data, err := s.fs.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, os.ErrNotExist
		}
		return nil, fmt.Errorf("failed to read workspace state: %w", err)
	}

	var state WorkspaceState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("failed to unmarshal workspace state: %w", err)
	}

	return &state, nil
}

// SaveWorkspace saves the workspace state atomically.
func (s *FileStateStore) SaveWorkspace(id string, state *WorkspaceState) error {
	path := filepath.Join(s.workspacesDir, id+".json")

	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal workspace state: %w", err)
	}

	if err := s.fs.AtomicWrite(path, data, 0644); err != nil {
		return fmt.Errorf("failed to write workspace state: %w", err)
	}

	return nil
}

// DeleteWorkspace deletes the workspace state file.
func (s *FileStateStore) DeleteWorkspace(id string) error {
	path := filepath.Join(s.workspacesDir, id+".json")

	if err := s.fs.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to delete workspace state: %w", err)
	}

	return nil
}

// LoadRepoState loads the repository state for the given repo fingerprint.
func (s *FileStateStore) LoadRepoState(fingerprint string) (*RepoState, error) {
	path := filepath.Join(s.reposDir, fingerprint+".json")

	data, err := s.fs.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, os.ErrNotExist
		}
		return nil, fmt.Errorf("failed to read repo state: %w", err)
	}

	var state RepoState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("failed to unmarshal repo state: %w", err)
	}

	return &state, nil
}

// SaveRepoState saves the repository state atomically.
func (s *FileStateStore) SaveRepoState(fingerprint string, state *RepoState) error {
	path := filepath.Join(s.reposDir, fingerprint+".json")

	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal repo state: %w", err)
	}

	if err := s.fs.AtomicWrite(path, data, 0644); err != nil {
		return fmt.Errorf("failed to write repo state: %w", err)
	}

	return nil
}
