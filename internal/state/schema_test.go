package state

import (
	"encoding/json"
	"testing"
	"time"
)

func TestNewWorkspaceState(t *testing.T) {
	ws := NewWorkspaceState("repo1", "workspace/path", "symlink")

	if ws.Repo != "repo1" {
		t.Errorf("expected Repo='repo1', got %q", ws.Repo)
	}
	if ws.WorkspacePath != "workspace/path" {
		t.Errorf("expected WorkspacePath='workspace/path', got %q", ws.WorkspacePath)
	}
	if ws.Applied != false {
		t.Errorf("expected Applied=false, got %v", ws.Applied)
	}
	if ws.Mode != "symlink" {
		t.Errorf("expected Mode='symlink', got %q", ws.Mode)
	}
	if ws.Stack == nil {
		t.Error("expected Stack to be initialized")
	}
	if len(ws.Stack) != 0 {
		t.Errorf("expected empty Stack, got %d items", len(ws.Stack))
	}
	if ws.ActiveStore != "" {
		t.Errorf("expected empty ActiveStore, got %q", ws.ActiveStore)
	}
	if ws.Paths == nil {
		t.Error("expected Paths to be initialized")
	}
	if len(ws.Paths) != 0 {
		t.Errorf("expected empty Paths, got %d items", len(ws.Paths))
	}
}

func TestNewRepoState(t *testing.T) {
	rs := NewRepoState("fingerprint123")

	if rs.Fingerprint != "fingerprint123" {
		t.Errorf("expected Fingerprint='fingerprint123', got %q", rs.Fingerprint)
	}
	if rs.Stack == nil {
		t.Error("expected Stack to be initialized")
	}
	if len(rs.Stack) != 0 {
		t.Errorf("expected empty Stack, got %d items", len(rs.Stack))
	}
	if rs.ActiveStore != "" {
		t.Errorf("expected empty ActiveStore, got %q", rs.ActiveStore)
	}
}

