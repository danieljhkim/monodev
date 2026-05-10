package sync

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/danieljhkim/monodev/internal/remote"
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

// pushStore implements the push operation for stores.
func (s *Syncer) pushStore(ctx context.Context, req *PushRequest) (*PushResult, error) {
	// Validate request
	if req.RepoRoot == "" {
		return nil, fmt.Errorf("repo root is required")
	}
	if err := checkContext(ctx); err != nil {
		return nil, err
	}

	// If no store IDs specified, push all local stores
	storeIDs := req.StoreIDs
	if len(storeIDs) == 0 && !req.WithWorkspace {
		if err := checkContext(ctx); err != nil {
			return nil, err
		}
		allStores, err := s.storeRepo.List()
		if err != nil {
			return nil, fmt.Errorf("failed to list local stores: %w", err)
		}
		if len(allStores) == 0 {
			return nil, fmt.Errorf("no stores found to push")
		}
		storeIDs = allStores
	}

	// Load or create remote config
	if err := checkContext(ctx); err != nil {
		return nil, err
	}
	config, err := s.loadOrCreateConfig(req.RepoRoot, req.Remote)
	if err != nil {
		return nil, err
	}

	// Ensure persistence repo exists
	if !req.DryRun {
		if err := checkContext(ctx); err != nil {
			return nil, err
		}
		if err := s.git.EnsureRepo(ctx, req.RepoRoot, config.Branch); err != nil {
			return nil, fmt.Errorf("failed to ensure persistence repo: %w", err)
		}

		// Get the remote URL from the main repository
		if err := checkContext(ctx); err != nil {
			return nil, err
		}
		remoteURL, err := s.git.GetRemoteURL(ctx, req.RepoRoot, config.Remote)
		if err != nil {
			return nil, fmt.Errorf("failed to get remote URL: %w", err)
		}

		// Configure the remote in the persistence repository
		if err := checkContext(ctx); err != nil {
			return nil, err
		}
		if err := s.git.SetRemote(ctx, req.RepoRoot, config.Remote, remoteURL); err != nil {
			return nil, fmt.Errorf("failed to set remote: %w", err)
		}
	}

	var workspaceRefPath string
	var workspaceRefData []byte
	var pushedWorkspace bool
	if req.WithWorkspace {
		if err := checkContext(ctx); err != nil {
			return nil, err
		}
		refPath, refData, err := s.prepareWorkspaceReference(req)
		if err != nil {
			return nil, err
		}
		workspaceRefPath = refPath
		workspaceRefData = refData
		pushedWorkspace = true
	}

	// Materialize stores to .monodev/persist/stores/
	var pushedStores []string
	for _, storeID := range storeIDs {
		if err := checkContext(ctx); err != nil {
			return nil, err
		}
		if !req.DryRun {
			if err := s.snapshotMgr.Materialize(storeID, s.storeRepo, req.RepoRoot); err != nil {
				return nil, fmt.Errorf("failed to materialize store %q: %w", storeID, err)
			}
		}
		pushedStores = append(pushedStores, storeID)
	}

	if !req.DryRun && pushedWorkspace {
		if err := checkContext(ctx); err != nil {
			return nil, err
		}
		if err := s.fs.AtomicWrite(workspaceRefPath, workspaceRefData, 0644); err != nil {
			return nil, fmt.Errorf("failed to write workspace reference: %w", err)
		}
	}

	// Build commit message
	commitMessage := s.buildPushCommitMessage(pushedStores, pushedWorkspace)

	// Stage and commit changes
	if !req.DryRun {
		persistDir := filepath.Join(req.RepoRoot, ".monodev", "persist")
		if err := checkContext(ctx); err != nil {
			return nil, err
		}
		if err := s.git.Commit(ctx, req.RepoRoot, commitMessage, []string{persistDir}); err != nil {
			return nil, fmt.Errorf("failed to commit: %w", err)
		}

		// Push to remote
		if err := checkContext(ctx); err != nil {
			return nil, err
		}
		if err := s.git.Push(ctx, req.RepoRoot, config.Remote, config.Branch, req.Force); err != nil {
			return nil, fmt.Errorf("failed to push: %w", err)
		}
	}

	return &PushResult{
		PushedStores:     pushedStores,
		PushedWorkspace:  pushedWorkspace,
		WorkspaceID:      req.WorkspaceID,
		WorkspaceRefPath: workspaceRefPath,
		CommitMessage:    commitMessage,
		Remote:           config.Remote,
		Branch:           config.Branch,
		DryRun:           req.DryRun,
	}, nil
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

// loadOrCreateConfig loads the remote config, or creates a default one if it doesn't exist.
func (s *Syncer) loadOrCreateConfig(repoRoot, remoteName string) (*remote.RemoteConfig, error) {
	config, err := s.configStore.Load(repoRoot)
	if err != nil {
		if err == remote.ErrRemoteNotConfigured {
			// Create default config
			config = remote.DefaultRemoteConfig()
			if remoteName != "" {
				config.Remote = remoteName
			}
			config.UpdatedAt = s.clock.Now()

			// Save the config
			if err := s.configStore.Save(repoRoot, config); err != nil {
				return nil, fmt.Errorf("failed to save default config: %w", err)
			}
		} else {
			return nil, fmt.Errorf("failed to load remote config: %w", err)
		}
	}

	// Override remote if specified in request
	if remoteName != "" && remoteName != config.Remote {
		config.Remote = remoteName
		config.UpdatedAt = time.Now()
		if err := s.configStore.Save(repoRoot, config); err != nil {
			return nil, fmt.Errorf("failed to update config: %w", err)
		}
	}

	return config, nil
}

// buildPushCommitMessage builds a commit message for a push operation.
func (s *Syncer) buildPushCommitMessage(storeIDs []string, withWorkspace bool) string {
	var parts []string

	if len(storeIDs) > 0 {
		if len(storeIDs) == 1 {
			parts = append(parts, fmt.Sprintf("store %s", storeIDs[0]))
		} else {
			parts = append(parts, fmt.Sprintf("%d stores", len(storeIDs)))
		}
	}

	if withWorkspace {
		parts = append(parts, "workspace")
	}

	return fmt.Sprintf("push: %s", strings.Join(parts, ", "))
}
