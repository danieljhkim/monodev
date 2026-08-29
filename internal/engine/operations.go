package engine

import (
	"fmt"
	"path/filepath"

	"github.com/danieljhkim/monodev/internal/fsops"
	"github.com/danieljhkim/monodev/internal/planner"
)

// executeOperation executes a single operation.
func (e *Engine) executeOperation(workspaceRoot string, op planner.Operation) error {
	if err := e.validateOperationDestination(workspaceRoot, op); err != nil {
		return err
	}

	if rootFS, ok := e.fs.(fsops.RootFS); ok {
		switch op.Type {
		case planner.OpRemove:
			return rootFS.RemoveAllWithinRoot(workspaceRoot, op.RelPath)
		case planner.OpCreateSymlink:
			return rootFS.SymlinkWithinRoot(workspaceRoot, op.RelPath, op.SourcePath)
		case planner.OpCopy:
			return rootFS.CopyWithinRoot(workspaceRoot, op.RelPath, op.SourcePath)
		default:
			return fmt.Errorf("unknown operation type: %s", op.Type)
		}
	}

	// Lightweight test doubles use the legacy absolute-path methods. All
	// production construction uses RealFS and therefore the confined path above.
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

func (e *Engine) validateOperationDestination(workspaceRoot string, op planner.Operation) error {
	if err := e.fs.ValidateRelPath(op.RelPath); err != nil {
		return fmt.Errorf("invalid operation path %q: %w", op.RelPath, err)
	}
	want := filepath.Join(workspaceRoot, filepath.Clean(op.RelPath))
	if filepath.Clean(op.DestPath) != want {
		return fmt.Errorf("operation destination %q does not match workspace-relative path %q", op.DestPath, op.RelPath)
	}
	return nil
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
	if err := e.fs.MkdirAll(parentDir, 0700); err != nil {
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