func TestWorkspaceState_Serialization(t *testing.T) {
	tests := []struct {
		name  string
		state *WorkspaceState
	}{
		{
			name:  "empty state",
			state: NewWorkspaceState("repo1", "workspace", "symlink"),
		},
		{
			name: "state with stack",
			state: &WorkspaceState{
				Repo:          "repo1",
				WorkspacePath: "workspace",
				Applied:       true,
				Mode:          "symlink",
				Stack:         []string{"store1", "store2"},
				ActiveStore:   "store3",
				Paths:         make(map[string]PathOwnership),
			},
		},
		{
			name: "state with paths",
			state: &WorkspaceState{
				Repo:          "repo1",
				WorkspacePath: "workspace",
				Applied:       true,
				Mode:          "copy",
				Stack:         []string{},
				ActiveStore:   "store1",
				Paths: map[string]PathOwnership{
					"Makefile": {
						Store:     "store1",
						Type:      "copy",
						Timestamp: time.Now(),
						Checksum:  "abc123",
					},
					"scripts": {
						Store:     "store1",
						Type:      "copy",
						Timestamp: time.Now(),
					},
				},
			},
		},
		{
			name: "state with all fields",
			state: &WorkspaceState{
				Repo:          "repo1",
				WorkspacePath: "workspace/path",
				Applied:       true,
				Mode:          "symlink",
				Stack:         []string{"global", "profile"},
				ActiveStore:   "component",
				Paths: map[string]PathOwnership{
					"Makefile": {
						Store:     "global",
						Type:      "symlink",
						Timestamp: time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC),
					},
					"config.json": {
						Store:     "component",
						Type:      "copy",
						Timestamp: time.Date(2024, 1, 2, 12, 0, 0, 0, time.UTC),
						Checksum:  "def456",
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Marshal to JSON
			data, err := json.Marshal(tt.state)
			if err != nil {
				t.Fatalf("Marshal() error = %v", err)
			}

			// Unmarshal back
			var unmarshaled WorkspaceState
			if err := json.Unmarshal(data, &unmarshaled); err != nil {
				t.Fatalf("Unmarshal() error = %v", err)
			}

			// Verify all fields match
			if unmarshaled.Repo != tt.state.Repo {
				t.Errorf("Repo: got %q, want %q", unmarshaled.Repo, tt.state.Repo)
			}
			if unmarshaled.WorkspacePath != tt.state.WorkspacePath {
				t.Errorf("WorkspacePath: got %q, want %q", unmarshaled.WorkspacePath, tt.state.WorkspacePath)
			}
			if unmarshaled.Applied != tt.state.Applied {
				t.Errorf("Applied: got %v, want %v", unmarshaled.Applied, tt.state.Applied)
			}
			if unmarshaled.Mode != tt.state.Mode {
				t.Errorf("Mode: got %q, want %q", unmarshaled.Mode, tt.state.Mode)
			}
			if len(unmarshaled.Stack) != len(tt.state.Stack) {
				t.Errorf("Stack length: got %d, want %d", len(unmarshaled.Stack), len(tt.state.Stack))
			}
			for i, store := range tt.state.Stack {
				if i < len(unmarshaled.Stack) && unmarshaled.Stack[i] != store {
					t.Errorf("Stack[%d]: got %q, want %q", i, unmarshaled.Stack[i], store)
				}
			}
			if unmarshaled.ActiveStore != tt.state.ActiveStore {
				t.Errorf("ActiveStore: got %q, want %q", unmarshaled.ActiveStore, tt.state.ActiveStore)
			}
			if len(unmarshaled.Paths) != len(tt.state.Paths) {
				t.Errorf("Paths length: got %d, want %d", len(unmarshaled.Paths), len(tt.state.Paths))
			}
			for path, ownership := range tt.state.Paths {
				if unmarshaledOwnership, ok := unmarshaled.Paths[path]; ok {
					if unmarshaledOwnership.Store != ownership.Store {
						t.Errorf("Paths[%q].Store: got %q, want %q", path, unmarshaledOwnership.Store, ownership.Store)
					}
					if unmarshaledOwnership.Type != ownership.Type {
						t.Errorf("Paths[%q].Type: got %q, want %q", path, unmarshaledOwnership.Type, ownership.Type)
					}
					if unmarshaledOwnership.Checksum != ownership.Checksum {
						t.Errorf("Paths[%q].Checksum: got %q, want %q", path, unmarshaledOwnership.Checksum, ownership.Checksum)
					}
				} else {
					t.Errorf("Paths[%q] missing in unmarshaled state", path)
				}
			}
		})
	}
}

func TestRepoState_Serialization(t *testing.T) {
	tests := []struct {
		name  string
		state *RepoState
	}{
		{
			name:  "empty state",
			state: NewRepoState("fingerprint1"),
		},
		{
			name: "state with stack and active store",
			state: &RepoState{
				Fingerprint: "fingerprint1",
				Stack:       []string{"store1", "store2"},
				ActiveStore: "store3",
			},
		},
		{
			name: "state with empty stack",
			state: &RepoState{
				Fingerprint: "fingerprint1",
				Stack:       []string{},
				ActiveStore: "store1",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Marshal to JSON
			data, err := json.Marshal(tt.state)
			if err != nil {
				t.Fatalf("Marshal() error = %v", err)
			}

			// Unmarshal back
			var unmarshaled RepoState
			if err := json.Unmarshal(data, &unmarshaled); err != nil {
				t.Fatalf("Unmarshal() error = %v", err)
			}

			// Verify all fields match
			if unmarshaled.Fingerprint != tt.state.Fingerprint {
				t.Errorf("Fingerprint: got %q, want %q", unmarshaled.Fingerprint, tt.state.Fingerprint)
			}
			if len(unmarshaled.Stack) != len(tt.state.Stack) {
				t.Errorf("Stack length: got %d, want %d", len(unmarshaled.Stack), len(tt.state.Stack))
			}
			for i, store := range tt.state.Stack {
				if i < len(unmarshaled.Stack) && unmarshaled.Stack[i] != store {
					t.Errorf("Stack[%d]: got %q, want %q", i, unmarshaled.Stack[i], store)
				}
			}
			if unmarshaled.ActiveStore != tt.state.ActiveStore {
				t.Errorf("ActiveStore: got %q, want %q", unmarshaled.ActiveStore, tt.state.ActiveStore)
			}
		})
	}
}

func TestPathOwnership_Serialization(t *testing.T) {
	tests := []struct {
		name      string
		ownership PathOwnership
	}{
		{
			name: "with checksum",
			ownership: PathOwnership{
				Store:     "store1",
				Type:      "copy",
				Timestamp: time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC),
				Checksum:  "abc123",
			},
		},
		{
			name: "without checksum",
			ownership: PathOwnership{
				Store:     "store1",
				Type:      "symlink",
				Timestamp: time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC),
			},
		},
		{
			name: "empty checksum",
			ownership: PathOwnership{
				Store:     "store1",
				Type:      "copy",
				Timestamp: time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC),
				Checksum:  "",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Marshal to JSON
			data, err := json.Marshal(tt.ownership)
			if err != nil {
				t.Fatalf("Marshal() error = %v", err)
			}

			// Unmarshal back
			var unmarshaled PathOwnership
			if err := json.Unmarshal(data, &unmarshaled); err != nil {
				t.Fatalf("Unmarshal() error = %v", err)
			}

			// Verify all fields match
			if unmarshaled.Store != tt.ownership.Store {
				t.Errorf("Store: got %q, want %q", unmarshaled.Store, tt.ownership.Store)
			}
			if unmarshaled.Type != tt.ownership.Type {
				t.Errorf("Type: got %q, want %q", unmarshaled.Type, tt.ownership.Type)
			}
			if !unmarshaled.Timestamp.Equal(tt.ownership.Timestamp) {
				t.Errorf("Timestamp: got %v, want %v", unmarshaled.Timestamp, tt.ownership.Timestamp)
			}
			if unmarshaled.Checksum != tt.ownership.Checksum {
				t.Errorf("Checksum: got %q, want %q", unmarshaled.Checksum, tt.ownership.Checksum)
			}
		})
	}
}

