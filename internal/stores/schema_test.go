package stores

import (
	"encoding/json"
	"testing"
	"time"
)

func TestTrackedPath_IsRequired(t *testing.T) {
	t.Run("returns true when Required is nil", func(t *testing.T) {
		tp := TrackedPath{
			Path:     "test.txt",
			Kind:     "file",
			Required: nil,
		}

		if !tp.IsRequired() {
			t.Error("Expected IsRequired() = true when Required is nil")
		}
	})

	t.Run("returns true when Required is true", func(t *testing.T) {
		required := true
		tp := TrackedPath{
			Path:     "test.txt",
			Kind:     "file",
			Required: &required,
		}

		if !tp.IsRequired() {
			t.Error("Expected IsRequired() = true when Required is true")
		}
	})

	t.Run("returns false when Required is false", func(t *testing.T) {
		required := false
		tp := TrackedPath{
			Path:     "test.txt",
			Kind:     "file",
			Required: &required,
		}

		if tp.IsRequired() {
			t.Error("Expected IsRequired() = false when Required is false")
		}
	})
}

func TestTrackFile_Paths(t *testing.T) {
	t.Run("returns empty slice for empty track file", func(t *testing.T) {
		tf := NewTrackFile()
		paths := tf.Paths()

		if len(paths) != 0 {
			t.Errorf("Expected empty paths slice, got %d elements", len(paths))
		}
	})

	t.Run("returns all tracked paths", func(t *testing.T) {
		tf := NewTrackFile()
		tf.Tracked = []TrackedPath{
			{Path: "path1", Kind: "file"},
			{Path: "path2", Kind: "dir"},
			{Path: "path3", Kind: "file"},
		}

		paths := tf.Paths()

		if len(paths) != 3 {
			t.Fatalf("Expected 3 paths, got %d", len(paths))
		}

		expectedPaths := []string{"path1", "path2", "path3"}
		for i, expected := range expectedPaths {
			if paths[i] != expected {
				t.Errorf("Path[%d] = %s, want %s", i, paths[i], expected)
			}
		}
	})

	t.Run("preserves order of tracked paths", func(t *testing.T) {
		tf := NewTrackFile()
		tf.Tracked = []TrackedPath{
			{Path: "z.txt", Kind: "file"},
			{Path: "a.txt", Kind: "file"},
			{Path: "m.txt", Kind: "file"},
		}

		paths := tf.Paths()

		// Order should match Tracked array, not alphabetical
		if paths[0] != "z.txt" || paths[1] != "a.txt" || paths[2] != "m.txt" {
			t.Error("Paths() did not preserve order")
		}
	})
}

func TestNewStoreMeta(t *testing.T) {
	t.Run("creates store meta with correct values", func(t *testing.T) {
		name := "Test Store"
		scope := "component"
		now := time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC)

		meta := NewStoreMeta(name, scope, now)

		if meta.Name != name {
			t.Errorf("Name = %s, want %s", meta.Name, name)
		}

		if meta.Scope != scope {
			t.Errorf("Scope = %s, want %s", meta.Scope, scope)
		}

		if !meta.CreatedAt.Equal(now) {
			t.Errorf("CreatedAt = %v, want %v", meta.CreatedAt, now)
		}

		if !meta.UpdatedAt.Equal(now) {
			t.Errorf("UpdatedAt = %v, want %v", meta.UpdatedAt, now)
		}
	})

	t.Run("sets UpdatedAt equal to CreatedAt initially", func(t *testing.T) {
		now := time.Now()
		meta := NewStoreMeta("Test", "global", now)

		if !meta.CreatedAt.Equal(meta.UpdatedAt) {
			t.Error("Expected CreatedAt and UpdatedAt to be equal initially")
		}
	})

	t.Run("creates meta with empty description", func(t *testing.T) {
		meta := NewStoreMeta("Test", "global", time.Now())

		if meta.Description != "" {
			t.Errorf("Expected empty Description, got %q", meta.Description)
		}
	})

}

func TestNewTrackFile(t *testing.T) {
	t.Run("creates track file with correct defaults", func(t *testing.T) {
		tf := NewTrackFile()

		if tf.SchemaVersion != 2 {
			t.Errorf("SchemaVersion = %d, want 2", tf.SchemaVersion)
		}

		if tf.Tracked == nil {
			t.Error("Tracked should not be nil")
		}

		if len(tf.Tracked) != 0 {
			t.Errorf("Tracked should be empty, got %d elements", len(tf.Tracked))
		}

		if tf.Ignore == nil {
			t.Error("Ignore should not be nil")
		}

		if len(tf.Ignore) != 0 {
			t.Errorf("Ignore should be empty, got %d elements", len(tf.Ignore))
		}
	})

	t.Run("tracked and ignore are not nil slices", func(t *testing.T) {
		tf := NewTrackFile()

		// These should be empty slices, not nil, for proper JSON marshaling
		if tf.Tracked == nil {
			t.Error("Tracked should be empty slice, not nil")
		}

		if tf.Ignore == nil {
			t.Error("Ignore should be empty slice, not nil")
		}
	})
}

