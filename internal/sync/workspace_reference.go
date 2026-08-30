package sync

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
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

	normalized := state.CloneWorkspaceState(workspaceState)
	normalized.MigrateDeprecatedStack()

	return workspaceReference{
		SchemaVersion: workspaceReferenceSchemaVersion,
		WorkspaceID:   req.WorkspaceID,
		Repo:          workspaceReferenceRepository(req, workspaceState),
		WorkspacePath: workspaceState.WorkspacePath,
		AbsolutePath:  absolutePath,
		Applied:       normalized.Applied,
		Mode:          normalized.Mode,
		ActiveStore:   normalized.ActiveStore,
		ActiveScope:   normalized.ActiveStoreScope,
		Stack:         []string{},
		AppliedStores: append([]state.AppliedStore(nil), normalized.AppliedStores...),
		PathOwnership: summarizePathOwnership(normalized.Paths),
		GeneratedAt:   s.clock.Now(),
	}
}

func workspaceReferenceRepository(req *PushRequest, workspaceState *state.WorkspaceState) string {
	if req.RepositoryIdentity != "" {
		return req.RepositoryIdentity
	}
	return workspaceState.Repo
}

func (s *Syncer) loadWorkspaceReference(req *PullRequest) (*workspaceReference, bool, error) {
	if req.WorkspaceID == "" {
		return nil, false, nil
	}
	if err := s.fs.ValidateIdentifier(req.WorkspaceID); err != nil {
		return nil, false, fmt.Errorf("invalid workspace ID: %w", err)
	}

	data, err := s.fs.ReadFile(workspaceReferencePath(req.RepoRoot, req.WorkspaceID))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, false, fmt.Errorf("workspace reference %q not found", req.WorkspaceID)
		}
		return nil, false, fmt.Errorf("failed to read workspace reference %q: %w", req.WorkspaceID, err)
	}

	var ref workspaceReference
	if err := json.Unmarshal(data, &ref); err != nil {
		return nil, true, fmt.Errorf("invalid workspace reference %q: %w", req.WorkspaceID, err)
	}
	if err := s.validateWorkspaceReference(req, &ref); err != nil {
		return nil, true, err
	}
	return &ref, true, nil
}

func (s *Syncer) validateWorkspaceReference(req *PullRequest, ref *workspaceReference) error {
	if ref.SchemaVersion != workspaceReferenceSchemaVersion {
		return fmt.Errorf("unsupported workspace reference schema version %d", ref.SchemaVersion)
	}
	if ref.WorkspaceID != req.WorkspaceID {
		return fmt.Errorf("workspace reference identity mismatch: requested %q, found %q", req.WorkspaceID, ref.WorkspaceID)
	}
	if req.LocalWorkspaceID == "" || req.RepoFingerprint == "" || req.RepositoryIdentity == "" || req.WorkspacePath == "" {
		return fmt.Errorf("local workspace identity is required when restoring a workspace reference")
	}
	if err := s.fs.ValidateIdentifier(req.LocalWorkspaceID); err != nil {
		return fmt.Errorf("invalid local workspace ID: %w", err)
	}
	if ref.Repo != req.RepositoryIdentity {
		return fmt.Errorf("workspace reference repository mismatch")
	}
	remoteWorkspacePath := filepath.Clean(ref.WorkspacePath)
	localWorkspacePath := filepath.Clean(req.WorkspacePath)
	if filepath.IsAbs(remoteWorkspacePath) || filepath.IsAbs(localWorkspacePath) || remoteWorkspacePath != localWorkspacePath || strings.HasPrefix(remoteWorkspacePath, "..") || strings.HasPrefix(localWorkspacePath, "..") {
		return fmt.Errorf("workspace reference path mismatch: remote %q, local %q", ref.WorkspacePath, req.WorkspacePath)
	}
	if ref.Mode != "copy" && ref.Mode != "symlink" {
		return fmt.Errorf("workspace reference has invalid mode %q", ref.Mode)
	}
	if ref.PathOwnership.Count != len(ref.PathOwnership.Paths) {
		return fmt.Errorf("workspace reference path ownership count mismatch")
	}

	storeIDs := workspaceReferenceStoreIDs(ref)
	stores := make(map[string]struct{}, len(storeIDs))
	for _, storeID := range storeIDs {
		if err := s.fs.ValidateIdentifier(storeID); err != nil {
			return fmt.Errorf("invalid workspace reference store %q: %w", storeID, err)
		}
		stores[storeID] = struct{}{}
	}
	seenPaths := make(map[string]struct{}, len(ref.PathOwnership.Paths))
	for _, ownership := range ref.PathOwnership.Paths {
		if err := s.fs.ValidateRelPath(ownership.Path); err != nil {
			return fmt.Errorf("invalid workspace reference path %q: %w", ownership.Path, err)
		}
		if _, exists := seenPaths[ownership.Path]; exists {
			return fmt.Errorf("duplicate workspace reference path %q", ownership.Path)
		}
		seenPaths[ownership.Path] = struct{}{}
		if _, exists := stores[ownership.Store]; !exists {
			return fmt.Errorf("workspace reference path %q names unknown store %q", ownership.Path, ownership.Store)
		}
	}
	return nil
}

func (s *Syncer) restoreWorkspaceReference(req *PullRequest, ref *workspaceReference) error {
	if existing, err := s.stateStore.LoadWorkspace(req.LocalWorkspaceID); err == nil && existing != nil {
		return fmt.Errorf("local workspace state %q already exists; refusing to overwrite it", req.LocalWorkspaceID)
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("failed to inspect local workspace state: %w", err)
	}

	for _, storeID := range workspaceReferenceStoreIDs(ref) {
		exists, err := s.storeRepo.Exists(storeID)
		if err != nil {
			return fmt.Errorf("failed to check workspace reference store %q: %w", storeID, err)
		}
		if !exists {
			return fmt.Errorf("workspace reference store %q is unavailable locally", storeID)
		}
	}

	// The reference describes a prior machine's applied files. Restoring it must
	// never claim those files on this checkout: normal apply will plan and
	// validate local changes before writing them.
	return s.stateStore.SaveWorkspace(req.LocalWorkspaceID, &state.WorkspaceState{
		Repo:             req.RepoFingerprint,
		WorkspacePath:    req.WorkspacePath,
		AbsolutePath:     filepath.Join(req.RepoRoot, req.WorkspacePath),
		Applied:          false,
		Mode:             ref.Mode,
		Stack:            []string{},
		AppliedStores:    []state.AppliedStore{},
		ActiveStore:      ref.ActiveStore,
		ActiveStoreScope: ref.ActiveScope,
		Paths:            make(map[string]state.PathOwnership),
	})
}

func workspaceReferenceStoreIDs(ref *workspaceReference) []string {
	ids := make([]string, 0, len(ref.Stack)+len(ref.AppliedStores)+1)
	seen := make(map[string]struct{})
	add := func(id string) {
		if id == "" {
			return
		}
		if _, exists := seen[id]; exists {
			return
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	for _, id := range ref.Stack {
		add(id)
	}
	for _, applied := range ref.AppliedStores {
		add(applied.Store)
	}
	add(ref.ActiveStore)
	return ids
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
