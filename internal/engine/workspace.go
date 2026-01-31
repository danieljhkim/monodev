package engine

import (
	"context"
	"fmt"
	"os"
	"slices"
	"strings"
)

// ListWorkspaces enumerates all workspace state files and returns summary information.
// Algorithm steps:
// 1. Read all workspace files from ~/.monodev/workspaces
// 2. Load each workspace state file
// 3. Build WorkspaceInfo summary for each
// 4. Skip corrupted files gracefully
// 5. Return sorted list by WorkspacePath
func (e *Engine) ListWorkspaces(ctx context.Context) (*ListWorkspacesResult, error) {
	// Step 1: Read all workspace files
	entries, err := os.ReadDir(e.configPaths.Workspaces)
	if err != nil {
		if os.IsNotExist(err) {
			return &ListWorkspacesResult{Workspaces: []WorkspaceInfo{}}, nil
		}
		return nil, fmt.Errorf("failed to read workspaces directory: %w", err)
	}

	var workspaces []WorkspaceInfo
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}

		// Extract workspace ID (strip .json extension)
		workspaceID := strings.TrimSuffix(entry.Name(), ".json")

		// Step 2: Load workspace state
		ws, err := e.stateStore.LoadWorkspace(workspaceID)
		if err != nil {
			// Step 4: Skip corrupted or unreadable workspace files
			continue
		}

		// Step 3: Build WorkspaceInfo summary
		workspaces = append(workspaces, WorkspaceInfo{
			WorkspaceID:      workspaceID,
			WorkspacePath:    ws.WorkspacePath,
			Repo:             ws.Repo,
			Applied:          ws.Applied,
			Mode:             ws.Mode,
			ActiveStore:      ws.ActiveStore,
			StackCount:       len(ws.Stack),
			AppliedPathCount: len(ws.Paths),
		})
	}

	// Step 5: Sort by WorkspacePath for consistency
	slices.SortFunc(workspaces, func(a, b WorkspaceInfo) int {
		return strings.Compare(a.WorkspacePath, b.WorkspacePath)
	})

	return &ListWorkspacesResult{Workspaces: workspaces}, nil
}

// DescribeWorkspace loads and returns detailed information about a specific workspace.
// Algorithm steps:
// 1. Load workspace state by ID
// 2. Return detailed information
// 3. Error if workspace not found
func (e *Engine) DescribeWorkspace(ctx context.Context, workspaceID string) (*DescribeWorkspaceResult, error) {
	// Step 1: Load workspace state
	ws, err := e.stateStore.LoadWorkspace(workspaceID)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("%w: workspace '%s' not found", ErrNotFound, workspaceID)
		}
		return nil, fmt.Errorf("failed to load workspace: %w", err)
	}

	// Step 2: Return detailed information
	return &DescribeWorkspaceResult{
		WorkspaceID:   workspaceID,
		WorkspacePath: ws.WorkspacePath,
		Repo:          ws.Repo,
		Applied:       ws.Applied,
		Mode:          ws.Mode,
		ActiveStore:   ws.ActiveStore,
		Stack:         ws.Stack,
		AppliedStores: ws.AppliedStores,
		Paths:         ws.Paths,
	}, nil
}

// DeleteWorkspace deletes a workspace state file.
// Algorithm steps:
// 1. Load workspace state (error if not found)
// 2. If DryRun: return preview of what would be deleted
// 3. If Applied==true && len(Paths)>0 && !Force: error with message to unapply first
// 4. Otherwise: call stateStore.DeleteWorkspace(workspaceID)
// 5. Return result with deletion status
func (e *Engine) DeleteWorkspace(ctx context.Context, req *DeleteWorkspaceRequest) (*DeleteWorkspaceResult, error) {
	// Step 1: Load workspace state
	ws, err := e.stateStore.LoadWorkspace(req.WorkspaceID)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("%w: workspace '%s' not found", ErrNotFound, req.WorkspaceID)
		}
		return nil, fmt.Errorf("failed to load workspace: %w", err)
	}

	pathsRemoved := len(ws.Paths)

	// Step 2: Return early if dry-run
	if req.DryRun {
		return &DeleteWorkspaceResult{
			WorkspaceID:   req.WorkspaceID,
			WorkspacePath: ws.WorkspacePath,
			Deleted:       false,
			DryRun:        true,
			PathsRemoved:  pathsRemoved,
		}, nil
	}

	// Step 3: Check if workspace has applied paths and force is not set
	if ws.Applied && len(ws.Paths) > 0 && !req.Force {
		return nil, fmt.Errorf("workspace '%s' has %d applied path(s); unapply first or use --force", req.WorkspaceID, len(ws.Paths))
	}

	// Step 4: Delete workspace
	if err := e.stateStore.DeleteWorkspace(req.WorkspaceID); err != nil {
		return nil, fmt.Errorf("failed to delete workspace: %w", err)
	}

	// Step 5: Return result
	return &DeleteWorkspaceResult{
		WorkspaceID:   req.WorkspaceID,
		WorkspacePath: ws.WorkspacePath,
		Deleted:       true,
		DryRun:        false,
		PathsRemoved:  pathsRemoved,
	}, nil
}
