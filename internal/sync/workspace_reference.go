package sync

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/danieljhkim/monodev/internal/state"
)

const workspaceReferenceSchemaVersion = 1

type workspaceReference struct {
	SchemaVersion int                           `json:"schemaVersion"`
	WorkspaceID   string                        `json:"workspaceID"`
	Repo          string                        `json:"repo"`
	WorkspacePath string                        `json:"workspacePath"`
	AbsolutePath  string                        `json:"absolutePath,omitempty"`
	Applied       bool                          `json:"applied"`
	Mode          string                        `json:"mode"`
	ActiveStore   string                        `json:"activeStore"`
	ActiveScope   string                        `json:"activeStoreScope,omitempty"`
	Stack         []string                      `json:"stack"`
	AppliedStores []state.AppliedStore          `json:"appliedStores"`
	PathOwnership workspacePathOwnershipSummary `json:"pathOwnership"`
	GeneratedAt   time.Time                     `json:"generatedAt"`
}

type workspacePathOwnershipSummary struct {
	Count int                      `json:"count"`
	Paths []workspacePathOwnership `json:"paths"`
}

type workspacePathOwnership struct {
	Path      string    `json:"path"`
	Store     string    `json:"store"`
	Type      string    `json:"type"`
	Timestamp time.Time `json:"timestamp"`
	Checksum  string    `json:"checksum,omitempty"`
}

func workspaceReferencePath(repoRoot, workspaceID string) string {
	return filepath.Join(repoRoot, ".monodev", "persist", "workspaces", workspaceID+".json")
}

func (s *Syncer) prepareWorkspaceReference(req *PushRequest) (string, []byte, error) {
	if req.WorkspaceID == "" {
		return "", nil, fmt.Errorf("workspace ID is required when pushing workspace references")
	}
	if err := s.fs.ValidateIdentifier(req.WorkspaceID); err != nil {
		return "", nil, fmt.Errorf("invalid workspace ID: %w", err)
	}

	workspaceState, err := s.stateStore.LoadWorkspace(req.WorkspaceID)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil, fmt.Errorf("workspace %q not found", req.WorkspaceID)
		}
		return "", nil, fmt.Errorf("failed to load workspace %q: %w", req.WorkspaceID, err)
	}

	ref := s.buildWorkspaceReference(req, workspaceState)
	data, err := json.MarshalIndent(ref, "", "  ")
	if err != nil {
		return "", nil, fmt.Errorf("failed to encode workspace reference: %w", err)
	}
	data = append(data, '\n')

	refPath := workspaceReferencePath(req.RepoRoot, req.WorkspaceID)
	return refPath, data, nil
}

func (s *Syncer) buildWorkspaceReference(req *PushRequest, workspaceState *state.WorkspaceState) workspaceReference {
	absolutePath := workspaceState.AbsolutePath
	if absolutePath == "" && workspaceState.WorkspacePath != "" {
		absolutePath = filepath.Clean(filepath.Join(req.RepoRoot, workspaceState.WorkspacePath))
	}

	return workspaceReference{
		SchemaVersion: workspaceReferenceSchemaVersion,
		WorkspaceID:   req.WorkspaceID,
		Repo:          workspaceState.Repo,
		WorkspacePath: workspaceState.WorkspacePath,
		AbsolutePath:  absolutePath,
		Applied:       workspaceState.Applied,
		Mode:          workspaceState.Mode,
		ActiveStore:   workspaceState.ActiveStore,
		ActiveScope:   workspaceState.ActiveStoreScope,
		Stack:         append([]string(nil), workspaceState.Stack...),
		AppliedStores: append([]state.AppliedStore(nil), workspaceState.AppliedStores...),
		PathOwnership: summarizePathOwnership(workspaceState.Paths),
		GeneratedAt:   s.clock.Now(),
	}
}

func summarizePathOwnership(paths map[string]state.PathOwnership) workspacePathOwnershipSummary {
	summary := workspacePathOwnershipSummary{
		Count: len(paths),
		Paths: make([]workspacePathOwnership, 0, len(paths)),
	}

	for path, ownership := range paths {
		summary.Paths = append(summary.Paths, workspacePathOwnership{
			Path:      path,
			Store:     ownership.Store,
			Type:      ownership.Type,
			Timestamp: ownership.Timestamp,
			Checksum:  ownership.Checksum,
		})
	}

	sort.Slice(summary.Paths, func(i, j int) bool {
		return summary.Paths[i].Path < summary.Paths[j].Path
	})

	return summary
}