func TestStoreMeta_Fields(t *testing.T) {
	t.Run("all fields can be set and retrieved", func(t *testing.T) {
		createdAt := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
		updatedAt := time.Date(2024, 2, 1, 0, 0, 0, 0, time.UTC)

		meta := &StoreMeta{
			Name:        "My Store",
			Scope:       "profile",
			Description: "Test description",
			CreatedAt:   createdAt,
			UpdatedAt:   updatedAt,
		}

		if meta.Name != "My Store" {
			t.Errorf("Name = %s, want 'My Store'", meta.Name)
		}

		if meta.Scope != "profile" {
			t.Errorf("Scope = %s, want 'profile'", meta.Scope)
		}

		if meta.Description != "Test description" {
			t.Errorf("Description = %s, want 'Test description'", meta.Description)
		}

		if !meta.CreatedAt.Equal(createdAt) {
			t.Errorf("CreatedAt = %v, want %v", meta.CreatedAt, createdAt)
		}

		if !meta.UpdatedAt.Equal(updatedAt) {
			t.Errorf("UpdatedAt = %v, want %v", meta.UpdatedAt, updatedAt)
		}
	})
}

func TestTrackFile_Fields(t *testing.T) {
	t.Run("all fields can be set and retrieved", func(t *testing.T) {
		required := false
		tf := &TrackFile{
			SchemaVersion: 2,
			Tracked: []TrackedPath{
				{Path: "file1.txt", Kind: "file"},
				{Path: "dir/", Kind: "dir", Required: &required},
			},
			Ignore: []string{"*.log", "tmp/"},
			Notes:  "Test notes",
		}

		if tf.SchemaVersion != 2 {
			t.Errorf("SchemaVersion = %d, want 2", tf.SchemaVersion)
		}

		if len(tf.Tracked) != 2 {
			t.Errorf("len(Tracked) = %d, want 2", len(tf.Tracked))
		}

		if tf.Tracked[0].Path != "file1.txt" {
			t.Errorf("Tracked[0].Path = %s, want 'file1.txt'", tf.Tracked[0].Path)
		}

		if tf.Tracked[1].IsRequired() {
			t.Error("Tracked[1] should not be required")
		}

		if len(tf.Ignore) != 2 {
			t.Errorf("len(Ignore) = %d, want 2", len(tf.Ignore))
		}

		if tf.Notes != "Test notes" {
			t.Errorf("Notes = %s, want 'Test notes'", tf.Notes)
		}
	})
}

func TestTrackedPath_Location(t *testing.T) {
	t.Run("location field can be set and retrieved", func(t *testing.T) {
		tp := TrackedPath{
			Path:     "test.txt",
			Kind:     "file",
			Location: "/home/user/workspace",
		}

		if tp.Location != "/home/user/workspace" {
			t.Errorf("Location = %s, want '/home/user/workspace'", tp.Location)
		}
	})

	t.Run("location field defaults to empty string", func(t *testing.T) {
		tp := TrackedPath{
			Path: "test.txt",
			Kind: "file",
		}

		if tp.Location != "" {
			t.Errorf("Location = %s, want empty string", tp.Location)
		}
	})
}

