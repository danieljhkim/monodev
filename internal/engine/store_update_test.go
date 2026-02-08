package engine

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/danieljhkim/monodev/internal/stores"
)

func TestCreateStore_WithMetadata(t *testing.T) {
	globalRepo := newScopedMockStoreRepo()
	componentRepo := newScopedMockStoreRepo()
	eng := newScopedTestEngine(globalRepo, componentRepo)

	err := eng.CreateStore(context.Background(), &CreateStoreRequest{
		CWD:          "/repo",
		StoreID:      "meta-store",
		Name:         "meta-store",
		Scope:        stores.ScopeGlobal,
		Description:  "test desc",
		Source:       stores.SourceAgent,
		Type:         stores.TypeTask,
		Owner:        "alice",
		TaskID:       "T-1",
		ParentTaskID: "T-0",
		Priority:     stores.PriorityHigh,
		Status:       stores.StatusTodo,
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
	if meta.Source != stores.SourceAgent {
		t.Errorf("Source = %s, want %s", meta.Source, stores.SourceAgent)
	}
	if meta.Type != stores.TypeTask {
		t.Errorf("Type = %s, want %s", meta.Type, stores.TypeTask)
	}
	if meta.Owner != "alice" {
		t.Errorf("Owner = %s, want 'alice'", meta.Owner)
	}
	if meta.TaskID != "T-1" {
		t.Errorf("TaskID = %s, want 'T-1'", meta.TaskID)
	}
	if meta.ParentTaskID != "T-0" {
		t.Errorf("ParentTaskID = %s, want 'T-0'", meta.ParentTaskID)
	}
	if meta.Priority != stores.PriorityHigh {
		t.Errorf("Priority = %s, want %s", meta.Priority, stores.PriorityHigh)
	}
	if meta.Status != stores.StatusTodo {
		t.Errorf("Status = %s, want %s", meta.Status, stores.StatusTodo)
	}
}

func TestCreateStore_InvalidMetadata(t *testing.T) {
	globalRepo := newScopedMockStoreRepo()
	eng := newScopedTestEngine(globalRepo, nil)

	err := eng.CreateStore(context.Background(), &CreateStoreRequest{
		CWD:     "/repo",
		StoreID: "bad-store",
		Name:    "bad-store",
		Scope:   stores.ScopeGlobal,
		Source:  "invalid_source",
	})
	if err == nil {
		t.Fatal("expected validation error for invalid source")
	}
}

func TestUpdateStore_Success(t *testing.T) {
	globalRepo := newScopedMockStoreRepo()
	globalRepo.storeIDs["my-store"] = true
	globalRepo.metas["my-store"] = stores.NewStoreMeta("my-store", stores.ScopeGlobal, time.Now())

	eng := newScopedTestEngine(globalRepo, nil)

	newStatus := stores.StatusInProgress
	newOwner := "bob"
	err := eng.UpdateStore(context.Background(), &UpdateStoreRequest{
		StoreID: "my-store",
		Status:  &newStatus,
		Owner:   &newOwner,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	meta := globalRepo.metas["my-store"]
	if meta.Status != stores.StatusInProgress {
		t.Errorf("Status = %s, want %s", meta.Status, stores.StatusInProgress)
	}
	if meta.Owner != "bob" {
		t.Errorf("Owner = %s, want 'bob'", meta.Owner)
	}
}

func TestUpdateStore_PartialUpdate(t *testing.T) {
	globalRepo := newScopedMockStoreRepo()
	meta := stores.NewStoreMeta("my-store", stores.ScopeGlobal, time.Now())
	meta.Source = stores.SourceHuman
	meta.Priority = stores.PriorityLow
	globalRepo.storeIDs["my-store"] = true
	globalRepo.metas["my-store"] = meta

	eng := newScopedTestEngine(globalRepo, nil)

	// Only update status, leave other fields untouched
	newStatus := stores.StatusDone
	err := eng.UpdateStore(context.Background(), &UpdateStoreRequest{
		StoreID: "my-store",
		Status:  &newStatus,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	updated := globalRepo.metas["my-store"]
	if updated.Status != stores.StatusDone {
		t.Errorf("Status = %s, want %s", updated.Status, stores.StatusDone)
	}
	// Unchanged fields should remain
	if updated.Source != stores.SourceHuman {
		t.Errorf("Source = %s, want %s (unchanged)", updated.Source, stores.SourceHuman)
	}
	if updated.Priority != stores.PriorityLow {
		t.Errorf("Priority = %s, want %s (unchanged)", updated.Priority, stores.PriorityLow)
	}
}

func TestUpdateStore_NotFound(t *testing.T) {
	globalRepo := newScopedMockStoreRepo()
	eng := newScopedTestEngine(globalRepo, nil)

	newStatus := stores.StatusDone
	err := eng.UpdateStore(context.Background(), &UpdateStoreRequest{
		StoreID: "nonexistent",
		Status:  &newStatus,
	})
	if err == nil {
		t.Fatal("expected error for non-existent store")
	}
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestUpdateStore_Validation(t *testing.T) {
	globalRepo := newScopedMockStoreRepo()
	globalRepo.storeIDs["my-store"] = true
	globalRepo.metas["my-store"] = stores.NewStoreMeta("my-store", stores.ScopeGlobal, time.Now())

	eng := newScopedTestEngine(globalRepo, nil)

	badStatus := "invalid_status"
	err := eng.UpdateStore(context.Background(), &UpdateStoreRequest{
		StoreID: "my-store",
		Status:  &badStatus,
	})
	if err == nil {
		t.Fatal("expected validation error for invalid status")
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
	globalRepo.metas["my-store"] = stores.NewStoreMeta("my-store", stores.ScopeGlobal, now)
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