func TestWorkspaceState_SchemaValidation(t *testing.T) {
	tests := []struct {
		name    string
		json    string
		wantErr bool
	}{
		{
			name:    "valid minimal state",
			json:    `{"repo":"r1","workspacePath":"wp","applied":false,"mode":"symlink","stack":[],"activeStore":"","paths":{}}`,
			wantErr: false,
		},
		{
			name:    "valid full state",
			json:    `{"repo":"r1","workspacePath":"wp","applied":true,"mode":"copy","stack":["s1"],"activeStore":"s2","paths":{"p1":{"store":"s1","type":"copy","timestamp":"2024-01-01T12:00:00Z"}}}`,
			wantErr: false,
		},
		{
			name:    "invalid JSON",
			json:    `{"repo":"r1",invalid}`,
			wantErr: true,
		},
		{
			name:    "missing required field (repo)",
			json:    `{"workspacePath":"wp","applied":false,"mode":"symlink","stack":[],"activeStore":"","paths":{}}`,
			wantErr: false, // JSON unmarshal doesn't require all fields
		},
		{
			name:    "empty JSON object",
			json:    `{}`,
			wantErr: false, // Empty object is valid, fields will be zero values
		},
		{
			name:    "null values",
			json:    `{"repo":null,"workspacePath":null}`,
			wantErr: false, // null values are allowed, will be empty strings
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var state WorkspaceState
			err := json.Unmarshal([]byte(tt.json), &state)

			if tt.wantErr && err == nil {
				t.Error("expected error but got none")
			}
			if !tt.wantErr && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

func TestWorkspaceID_Stability(t *testing.T) {
	// Test that the same inputs always produce the same workspace ID
	repoFingerprint := "abc123def456"
	workspacePath := "services/search/indexer"

	// Compute ID multiple times
	id1 := ComputeWorkspaceID(repoFingerprint, workspacePath)
	id2 := ComputeWorkspaceID(repoFingerprint, workspacePath)
	id3 := ComputeWorkspaceID(repoFingerprint, workspacePath)

	if id1 != id2 {
		t.Errorf("ID not stable: first call = %q, second call = %q", id1, id2)
	}
	if id2 != id3 {
		t.Errorf("ID not stable: second call = %q, third call = %q", id2, id3)
	}
	if id1 != id3 {
		t.Errorf("ID not stable: first call = %q, third call = %q", id1, id3)
	}
}

func TestWorkspaceState_EmptyFields(t *testing.T) {
	// Test that empty fields are handled correctly
	ws := &WorkspaceState{}

	data, err := json.Marshal(ws)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}

	var unmarshaled WorkspaceState
	if err := json.Unmarshal(data, &unmarshaled); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}

	// Verify zero values
	if unmarshaled.Repo != "" {
		t.Errorf("expected empty Repo, got %q", unmarshaled.Repo)
	}
	if unmarshaled.WorkspacePath != "" {
		t.Errorf("expected empty WorkspacePath, got %q", unmarshaled.WorkspacePath)
	}
	if unmarshaled.Applied != false {
		t.Errorf("expected Applied=false, got %v", unmarshaled.Applied)
	}
	if unmarshaled.Mode != "" {
		t.Errorf("expected empty Mode, got %q", unmarshaled.Mode)
	}
}

func TestPathOwnership_ChecksumOmitEmpty(t *testing.T) {
	// Test that empty checksum is omitted from JSON
	ownership := PathOwnership{
		Store:     "store1",
		Type:      "symlink",
		Timestamp: time.Now(),
		Checksum:  "", // Empty checksum
	}

	data, err := json.Marshal(ownership)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}

	// Verify checksum field is not present when empty (due to omitempty tag)
	jsonStr := string(data)
	if contains(jsonStr, `"checksum":""`) {
		t.Error("expected empty checksum to be omitted from JSON")
	}

	// Unmarshal should still work
	var unmarshaled PathOwnership
	if err := json.Unmarshal(data, &unmarshaled); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
}

// Helper function
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr ||
		(len(s) > len(substr) && containsMiddle(s, substr)))
}

func containsMiddle(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