func TestTrackedPath_JSONSerialization(t *testing.T) {
	t.Run("marshals location field when set", func(t *testing.T) {
		tp := TrackedPath{
			Path:     "test.txt",
			Kind:     "file",
			Location: "/home/user/workspace",
		}

		data, err := json.Marshal(tp)
		if err != nil {
			t.Fatalf("Failed to marshal: %v", err)
		}

		var result TrackedPath
		if err := json.Unmarshal(data, &result); err != nil {
			t.Fatalf("Failed to unmarshal: %v", err)
		}

		if result.Location != "/home/user/workspace" {
			t.Errorf("Location = %s, want '/home/user/workspace'", result.Location)
		}
	})

	t.Run("omits location field when empty (omitempty)", func(t *testing.T) {
		tp := TrackedPath{
			Path:     "test.txt",
			Kind:     "file",
			Location: "",
		}

		data, err := json.Marshal(tp)
		if err != nil {
			t.Fatalf("Failed to marshal: %v", err)
		}

		// Should not contain "location" key
		jsonStr := string(data)
		if contains(jsonStr, "location") {
			t.Errorf("JSON should not contain 'location' key when empty: %s", jsonStr)
		}
	})

	t.Run("unmarshals old JSON without location field (backward compatibility)", func(t *testing.T) {
		// Simulate old track.json format without location field
		oldJSON := `{"path":"test.txt","kind":"file"}`

		var tp TrackedPath
		if err := json.Unmarshal([]byte(oldJSON), &tp); err != nil {
			t.Fatalf("Failed to unmarshal old JSON: %v", err)
		}

		if tp.Path != "test.txt" {
			t.Errorf("Path = %s, want 'test.txt'", tp.Path)
		}

		if tp.Kind != "file" {
			t.Errorf("Kind = %s, want 'file'", tp.Kind)
		}

		if tp.Location != "" {
			t.Errorf("Location should be empty string, got %s", tp.Location)
		}
	})

	t.Run("round-trip marshaling preserves all fields", func(t *testing.T) {
		required := true
		original := TrackedPath{
			Path:     "config.yaml",
			Kind:     "file",
			Required: &required,
			Location: "/Users/test/myproject",
		}

		data, err := json.Marshal(original)
		if err != nil {
			t.Fatalf("Failed to marshal: %v", err)
		}

		var result TrackedPath
		if err := json.Unmarshal(data, &result); err != nil {
			t.Fatalf("Failed to unmarshal: %v", err)
		}

		if result.Path != original.Path {
			t.Errorf("Path = %s, want %s", result.Path, original.Path)
		}

		if result.Kind != original.Kind {
			t.Errorf("Kind = %s, want %s", result.Kind, original.Kind)
		}

		if result.Location != original.Location {
			t.Errorf("Location = %s, want %s", result.Location, original.Location)
		}

		if result.Required == nil || *result.Required != *original.Required {
			t.Errorf("Required mismatch")
		}
	})
}

func TestNewStoreMeta_SchemaVersion(t *testing.T) {
	meta := NewStoreMeta("test", "global", time.Now())
	if meta.SchemaVersion != 2 {
		t.Errorf("SchemaVersion = %d, want 2", meta.SchemaVersion)
	}
}

func TestStoreMeta_Validate(t *testing.T) {
	t.Run("always passes", func(t *testing.T) {
		meta := NewStoreMeta("test", "global", time.Now())
		if err := meta.Validate(); err != nil {
			t.Errorf("unexpected validation error: %v", err)
		}
	})
}

func TestStoreMeta_BackwardCompat(t *testing.T) {
	t.Run("unmarshal old JSON without new fields", func(t *testing.T) {
		oldJSON := `{"name":"my-store","scope":"global","createdAt":"2024-01-15T10:30:00Z","updatedAt":"2024-01-15T10:30:00Z"}`

		var meta StoreMeta
		if err := json.Unmarshal([]byte(oldJSON), &meta); err != nil {
			t.Fatalf("Failed to unmarshal old JSON: %v", err)
		}

		if meta.Name != "my-store" {
			t.Errorf("Name = %s, want 'my-store'", meta.Name)
		}
		if meta.SchemaVersion != 0 {
			t.Errorf("SchemaVersion = %d, want 0 (zero value)", meta.SchemaVersion)
		}
		if meta.Owner != "" {
			t.Errorf("Owner = %s, want empty", meta.Owner)
		}
		if meta.TaskID != "" {
			t.Errorf("TaskID = %s, want empty", meta.TaskID)
		}
	})

	t.Run("round-trip with remaining fields", func(t *testing.T) {
		meta := NewStoreMeta("test", "global", time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC))
		meta.Description = "a test store"
		meta.Owner = "alice"
		meta.TaskID = "TASK-123"

		data, err := json.Marshal(meta)
		if err != nil {
			t.Fatalf("Failed to marshal: %v", err)
		}

		var result StoreMeta
		if err := json.Unmarshal(data, &result); err != nil {
			t.Fatalf("Failed to unmarshal: %v", err)
		}

		if result.SchemaVersion != 2 {
			t.Errorf("SchemaVersion = %d, want 2", result.SchemaVersion)
		}
		if result.Owner != "alice" {
			t.Errorf("Owner = %s, want 'alice'", result.Owner)
		}
		if result.TaskID != "TASK-123" {
			t.Errorf("TaskID = %s, want 'TASK-123'", result.TaskID)
		}
		if result.Description != "a test store" {
			t.Errorf("Description = %s, want 'a test store'", result.Description)
		}
	})
}

