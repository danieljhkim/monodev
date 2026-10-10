package sync

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/danieljhkim/monodev/internal/gitx"
	"github.com/danieljhkim/monodev/internal/state"
)

// WorkspaceReferenceMatch describes a persisted workspace reference that
// belongs to the same repository and workspace path as the caller.
type WorkspaceReferenceMatch struct {
	// WorkspaceID is the reference's own workspace ID, which names its file on
	// the persistence branch and is what PullRequest.WorkspaceID expects.
	WorkspaceID string

	// ActiveStore is the store that was active when the reference was pushed.
	ActiveStore string

	// AppliedStores lists the stores applied when the reference was pushed,
	// in ledger order.
	AppliedStores []string

	// StoreIDs is every store the reference names, which a restore must pull.
	StoreIDs []string

	// GeneratedAt is when the reference was pushed.
	GeneratedAt time.Time
}

// FindWorkspaceReference scans the persistence work tree on disk for a
// workspace reference whose repository identity and workspace path match the
// caller's. Workspace IDs are machine-specific, so the match is by identity
// and path rather than by ID. When several machines pushed a reference for
// the same path, the most recently generated one wins. It returns nil when
// nothing matches. References that are unreadable or from a newer schema are
// skipped; the pull that restores a match validates it in full.
func (s *Syncer) FindWorkspaceReference(repoRoot, repositoryIdentity, workspacePath string) (*WorkspaceReferenceMatch, error) {
	if repoRoot == "" || repositoryIdentity == "" {
		return nil, nil
	}
	dir := workspaceReferencesDir(repoRoot)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to read persisted workspace references: %w", err)
	}

	localPath := filepath.Clean(workspacePath)
	var best *WorkspaceReferenceMatch
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".json") {
			continue
		}
		id := strings.TrimSuffix(name, ".json")
		if s.fs.ValidateIdentifier(id) != nil {
			continue
		}
		refPath := filepath.Join(dir, name)
		data, err := s.fs.ReadFile(refPath)
		if err != nil {
			continue
		}
		if _, err := state.CheckSchemaVersion(refPath, data, workspaceReferenceSchemaVersion); err != nil {
			continue
		}
		var ref workspaceReference
		if err := json.Unmarshal(data, &ref); err != nil || ref.WorkspaceID != id {
			continue
		}
		if !gitx.SameRemoteIdentity(ref.Repo, repositoryIdentity) || filepath.Clean(ref.WorkspacePath) != localPath {
			continue
		}
		match := &WorkspaceReferenceMatch{
			WorkspaceID:   id,
			ActiveStore:   ref.ActiveStore,
			AppliedStores: make([]string, 0, len(ref.AppliedStores)),
			StoreIDs:      workspaceReferenceStoreIDs(&ref),
			GeneratedAt:   ref.GeneratedAt,
		}
		for _, applied := range ref.AppliedStores {
			match.AppliedStores = append(match.AppliedStores, applied.Store)
		}
		if best == nil || match.GeneratedAt.After(best.GeneratedAt) ||
			(match.GeneratedAt.Equal(best.GeneratedAt) && match.WorkspaceID < best.WorkspaceID) {
			best = match
		}
	}
	return best, nil
}
