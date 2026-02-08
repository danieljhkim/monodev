package stores

import "time"

const (
	// ScopeGlobal indicates a store stored at ~/.monodev/stores/
	ScopeGlobal = "global"

	// ScopeComponent indicates a store stored at repo_root/.monodev/stores/
	ScopeComponent = "component"
)

// ScopedStore wraps a store with its scope location.
type ScopedStore struct {
	// ID is the store identifier
	ID string

	// Meta is the store metadata
	Meta *StoreMeta

	// Scope indicates where the store is located (ScopeGlobal or ScopeComponent)
	Scope string
}

// StoreLocation records where a store was found during scope search.
type StoreLocation struct {
	// Scope is the scope where the store was found
	Scope string

	// Repo is the StoreRepo instance for this scope
	Repo StoreRepo
}

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

	// Location is the absolute path where tracking occurred (optional)
	Location string `json:"location,omitempty"`
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