func TestTrackedPath_NewFields(t *testing.T) {
	t.Run("backward compat: old JSON without new fields", func(t *testing.T) {
		oldJSON := `{"path":"test.txt","kind":"file"}`

		var tp TrackedPath
		if err := json.Unmarshal([]byte(oldJSON), &tp); err != nil {
			t.Fatalf("Failed to unmarshal: %v", err)
		}

		if tp.Role != "" {
			t.Errorf("Role = %s, want empty", tp.Role)
		}
		if tp.Description != "" {
			t.Errorf("Description = %s, want empty", tp.Description)
		}
		if tp.CreatedAt != nil {
			t.Errorf("CreatedAt = %v, want nil", tp.CreatedAt)
		}
		if tp.UpdatedAt != nil {
			t.Errorf("UpdatedAt = %v, want nil", tp.UpdatedAt)
		}
		if tp.Origin != "" {
			t.Errorf("Origin = %s, want empty", tp.Origin)
		}
	})

	t.Run("nil time pointer omitted from JSON", func(t *testing.T) {
		tp := TrackedPath{Path: "test.txt", Kind: "file"}
		data, err := json.Marshal(tp)
		if err != nil {
			t.Fatalf("Failed to marshal: %v", err)
		}
		jsonStr := string(data)
		if contains(jsonStr, "createdAt") {
			t.Errorf("JSON should not contain 'createdAt' when nil: %s", jsonStr)
		}
		if contains(jsonStr, "updatedAt") {
			t.Errorf("JSON should not contain 'updatedAt' when nil: %s", jsonStr)
		}
	})

	t.Run("non-nil time pointer round-trips", func(t *testing.T) {
		now := time.Date(2024, 6, 15, 12, 0, 0, 0, time.UTC)
		tp := TrackedPath{
			Path:        "config.yaml",
			Kind:        "file",
			Role:        RoleConfig,
			Description: "app config",
			CreatedAt:   &now,
			UpdatedAt:   &now,
			Origin:      OriginUser,
		}

		data, err := json.Marshal(tp)
		if err != nil {
			t.Fatalf("Failed to marshal: %v", err)
		}

		var result TrackedPath
		if err := json.Unmarshal(data, &result); err != nil {
			t.Fatalf("Failed to unmarshal: %v", err)
		}

		if result.Role != RoleConfig {
			t.Errorf("Role = %s, want %s", result.Role, RoleConfig)
		}
		if result.Description != "app config" {
			t.Errorf("Description = %s, want 'app config'", result.Description)
		}
		if result.CreatedAt == nil || !result.CreatedAt.Equal(now) {
			t.Errorf("CreatedAt = %v, want %v", result.CreatedAt, now)
		}
		if result.UpdatedAt == nil || !result.UpdatedAt.Equal(now) {
			t.Errorf("UpdatedAt = %v, want %v", result.UpdatedAt, now)
		}
		if result.Origin != OriginUser {
			t.Errorf("Origin = %s, want %s", result.Origin, OriginUser)
		}
	})
}

func TestValidateRole(t *testing.T) {
	t.Run("valid roles pass", func(t *testing.T) {
		for _, role := range []string{RoleScript, RoleDocs, RoleStyle, RoleConfig, RoleOther} {
			if err := ValidateRole(role); err != nil {
				t.Errorf("unexpected error for role %q: %v", role, err)
			}
		}
	})

	t.Run("empty role passes", func(t *testing.T) {
		if err := ValidateRole(""); err != nil {
			t.Errorf("unexpected error for empty role: %v", err)
		}
	})

	t.Run("invalid role rejected", func(t *testing.T) {
		if err := ValidateRole("invalid"); err == nil {
			t.Error("expected error for invalid role")
		}
	})
}

func TestValidateOrigin(t *testing.T) {
	t.Run("valid origins pass", func(t *testing.T) {
		for _, origin := range []string{OriginUser, OriginAgent, OriginOther} {
			if err := ValidateOrigin(origin); err != nil {
				t.Errorf("unexpected error for origin %q: %v", origin, err)
			}
		}
	})

	t.Run("empty origin passes", func(t *testing.T) {
		if err := ValidateOrigin(""); err != nil {
			t.Errorf("unexpected error for empty origin: %v", err)
		}
	})

	t.Run("invalid origin rejected", func(t *testing.T) {
		if err := ValidateOrigin("invalid"); err == nil {
			t.Error("expected error for invalid origin")
		}
	})
}

// Helper function to check if a string contains a substring
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > len(substr) && containsHelper(s, substr))
}

func containsHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
