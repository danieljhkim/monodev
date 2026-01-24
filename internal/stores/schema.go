package stores

import "time"

// StoreMeta contains metadata about a store.
type StoreMeta struct {
	// Name is the human-readable name of the store
	Name string `json:"name"`

	// Scope indicates the intended use of the store
	// Valid values: "global", "profile", "component"
	Scope string `json:"scope"`

	// Description provides additional context about the store
	Description string `json:"description,omitempty"`

	// CreatedAt is when the store was created
	CreatedAt time.Time `json:"createdAt"`

	// UpdatedAt is when the store was last modified
	UpdatedAt time.Time `json:"updatedAt"`
}

// TrackFile represents the track.json file in a store.
type TrackFile struct {
	// SchemaVersion is the version of this schema
	SchemaVersion int `json:"schemaVersion"`

	// Tracked is the list of tracked paths with metadata
	Tracked []TrackedPath `json:"tracked"`

	// Ignore is the list of ignore patterns
	Ignore []string `json:"ignore,omitempty"`

	// Notes is an optional description of this store's purpose
	Notes string `json:"notes,omitempty"`
}

// TrackedPath represents a tracked file or directory.
type TrackedPath struct {
	// Path is the relative path from workspace root
	Path string `json:"path"`

	// Kind is the type of path ("file" or "dir")
	Kind string `json:"kind"`

	// Required indicates if this path must exist when applying (default: true)
	Required *bool `json:"required,omitempty"`
}

// IsRequired returns whether this path is required.
func (t TrackedPath) IsRequired() bool {
	if t.Required == nil {
		return true // default to required
	}
	return *t.Required
}

// Paths returns a list of all tracked path strings (for backward compatibility).
func (tf *TrackFile) Paths() []string {
	paths := make([]string, len(tf.Tracked))
	for i, t := range tf.Tracked {
		paths[i] = t.Path
	}
	return paths
}

// NewStoreMeta creates a new StoreMeta with the given name and scope.
func NewStoreMeta(name, scope string, createdAt time.Time) *StoreMeta {
	return &StoreMeta{
		Name:      name,
		Scope:     scope,
		CreatedAt: createdAt,
		UpdatedAt: createdAt,
	}
}

// NewTrackFile creates a new empty TrackFile.
func NewTrackFile() *TrackFile {
	return &TrackFile{
		SchemaVersion: 1,
		Tracked:       []TrackedPath{},
		Ignore:        []string{},
	}
}
