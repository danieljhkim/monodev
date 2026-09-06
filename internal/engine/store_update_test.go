package engine

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/danieljhkim/monodev/internal/clock"
	"github.com/danieljhkim/monodev/internal/config"
	"github.com/danieljhkim/monodev/internal/fsops"
	"github.com/danieljhkim/monodev/internal/stores"
)

func TestCreateStore_WithMetadata(t *testing.T) {
	globalRepo := newScopedMockStoreRepo()
	componentRepo := newScopedMockStoreRepo()
	eng := newScopedTestEngine(globalRepo, componentRepo)

	err := eng.CreateStore(context.Background(), &CreateStoreRequest{
		CWD:         "/repo",
		StoreID:     "meta-store",
		Name:        "meta-store",
		Scope:       stores.ScopeGlobal,
		Description: "test desc",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	meta := globalRepo.metas["meta-store"]
	if meta == nil {
		t.Fatal("expected store meta to be created")
	}
	if meta.SchemaVersion != 2 {
		t.Errorf("SchemaVersion = %d, want 2", meta.SchemaVersion)
	}
	if meta.Description != "test desc" {
		t.Errorf("Description = %s, want 'test desc'", meta.Description)
	}
}

func TestUpdateStore_Success(t *testing.T) {
	globalRepo := newScopedMockStoreRepo()
	globalRepo.storeIDs["my-store"] = true
	globalRepo.metas["my-store"] = stores.NewStoreMeta("my-store", time.Now())

	eng := newScopedTestEngine(globalRepo, nil)

	newDesc := "updated"
	err := eng.UpdateStore(context.Background(), &UpdateStoreRequest{
		StoreID:     "my-store",
		Description: &newDesc,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	meta := globalRepo.metas["my-store"]
	if meta.Description != "updated" {
		t.Errorf("Description = %s, want 'updated'", meta.Description)
	}
}

func TestUpdateStore_PartialUpdate(t *testing.T) {
	globalRepo := newScopedMockStoreRepo()
	meta := stores.NewStoreMeta("my-store", time.Now())
	meta.Name = "my-store"
	globalRepo.storeIDs["my-store"] = true
	globalRepo.metas["my-store"] = meta

	eng := newScopedTestEngine(globalRepo, nil)

	newDesc := "updated description"
	err := eng.UpdateStore(context.Background(), &UpdateStoreRequest{
		StoreID:     "my-store",
		Description: &newDesc,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	updated := globalRepo.metas["my-store"]
	if updated.Description != "updated description" {
		t.Errorf("Description = %s, want 'updated description'", updated.Description)
	}
	if updated.Name != "my-store" {
		t.Errorf("Name = %s, want 'my-store' (unchanged)", updated.Name)
	}
}

func TestUpdateStore_NotFound(t *testing.T) {
	globalRepo := newScopedMockStoreRepo()
	eng := newScopedTestEngine(globalRepo, nil)

	newDesc := "bob"
	err := eng.UpdateStore(context.Background(), &UpdateStoreRequest{
		StoreID:     "nonexistent",
		Description: &newDesc,
	})
	if err == nil {
		t.Fatal("expected error for non-existent store")
	}
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestTrackRequest_MetadataFields(t *testing.T) {
	req := &TrackRequest{
		CWD:         "/test/workspace",
		Paths:       []string{"file.txt"},
		Role:        stores.RoleConfig,
		Description: "app config",
		Origin:      stores.OriginUser,
	}

	if req.Role != stores.RoleConfig {
		t.Errorf("Role = %s, want %s", req.Role, stores.RoleConfig)
	}
	if req.Description != "app config" {
		t.Errorf("Description = %s, want 'app config'", req.Description)
	}
	if req.Origin != stores.OriginUser {
		t.Errorf("Origin = %s, want %s", req.Origin, stores.OriginUser)
	}
}

func TestDescribeStore_TrackedPathsType(t *testing.T) {
	globalRepo := newScopedMockStoreRepo()
	now := time.Now()
	globalRepo.storeIDs["my-store"] = true
	globalRepo.metas["my-store"] = stores.NewStoreMeta("my-store", now)
	globalRepo.tracks["my-store"] = &stores.TrackFile{
		SchemaVersion: 2,
		Tracked: []stores.TrackedPath{
			{Path: "file.txt", Kind: "file", Role: stores.RoleScript},
			{Path: "config.yaml", Kind: "file", Role: stores.RoleConfig, Description: "app config"},
		},
	}

	eng := newScopedTestEngine(globalRepo, nil)

	results, err := eng.DescribeStore(context.Background(), "my-store")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if len(results[0].TrackedPaths) != 2 {
		t.Fatalf("expected 2 tracked paths, got %d", len(results[0].TrackedPaths))
	}
	if results[0].TrackedPaths[0].Role != stores.RoleScript {
		t.Errorf("TrackedPaths[0].Role = %s, want %s", results[0].TrackedPaths[0].Role, stores.RoleScript)
	}
	if results[0].TrackedPaths[1].Description != "app config" {
		t.Errorf("TrackedPaths[1].Description = %s, want 'app config'", results[0].TrackedPaths[1].Description)
	}
}

func TestCloneStore_CopiesCommittedOverlayAndTrackingIndependently(t *testing.T) {
	cloneTime := time.Date(2030, 1, 2, 3, 4, 5, 0, time.UTC)
	eng, repo := newCloneTestEngine(t, fsops.NewRealFS(), cloneTime)
	createCloneSource(t, repo)

	if err := eng.CloneStore(context.Background(), &CloneStoreRequest{
		SourceID:      "source",
		DestinationID: "variant",
	}); err != nil {
		t.Fatalf("CloneStore() error = %v", err)
	}

	destinationMeta, err := repo.LoadMeta("variant")
	if err != nil {
		t.Fatalf("LoadMeta(destination): %v", err)
	}
	if destinationMeta.Name != "variant" {
		t.Errorf("destination name = %q, want variant", destinationMeta.Name)
	}
	if destinationMeta.Description != "source context" {
		t.Errorf("destination description = %q, want source context", destinationMeta.Description)
	}
	if !destinationMeta.CreatedAt.Equal(cloneTime) || !destinationMeta.UpdatedAt.Equal(cloneTime) {
		t.Errorf("destination timestamps = %v, %v, want %v", destinationMeta.CreatedAt, destinationMeta.UpdatedAt, cloneTime)
	}

	sourcePath := filepath.Join(repo.OverlayRoot("source"), "bin", "tool")
	destinationPath := filepath.Join(repo.OverlayRoot("variant"), "bin", "tool")
	sourceData, err := os.ReadFile(sourcePath)
	if err != nil {
		t.Fatalf("ReadFile(source): %v", err)
	}
	destinationData, err := os.ReadFile(destinationPath)
	if err != nil {
		t.Fatalf("ReadFile(destination): %v", err)
	}
	if string(destinationData) != string(sourceData) {
		t.Errorf("destination content = %q, want %q", destinationData, sourceData)
	}
	destinationInfo, err := os.Stat(destinationPath)
	if err != nil {
		t.Fatalf("Stat(destination): %v", err)
	}
	if destinationInfo.Mode()&0100 == 0 {
		t.Errorf("destination executable mode = %o, want executable", destinationInfo.Mode().Perm())
	}

	destinationTrack, err := repo.LoadTrack("variant")
	if err != nil {
		t.Fatalf("LoadTrack(destination): %v", err)
	}
	if len(destinationTrack.Tracked) != 1 || destinationTrack.Tracked[0].Role != stores.RoleScript || destinationTrack.Tracked[0].Origin != stores.OriginAgent {
		t.Fatalf("destination tracking metadata = %#v, want preserved role and origin", destinationTrack.Tracked)
	}
	if destinationTrack.Notes != "agent context" || len(destinationTrack.Ignore) != 1 || destinationTrack.Ignore[0] != "*.tmp" {
		t.Errorf("destination tracking metadata = %#v, want notes and ignore patterns", destinationTrack)
	}

	if err := os.WriteFile(destinationPath, []byte("variant\n"), 0600); err != nil {
		t.Fatalf("WriteFile(destination): %v", err)
	}
	destinationTrack.Tracked[0].Role = stores.RoleDocs
	if err := repo.SaveTrack("variant", destinationTrack); err != nil {
		t.Fatalf("SaveTrack(destination): %v", err)
	}

	sourceData, err = os.ReadFile(sourcePath)
	if err != nil {
		t.Fatalf("ReadFile(source after destination mutation): %v", err)
	}
	if string(sourceData) != "#!/bin/sh\nprintf 'source\x00bytes'\n" {
		t.Errorf("source content after destination mutation = %q", sourceData)
	}
	sourceTrack, err := repo.LoadTrack("source")
	if err != nil {
		t.Fatalf("LoadTrack(source): %v", err)
	}
	if sourceTrack.Tracked[0].Role != stores.RoleScript {
		t.Errorf("source role after destination mutation = %q, want script", sourceTrack.Tracked[0].Role)
	}
}

func TestCloneStore_RefusesExistingDestination(t *testing.T) {
	eng, repo := newCloneTestEngine(t, fsops.NewRealFS(), time.Now())
	createCloneSource(t, repo)
	if err := repo.Create("destination", stores.NewStoreMeta("destination", time.Now())); err != nil {
		t.Fatalf("Create(destination): %v", err)
	}

	err := eng.CloneStore(context.Background(), &CloneStoreRequest{SourceID: "source", DestinationID: "destination"})
	if err == nil {
		t.Fatal("CloneStore() error = nil, want existing destination refusal")
	}
	if got := err.Error(); got != "store already exists: destination" {
		t.Errorf("CloneStore() error = %q, want existing-destination error", got)
	}
	meta, err := repo.LoadMeta("destination")
	if err != nil {
		t.Fatalf("LoadMeta(destination): %v", err)
	}
	if meta.Name != "destination" {
		t.Errorf("existing destination was overwritten: %#v", meta)
	}
}

func TestCloneStore_CleansUpDestinationWhenCopyFails(t *testing.T) {
	filesystem := cloneCopyFailingFS{FS: fsops.NewRealFS(), err: errors.New("injected copy failure")}
	eng, repo := newCloneTestEngine(t, filesystem, time.Now())
	createCloneSource(t, repo)

	err := eng.CloneStore(context.Background(), &CloneStoreRequest{SourceID: "source", DestinationID: "destination"})
	if err == nil || !errors.Is(err, filesystem.err) {
		t.Fatalf("CloneStore() error = %v, want injected copy failure", err)
	}
	exists, err := repo.Exists("destination")
	if err != nil {
		t.Fatalf("Exists(destination): %v", err)
	}
	if exists {
		t.Fatal("destination remains visible after copy failure")
	}
}

func newCloneTestEngine(t *testing.T, filesystem fsops.FS, cloneTime time.Time) (*Engine, *stores.FileStoreRepo) {
	t.Helper()
	repo := stores.NewFileStoreRepo(filesystem, t.TempDir())
	eng := New(
		&mockGitRepo{},
		repo,
		newMockStateStore(),
		filesystem,
		&mockHasher{},
		clock.NewFakeClock(cloneTime),
		config.Paths{},
	)
	return eng, repo
}

func createCloneSource(t *testing.T, repo *stores.FileStoreRepo) {
	t.Helper()
	sourceTime := time.Date(2025, 1, 2, 3, 4, 5, 0, time.UTC)
	meta := stores.NewStoreMeta("source", sourceTime)
	meta.Description = "source context"
	if err := repo.Create("source", meta); err != nil {
		t.Fatalf("Create(source): %v", err)
	}
	track := stores.NewTrackFile()
	track.Tracked = []stores.TrackedPath{{Path: "bin/tool", Kind: "file", Role: stores.RoleScript, Origin: stores.OriginAgent}}
	track.Ignore = []string{"*.tmp"}
	track.Notes = "agent context"
	if err := repo.SaveTrack("source", track); err != nil {
		t.Fatalf("SaveTrack(source): %v", err)
	}
	sourcePath := filepath.Join(repo.OverlayRoot("source"), "bin", "tool")
	if err := os.MkdirAll(filepath.Dir(sourcePath), 0700); err != nil {
		t.Fatalf("MkdirAll(source overlay): %v", err)
	}
	if err := os.WriteFile(sourcePath, []byte("#!/bin/sh\nprintf 'source\x00bytes'\n"), 0755); err != nil {
		t.Fatalf("WriteFile(source overlay): %v", err)
	}
}

type cloneCopyFailingFS struct {
	fsops.FS
	err error
}

func (fs cloneCopyFailingFS) Copy(_, _ string) error { return fs.err }
