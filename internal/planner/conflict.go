package planner

import (
	"fmt"

	"github.com/danieljhkim/monodev/internal/fsops"
	"github.com/danieljhkim/monodev/internal/state"
)

// ConflictChecker checks for conflicts when applying overlays.
type ConflictChecker struct {
	fs        fsops.FS
	workspace *state.WorkspaceState
	force     bool
}

// NewConflictChecker creates a new ConflictChecker.
func NewConflictChecker(fs fsops.FS, workspace *state.WorkspaceState, force bool) *ConflictChecker {
	return &ConflictChecker{
		fs:        fs,
		workspace: workspace,
		force:     force,
	}
}

// CheckPath checks for conflicts at the given destination path.
// Returns a Conflict if one is detected, or nil if the path is safe to use.
func (c *ConflictChecker) CheckPath(destPath, incomingType, incomingMode, incomingStore string) *Conflict {
	// Check if path exists on filesystem
	exists, err := c.fs.Exists(destPath)
	if err != nil {
		return &Conflict{
			Path:     destPath,
			Reason:   fmt.Sprintf("Failed to check path: %v", err),
			Existing: "unknown",
			Incoming: incomingType,
		}
	}

	if !exists {
		// Path doesn't exist - no conflict
		return nil
	}

	// Path exists - check if it's managed by monodev
	ownership, isManaged := c.workspace.Paths[destPath]

	if !isManaged {
		// Unmanaged path exists - this is a conflict unless force is enabled
		if !c.force {
			return &Conflict{
				Path:     destPath,
				Reason:   "Unmanaged file/directory exists at destination",
				Existing: "unmanaged",
				Incoming: incomingType,
			}
		}
		// Force is enabled - allow overwrite
		return nil
	}

	// Path is managed - check for compatibility

	// Check mode conflict (symlink vs copy)
	if ownership.Type != incomingMode {
		if !c.force {
			return &Conflict{
				Path:     destPath,
				Reason:   fmt.Sprintf("Mode mismatch: existing is %s, incoming is %s", ownership.Type, incomingMode),
				Existing: ownership.Type,
				Incoming: incomingMode,
			}
		}
		// Force is enabled - allow mode change
		return nil
	}

	// Check type conflict (file vs directory)
	existingInfo, err := c.fs.Lstat(destPath)
	if err != nil {
		return &Conflict{
			Path:     destPath,
			Reason:   fmt.Sprintf("Failed to stat existing path: %v", err),
			Existing: "unknown",
			Incoming: incomingType,
		}
	}

	existingIsDir := existingInfo.IsDir()
	incomingIsDir := (incomingType == "directory")

	if existingIsDir != incomingIsDir {
		if !c.force {
			existingType := "file"
			if existingIsDir {
				existingType = "directory"
			}
			return &Conflict{
				Path:     destPath,
				Reason:   fmt.Sprintf("Type mismatch: existing is %s, incoming is %s", existingType, incomingType),
				Existing: existingType,
				Incoming: incomingType,
			}
		}
		// Force is enabled - allow type change
		return nil
	}

	// Validate symlink if in symlink mode
	if ownership.Type == "symlink" {
		target, err := c.fs.Readlink(destPath)
		if err != nil {
			// Path exists but isn't a symlink or can't be read
			if !c.force {
				return &Conflict{
					Path:     destPath,
					Reason:   "Expected symlink but found non-symlink",
					Existing: "non-symlink",
					Incoming: "symlink",
				}
			}
			return nil
		}

		// Symlink target validation is done during plan execution
		// Here we just verify it's a symlink
		_ = target
	}

	// No conflict - this is a store-to-store override
	// Later stores take precedence, so this is allowed
	return nil
}

// IsPathManaged returns true if the path is managed by monodev.
func (c *ConflictChecker) IsPathManaged(destPath string) bool {
	_, isManaged := c.workspace.Paths[destPath]
	return isManaged
}

// GetOwnership returns the ownership info for a managed path.
// Returns nil if the path is not managed.
func (c *ConflictChecker) GetOwnership(destPath string) *state.PathOwnership {
	if ownership, ok := c.workspace.Paths[destPath]; ok {
		return &ownership
	}
	return nil
}
