package stores

import (
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

		if tf.SchemaVersion != 1 {
			t.Errorf("SchemaVersion = %d, want 1", tf.SchemaVersion)
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
