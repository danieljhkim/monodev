package stores

import (
	"fmt"
	"time"
)

const (
	// ScopeGlobal indicates a store stored at ~/.monodev/stores/
	ScopeGlobal = "global"

	// ScopeComponent indicates a store stored at repo_root/.monodev/stores/
	ScopeComponent = "component"

	// TrackedPath Role values
	RoleScript = "script"
	RoleDocs   = "docs"
	RoleStyle  = "style"
	RoleConfig = "config"
	RoleOther  = "other"

	// TrackedPath Origin values
	OriginUser  = "user"
	OriginAgent = "agent"
	OriginOther = "other"
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
	// Valid values: "global", "component"
	Scope string `json:"scope"`

	// Description provides additional context about the store
	Description string `json:"description,omitempty"`

	// CreatedAt is when the store was created
	CreatedAt time.Time `json:"createdAt"`

	// UpdatedAt is when the store was last modified
	UpdatedAt time.Time `json:"updatedAt"`

	// SchemaVersion is the version of the store metadata schema
	SchemaVersion int `json:"schemaVersion,omitempty"`

	// Owner identifies who owns the store
	Owner string `json:"owner,omitempty"`

	// TaskID links the store to an external task
	TaskID string `json:"taskId,omitempty"`
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
	// Path is the relative path from the workspace root (the directory where tracking occurred).
	Path string `json:"path"`

	// Kind is the type of path ("file" or "dir")
	Kind string `json:"kind"`

	// Required indicates if this path must exist when applying (default: true)
	Required *bool `json:"required,omitempty"`

	// Deprecated: Location was the absolute path where tracking occurred.
	// As of schema version 2, paths are repo-root-relative and Location is unused.
	Location string `json:"location,omitempty"`

	// Role categorizes the tracked path (script, docs, style, config, other)
	Role string `json:"role,omitempty"`

	// Description provides additional context about the tracked path
	Description string `json:"description,omitempty"`

	// CreatedAt is when the path was first tracked (pointer for proper omitempty)
	CreatedAt *time.Time `json:"createdAt,omitempty"`

	// UpdatedAt is when the path tracking was last modified (pointer for proper omitempty)
	UpdatedAt *time.Time `json:"updatedAt,omitempty"`

	// Origin indicates how the path was tracked (user, agent, other)
	Origin string `json:"origin,omitempty"`
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
		Name:          name,
		Scope:         scope,
		CreatedAt:     createdAt,
		UpdatedAt:     createdAt,
		SchemaVersion: 2,
	}
}

// Validate checks that all fields contain valid values.
func (m *StoreMeta) Validate() error {
	return nil
}

// validRoles is the set of valid Role values for TrackedPath.
var validRoles = map[string]bool{
	RoleScript: true, RoleDocs: true, RoleStyle: true, RoleConfig: true, RoleOther: true,
}

// validOrigins is the set of valid Origin values for TrackedPath.
var validOrigins = map[string]bool{
	OriginUser: true, OriginAgent: true, OriginOther: true,
}

// ValidateRole checks that a role value is valid (if non-empty).
func ValidateRole(role string) error {
	if role != "" && !validRoles[role] {
		return fmt.Errorf("invalid role %q: must be one of script, docs, style, config, other", role)
	}
	return nil
}

// ValidateOrigin checks that an origin value is valid (if non-empty).
func ValidateOrigin(origin string) error {
	if origin != "" && !validOrigins[origin] {
		return fmt.Errorf("invalid origin %q: must be one of user, agent, other", origin)
	}
	return nil
}

// NewTrackFile creates a new empty TrackFile.
func NewTrackFile() *TrackFile {
	return &TrackFile{
		SchemaVersion: 2,
		Tracked:       []TrackedPath{},
		Ignore:        []string{},
	}
}
