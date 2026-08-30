package state

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/danieljhkim/monodev/internal/fsops"
	"github.com/danieljhkim/monodev/internal/lockfile"
)

// StateStore provides an interface for persisting workspace state.
type StateStore interface {
	// LoadWorkspace loads the workspace state for the given workspace ID.
	// Returns os.ErrNotExist if the state doesn't exist.
	LoadWorkspace(id string) (*WorkspaceState, error)

	// SaveWorkspace saves the workspace state atomically.
	SaveWorkspace(id string, state *WorkspaceState) error

	// DeleteWorkspace deletes the workspace state file.
	DeleteWorkspace(id string) error
}

// WorkspaceLocker is implemented by state stores that can coordinate a
// complete workspace transaction across processes. Engine callers use this as
// an optional capability so in-memory test stores remain lightweight.
type WorkspaceLocker interface {
	LockWorkspace(ctx context.Context, id string, mode lockfile.Mode) (*lockfile.Lock, error)
}

// FileStateStore implements StateStore using JSON files on disk.
type FileStateStore struct {
	fs            fsops.FS
	workspacesDir string
}

// NewFileStateStore creates a new FileStateStore.
func NewFileStateStore(fs fsops.FS, workspacesDir string) *FileStateStore {
	return &FileStateStore{
		fs:            fs,
		workspacesDir: workspacesDir,
	}
}

// LoadWorkspace loads the workspace state for the given workspace ID.
func (s *FileStateStore) LoadWorkspace(id string) (*WorkspaceState, error) {
	path, err := s.workspacePath(id)
	if err != nil {
		return nil, err
	}

	data, err := s.fs.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, os.ErrNotExist
		}
		return nil, fmt.Errorf("failed to read workspace state: %w", err)
	}

	migrated, changed, err := MigrateWorkspaceJSON(path, data)
	if err != nil {
		return nil, err
	}
	if changed {
		if err := s.fs.AtomicWrite(path, migrated, 0600); err != nil {
			return nil, fmt.Errorf("failed to persist migrated workspace state: %w", err)
		}
	}

	var state WorkspaceState
	if err := json.Unmarshal(migrated, &state); err != nil {
		return nil, fmt.Errorf("failed to unmarshal workspace state: %w", err)
	}

	return &state, nil
}

// SaveWorkspace saves the workspace state atomically.
func (s *FileStateStore) SaveWorkspace(id string, state *WorkspaceState) error {
	path, err := s.workspacePath(id)
	if err != nil {
		return err
	}

	state.SchemaVersion = WorkspaceSchemaVersion
	state.MigrateDeprecatedStack()
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal workspace state: %w", err)
	}

	if err := s.fs.AtomicWrite(path, data, 0600); err != nil {
		return fmt.Errorf("failed to write workspace state: %w", err)
	}

	return nil
}

// DeleteWorkspace deletes the workspace state file.
func (s *FileStateStore) DeleteWorkspace(id string) error {
	path, err := s.workspacePath(id)
	if err != nil {
		return err
	}

	if err := s.fs.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to delete workspace state: %w", err)
	}

	return nil
}

// LockWorkspace acquires the process-wide lock for one workspace. Locks live
// beside (not inside) workspace JSON so deletion cannot invalidate the lock.
func (s *FileStateStore) LockWorkspace(ctx context.Context, id string, mode lockfile.Mode) (*lockfile.Lock, error) {
	if err := s.fs.ValidateIdentifier(id); err != nil {
		return nil, fmt.Errorf("invalid workspace ID: %w", err)
	}
	return lockfile.Acquire(ctx, filepath.Join(s.workspacesDir, ".locks", id+".lock"), mode, lockfile.DefaultTimeout)
}

func (s *FileStateStore) workspacePath(id string) (string, error) {
	if err := s.fs.ValidateIdentifier(id); err != nil {
		return "", fmt.Errorf("invalid workspace ID: %w", err)
	}

	return filepath.Join(s.workspacesDir, id+".json"), nil
}
