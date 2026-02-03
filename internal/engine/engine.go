// Package engine provides the core business logic for monodev operations.
//
// The engine package acts as the orchestration layer between CLI commands and
// lower-level operations. It coordinates workspace discovery, state management,
// store operations, and overlay application/removal.
//
// Key components:
//   - Engine: Main orchestrator that coordinates all operations
//   - Apply/Unapply: Manages overlay application and removal
//   - Track/Commit: Handles tracking and persisting changes
//   - State management: Workspace and store state operations
package engine

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/danieljhkim/monodev/internal/clock"
	"github.com/danieljhkim/monodev/internal/config"
	"github.com/danieljhkim/monodev/internal/fsops"
	"github.com/danieljhkim/monodev/internal/gitx"
	"github.com/danieljhkim/monodev/internal/hash"
	"github.com/danieljhkim/monodev/internal/planner"
	"github.com/danieljhkim/monodev/internal/state"
	"github.com/danieljhkim/monodev/internal/stores"
)

// Engine orchestrates all monodev operations.
// It is the main API surface called by the CLI.
type Engine struct {
	gitRepo     gitx.GitRepo
	storeRepo   stores.StoreRepo
	stateStore  state.StateStore
	fs          fsops.FS
	hasher      hash.Hasher
	clock       clock.Clock
	configPaths config.Paths
}

// New creates a new Engine with the given dependencies.
func New(
	gitRepo gitx.GitRepo,
	storeRepo stores.StoreRepo,
	stateStore state.StateStore,
	fs fsops.FS,
	hasher hash.Hasher,
	clk clock.Clock,
	paths config.Paths,
) *Engine {
	return &Engine{
		gitRepo:     gitRepo,
		storeRepo:   storeRepo,
		stateStore:  stateStore,
		fs:          fs,
		hasher:      hasher,
		clock:       clk,
		configPaths: paths,
	}
}

// executeOperation executes a single operation.
func (e *Engine) executeOperation(op planner.Operation) error {
	switch op.Type {
	case planner.OpRemove:
		return e.executeRemove(op)
	case planner.OpCreateSymlink:
		return e.executeCreateSymlink(op)
	case planner.OpCopy:
		return e.executeCopy(op)
	default:
		return fmt.Errorf("unknown operation type: %s", op.Type)
	}
}

// executeRemove removes a path.
func (e *Engine) executeRemove(op planner.Operation) error {
	exists, err := e.fs.Exists(op.DestPath)
	if err != nil {
		return fmt.Errorf("failed to check if path exists: %w", err)
	}
	if !exists {
		return nil
	}
	if err := e.fs.RemoveAll(op.DestPath); err != nil {
		return fmt.Errorf("failed to remove path: %w", err)
	}

	return nil
}

// executeCreateSymlink creates a symlink.
func (e *Engine) executeCreateSymlink(op planner.Operation) error {
	// Create parent directory if needed
	parentDir := filepath.Dir(op.DestPath)
	if err := e.fs.MkdirAll(parentDir, 0755); err != nil {
		return fmt.Errorf("failed to create parent directory: %w", err)
	}
	if err := e.fs.Symlink(op.SourcePath, op.DestPath); err != nil {
		return fmt.Errorf("failed to create symlink: %w", err)
	}

	return nil
}

// executeCopy copies a file or directory.
func (e *Engine) executeCopy(op planner.Operation) error {
	if err := e.fs.Copy(op.SourcePath, op.DestPath); err != nil {
		return fmt.Errorf("failed to copy: %w", err)
	}

	return nil
}

// discoverWorkspace returns repo root, fingerprint, and workspace path
func (e *Engine) DiscoverWorkspace(cwd string) (root, fingerprint, workspacePath string, err error) {
	root, err = e.gitRepo.Discover(cwd)
	if err != nil {
		fmt.Printf("failed to discover git repo. Using absolute path for workspace fingerprint: %v\n", err)
		root = cwd
	}

	fingerprint, err = e.gitRepo.Fingerprint(root)
	if err != nil {
		return "", "", "", fmt.Errorf("failed to get workspace fingerprint: %w", err)
	}

	workspacePath, err = e.gitRepo.RelPath(root, cwd)
	if err != nil {
		return "", "", "", fmt.Errorf("failed to compute workspace path: %w", err)
	}

	return root, fingerprint, workspacePath, nil
}

func (e *Engine) LoadOrCreateWorkspaceState(repoFingerprint, workspacePath, mode string) (*state.WorkspaceState, string, error) {
	workspaceID := state.ComputeWorkspaceID(repoFingerprint, workspacePath)
	workspaceState, err := e.stateStore.LoadWorkspace(workspaceID)
	if err != nil {
		if os.IsNotExist(err) {
			workspaceState = state.NewWorkspaceState(repoFingerprint, workspacePath, mode)
		} else {
			return nil, workspaceID, fmt.Errorf("failed to load workspace state: %w", err)
		}
	}
	return workspaceState, workspaceID, nil
}

// touchStoreMeta updates the UpdatedAt timestamp of a store's metadata.
// This is used to track when a store was last modified.
func (e *Engine) touchStoreMeta(storeID string) error {
	meta, err := e.storeRepo.LoadMeta(storeID)
	if err != nil {
		return fmt.Errorf("failed to load store metadata: %w", err)
	}

	meta.UpdatedAt = e.clock.Now()
	if err := e.storeRepo.SaveMeta(storeID, meta); err != nil {
		return fmt.Errorf("failed to save store metadata: %w", err)
	}

	return nil
}
