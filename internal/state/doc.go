// Package state manages workspace and store state persistence.
//
// The state package provides abstractions for storing and retrieving workspace
// metadata, including tracked paths, applied stores, and ownership information.
// State is persisted as JSON files in the .monodev/workspaces directory.
//
// Key concepts:
//   - WorkspaceState: Tracks which paths are managed and by which stores
//   - WorkspaceID: Unique identifier derived from repo fingerprint and path
//   - PathOwnership: Tracks which store owns each managed path
//   - StateStore: Interface for persisting and loading workspace state
package state
