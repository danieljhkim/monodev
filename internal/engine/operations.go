package engine

import (
	"fmt"
	"path/filepath"

	"github.com/danieljhkim/monodev/internal/planner"
)

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
