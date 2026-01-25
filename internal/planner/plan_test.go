package planner

import (
	"testing"
)

func TestNewApplyPlan(t *testing.T) {
	stores := []string{"store1", "store2", "store3"}
	plan := NewApplyPlan(stores)

	if len(plan.Stores) != 3 {
		t.Errorf("expected 3 stores, got %d", len(plan.Stores))
	}
	if plan.Stores[0] != "store1" || plan.Stores[1] != "store2" || plan.Stores[2] != "store3" {
		t.Errorf("stores not set correctly: %v", plan.Stores)
	}
	if plan.Operations == nil {
		t.Error("expected Operations to be initialized")
	}
	if len(plan.Operations) != 0 {
		t.Errorf("expected empty Operations, got %d", len(plan.Operations))
	}
	if plan.Conflicts == nil {
		t.Error("expected Conflicts to be initialized")
	}
	if len(plan.Conflicts) != 0 {
		t.Errorf("expected empty Conflicts, got %d", len(plan.Conflicts))
	}
}

func TestApplyPlan_HasConflicts(t *testing.T) {
	tests := []struct {
		name      string
		conflicts []Conflict
		wantHas   bool
	}{
		{
			name:      "no conflicts",
			conflicts: []Conflict{},
			wantHas:   false,
		},
		{
			name: "has conflicts",
			conflicts: []Conflict{
				{Path: "Makefile", Reason: "unmanaged file exists"},
			},
			wantHas: true,
		},
		{
			name: "multiple conflicts",
			conflicts: []Conflict{
				{Path: "Makefile", Reason: "unmanaged file exists"},
				{Path: "scripts", Reason: "mode mismatch"},
			},
			wantHas: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plan := NewApplyPlan([]string{"store1"})
			plan.Conflicts = tt.conflicts

			has := plan.HasConflicts()
			if has != tt.wantHas {
				t.Errorf("HasConflicts() = %v, want %v", has, tt.wantHas)
			}
		})
	}
}

func TestApplyPlan_AddOperation(t *testing.T) {
	plan := NewApplyPlan([]string{"store1"})

	op1 := Operation{
		Type:       OpCreateSymlink,
		SourcePath: "/store1/overlay/Makefile",
		DestPath:   "/workspace/Makefile",
		RelPath:    "Makefile",
		Store:      "store1",
	}

	op2 := Operation{
		Type:       OpCopy,
		SourcePath: "/store1/overlay/script.sh",
		DestPath:   "/workspace/script.sh",
		RelPath:    "script.sh",
		Store:      "store1",
	}

	plan.AddOperation(op1)
	if len(plan.Operations) != 1 {
		t.Errorf("expected 1 operation, got %d", len(plan.Operations))
	}
	if plan.Operations[0].Type != OpCreateSymlink {
		t.Errorf("expected operation type %q, got %q", OpCreateSymlink, plan.Operations[0].Type)
	}

	plan.AddOperation(op2)
	if len(plan.Operations) != 2 {
		t.Errorf("expected 2 operations, got %d", len(plan.Operations))
	}
	if plan.Operations[1].Type != OpCopy {
		t.Errorf("expected operation type %q, got %q", OpCopy, plan.Operations[1].Type)
	}
}

func TestApplyPlan_AddConflict(t *testing.T) {
	plan := NewApplyPlan([]string{"store1"})

	conflict1 := Conflict{
		Path:     "Makefile",
		Reason:   "unmanaged file exists",
		Existing: "unmanaged",
		Incoming: "file",
	}

	conflict2 := Conflict{
		Path:     "scripts",
		Reason:   "mode mismatch",
		Existing: "symlink",
		Incoming: "copy",
	}

	plan.AddConflict(conflict1)
	if len(plan.Conflicts) != 1 {
		t.Errorf("expected 1 conflict, got %d", len(plan.Conflicts))
	}
	if plan.Conflicts[0].Path != "Makefile" {
		t.Errorf("expected conflict path %q, got %q", "Makefile", plan.Conflicts[0].Path)
	}

	plan.AddConflict(conflict2)
	if len(plan.Conflicts) != 2 {
		t.Errorf("expected 2 conflicts, got %d", len(plan.Conflicts))
	}
	if plan.Conflicts[1].Path != "scripts" {
		t.Errorf("expected conflict path %q, got %q", "scripts", plan.Conflicts[1].Path)
	}
}

func TestOperationConstants(t *testing.T) {
	if OpCreateSymlink != "create_symlink" {
		t.Errorf("OpCreateSymlink = %q, want %q", OpCreateSymlink, "create_symlink")
	}
	if OpCopy != "copy" {
		t.Errorf("OpCopy = %q, want %q", OpCopy, "copy")
	}
	if OpRemove != "remove" {
		t.Errorf("OpRemove = %q, want %q", OpRemove, "remove")
	}
}

func TestApplyPlan_OperationsOrder(t *testing.T) {
	// Test that operations are added in order
	plan := NewApplyPlan([]string{"store1"})

	ops := []Operation{
		{Type: OpRemove, RelPath: "path1", Store: "store1"},
		{Type: OpCreateSymlink, RelPath: "path1", Store: "store1"},
		{Type: OpRemove, RelPath: "path2", Store: "store1"},
		{Type: OpCopy, RelPath: "path2", Store: "store1"},
	}

	for _, op := range ops {
		plan.AddOperation(op)
	}

	if len(plan.Operations) != len(ops) {
		t.Fatalf("expected %d operations, got %d", len(ops), len(plan.Operations))
	}

	for i, expected := range ops {
		if plan.Operations[i].Type != expected.Type {
			t.Errorf("operation %d: expected type %q, got %q", i, expected.Type, plan.Operations[i].Type)
		}
		if plan.Operations[i].RelPath != expected.RelPath {
			t.Errorf("operation %d: expected relPath %q, got %q", i, expected.RelPath, plan.Operations[i].RelPath)
		}
	}
}
