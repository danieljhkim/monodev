// Package stores manages store repositories and overlay files.
//
// A store is a named collection of dev-only files (editor config, scripts, etc.)
// that can be applied to workspaces. Stores are persisted in ~/.monodev/stores/
// with each store containing metadata, tracked paths, and overlay files.
//
// Key components:
//   - StoreRepo: Interface for managing store lifecycle (create, load, delete)
//   - StoreMeta: Store metadata (description, scope, timestamps)
//   - TrackFile: List of paths tracked by the store
//   - Overlay directory: Contains the actual files managed by the store
package stores

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/danieljhkim/monodev/internal/fsops"
)

// StoreRepo provides an interface for managing stores.
type StoreRepo interface {
	// List returns all store IDs.
	List() ([]string, error)

	// Exists checks if a store with the given ID exists.
	Exists(id string) (bool, error)

	// Create creates a new store with the given ID and metadata.
	Create(id string, meta *StoreMeta) error

	// LoadMeta loads the metadata for a store.
	LoadMeta(id string) (*StoreMeta, error)

	// SaveMeta saves the metadata for a store.
	SaveMeta(id string, meta *StoreMeta) error

	// LoadTrack loads the track file for a store.
	LoadTrack(id string) (*TrackFile, error)

	// SaveTrack saves the track file for a store.
	SaveTrack(id string, track *TrackFile) error

	// OverlayRoot returns the path to the overlay directory for a store.
	OverlayRoot(id string) string

	// Delete deletes a store and all its contents.
	Delete(id string) error
}

// FileStoreRepo implements StoreRepo using files on disk.
type FileStoreRepo struct {
	fs        fsops.FS
	storesDir string
}

// NewFileStoreRepo creates a new FileStoreRepo.
func NewFileStoreRepo(fs fsops.FS, storesDir string) *FileStoreRepo {
	return &FileStoreRepo{
		fs:        fs,
		storesDir: storesDir,
	}
}

// List returns all store IDs.
func (r *FileStoreRepo) List() ([]string, error) {
	entries, err := os.ReadDir(r.storesDir)
	if err != nil {
		if os.IsNotExist(err) {
			return []string{}, nil
		}
		return nil, fmt.Errorf("failed to read stores directory: %w", err)
	}

	var stores []string
	for _, entry := range entries {
		if entry.IsDir() {
			stores = append(stores, entry.Name())
		}
	}

	return stores, nil
}

// Exists checks if a store with the given ID exists.
func (r *FileStoreRepo) Exists(id string) (bool, error) {
	// Validate store ID for safety
	if err := r.fs.ValidateIdentifier(id); err != nil {
		return false, fmt.Errorf("invalid store ID: %w", err)
	}

	storePath := filepath.Join(r.storesDir, id)
	return r.fs.Exists(storePath)
}

// Create creates a new store with the given ID and metadata.
func (r *FileStoreRepo) Create(id string, meta *StoreMeta) error {
	// Validate store ID for safety
	if err := r.fs.ValidateIdentifier(id); err != nil {
		return fmt.Errorf("invalid store ID: %w", err)
	}

	storePath := filepath.Join(r.storesDir, id)

	// Check if store already exists
	exists, err := r.Exists(id)
	if err != nil {
		return err
	}
	if exists {
		return fmt.Errorf("store already exists: %s", id)
	}

	// Create store directory
	if err := r.fs.MkdirAll(storePath, 0755); err != nil {
		return fmt.Errorf("failed to create store directory: %w", err)
	}

	// Create overlay directory
	overlayPath := filepath.Join(storePath, "overlay")
	if err := r.fs.MkdirAll(overlayPath, 0755); err != nil {
		return fmt.Errorf("failed to create overlay directory: %w", err)
	}

	// Save metadata
	if err := r.SaveMeta(id, meta); err != nil {
		return fmt.Errorf("failed to save metadata: %w", err)
	}

	// Create empty track file
	track := NewTrackFile()
	if err := r.SaveTrack(id, track); err != nil {
		return fmt.Errorf("failed to save track file: %w", err)
	}

	return nil
}

