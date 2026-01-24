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

// TrackFile contains the list of paths tracked by a store.
type TrackFile struct {
	// Paths is the list of tracked file/directory paths (relative to overlay root)
	Paths []string `json:"paths"`

	// Ignores is a list of glob patterns to ignore (optional, for future use)
	Ignores []string `json:"ignores,omitempty"`
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
		Paths:   []string{},
		Ignores: []string{},
	}
}