// LoadMeta loads the metadata for a store.
func (r *FileStoreRepo) LoadMeta(id string) (*StoreMeta, error) {
	// Validate store ID for safety
	if err := r.fs.ValidateIdentifier(id); err != nil {
		return nil, fmt.Errorf("invalid store ID: %w", err)
	}

	metaPath := filepath.Join(r.storesDir, id, "meta.json")

	data, err := r.fs.ReadFile(metaPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("store not found: %s", id)
		}
		return nil, fmt.Errorf("failed to read meta file: %w", err)
	}

	var meta StoreMeta
	if err := json.Unmarshal(data, &meta); err != nil {
		return nil, fmt.Errorf("failed to unmarshal meta file: %w", err)
	}

	return &meta, nil
}

// SaveMeta saves the metadata for a store.
func (r *FileStoreRepo) SaveMeta(id string, meta *StoreMeta) error {
	// Validate store ID for safety
	if err := r.fs.ValidateIdentifier(id); err != nil {
		return fmt.Errorf("invalid store ID: %w", err)
	}

	metaPath := filepath.Join(r.storesDir, id, "meta.json")

	data, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal metadata: %w", err)
	}

	if err := r.fs.AtomicWrite(metaPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write meta file: %w", err)
	}

	return nil
}

// LoadTrack loads the track file for a store.
func (r *FileStoreRepo) LoadTrack(id string) (*TrackFile, error) {
	// Validate store ID for safety
	if err := r.fs.ValidateIdentifier(id); err != nil {
		return nil, fmt.Errorf("invalid store ID: %w", err)
	}

	trackPath := filepath.Join(r.storesDir, id, "track.json")

	data, err := r.fs.ReadFile(trackPath)
	if err != nil {
		if os.IsNotExist(err) {
			// Return empty track file if it doesn't exist
			return NewTrackFile(), nil
		}
		return nil, fmt.Errorf("failed to read track file: %w", err)
	}

	var track TrackFile
	if err := json.Unmarshal(data, &track); err != nil {
		return nil, fmt.Errorf("failed to unmarshal track file: %w", err)
	}

	return &track, nil
}

// SaveTrack saves the track file for a store.
func (r *FileStoreRepo) SaveTrack(id string, track *TrackFile) error {
	// Validate store ID for safety
	if err := r.fs.ValidateIdentifier(id); err != nil {
		return fmt.Errorf("invalid store ID: %w", err)
	}

	trackPath := filepath.Join(r.storesDir, id, "track.json")

	data, err := json.MarshalIndent(track, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal track file: %w", err)
	}

	if err := r.fs.AtomicWrite(trackPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write track file: %w", err)
	}

	return nil
}

// OverlayRoot returns the path to the overlay directory for a store.
// Returns an empty string if the store ID is invalid.
// Callers should validate store IDs using Exists() before calling this method.
func (r *FileStoreRepo) OverlayRoot(id string) string {
	// Validate store ID for safety even for read-only operations
	// to prevent exposing internal paths to untrusted IDs
	if err := r.fs.ValidateIdentifier(id); err != nil {
		// For this read-only operation, return a safe invalid path
		// rather than panicking. Callers should validate IDs beforehand.
		return ""
	}
	return filepath.Join(r.storesDir, id, "overlay")
}

// Delete deletes a store and all its contents.
func (r *FileStoreRepo) Delete(id string) error {
	// Validate store ID for safety
	if err := r.fs.ValidateIdentifier(id); err != nil {
		return fmt.Errorf("invalid store ID: %w", err)
	}

	storePath := filepath.Join(r.storesDir, id)

	if err := r.fs.RemoveAll(storePath); err != nil {
		return fmt.Errorf("failed to delete store: %w", err)
	}

	return nil
}
