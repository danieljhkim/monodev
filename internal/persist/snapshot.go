package persist

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/danieljhkim/monodev/internal/fsops"
	"github.com/danieljhkim/monodev/internal/hash"
	"github.com/danieljhkim/monodev/internal/stores"
)

const (
	verificationManifestName          = "verification-manifest.json"
	verificationManifestSchemaVersion = 1
	verificationHashAlgorithm         = "sha256"
)

// ErrVerificationManifestMissing is returned for persisted stores created
// before checksum manifests existed. Callers may keep pulling these legacy
// stores for compatibility, but they must not report checksum verification as
// successful when this error is returned.
var ErrVerificationManifestMissing = errors.New("verification manifest missing")

type verificationManifest struct {
	SchemaVersion int                        `json:"schemaVersion"`
	HashAlgorithm string                     `json:"hashAlgorithm"`
	Files         []verificationManifestFile `json:"files"`
}

type verificationManifestFile struct {
	Path string `json:"path"`
	Hash string `json:"hash"`
}

// SnapshotManager handles materialization and dematerialization of stores
// between the user's home directory (~/.monodev/stores) and the persistence
// directory (.monodev/persist/stores).
type SnapshotManager struct {
	fs fsops.FS
}

// NewSnapshotManager creates a new SnapshotManager.
func NewSnapshotManager(fs fsops.FS) *SnapshotManager {
	return &SnapshotManager{fs: fs}
}

// persistStoresDir returns the path to the persist stores directory.
func persistStoresDir(persistRoot string) string {
	return filepath.Join(persistRoot, ".monodev", "persist", "stores")
}

// persistStoreDir returns the path to a specific store in the persist directory.
func persistStoreDir(persistRoot, storeID string) string {
	return filepath.Join(persistStoresDir(persistRoot), storeID)
}

// Materialize copies a store from ~/.monodev/stores/<store-id> to
// .monodev/persist/stores/<store-id>/.
func (s *SnapshotManager) Materialize(storeID string, storeRepo stores.StoreRepo, persistRoot string) error {
	// Validate store ID
	if err := s.fs.ValidateIdentifier(storeID); err != nil {
		return fmt.Errorf("invalid store ID: %w", err)
	}

	// Check if store exists
	exists, err := storeRepo.Exists(storeID)
	if err != nil {
		return fmt.Errorf("failed to check if store exists: %w", err)
	}
	if !exists {
		return fmt.Errorf("store %q not found", storeID)
	}

	// Get the store path - overlay root's parent directory
	storePath := filepath.Dir(storeRepo.OverlayRoot(storeID))

	// Destination path
	dstPath := persistStoreDir(persistRoot, storeID)

	if err := fsops.ValidateCopySource(storePath); err != nil {
		return fmt.Errorf("store %q contains an unsafe copy source: %w", storeID, err)
	}

	// Remove existing destination if present
	if exists, err := s.fs.Exists(dstPath); err != nil {
		return fmt.Errorf("failed to check destination: %w", err)
	} else if exists {
		if err := s.fs.RemoveAll(dstPath); err != nil {
			return fmt.Errorf("failed to remove existing destination: %w", err)
		}
	}

	// Copy the store directory
	if err := s.fs.Copy(storePath, dstPath); err != nil {
		return fmt.Errorf("failed to copy store: %w", err)
	}

	if err := s.writeVerificationManifest(storeID, dstPath, hash.NewSHA256Hasher()); err != nil {
		return fmt.Errorf("failed to write verification manifest for store %q: %w", storeID, err)
	}

	return nil
}

// Dematerialize copies a store from .monodev/persist/stores/<store-id>/ to
// ~/.monodev/stores/<store-id>/.
func (s *SnapshotManager) Dematerialize(storeID string, persistRoot string, storeRepo stores.StoreRepo) error {
	// Validate store ID
	if err := s.fs.ValidateIdentifier(storeID); err != nil {
		return fmt.Errorf("invalid store ID: %w", err)
	}

	// Source path in persist directory
	srcPath := persistStoreDir(persistRoot, storeID)

	// Check if source exists
	exists, err := s.fs.Exists(srcPath)
	if err != nil {
		return fmt.Errorf("failed to check if persist store exists: %w", err)
	}
	if !exists {
		return fmt.Errorf("store %q not found in persist directory at %s", storeID, srcPath)
	}

	// Destination path - overlay root's parent directory
	dstPath := filepath.Dir(storeRepo.OverlayRoot(storeID))

	if err := fsops.ValidateCopySource(srcPath); err != nil {
		return fmt.Errorf("persisted store %q contains an unsafe copy source: %w", storeID, err)
	}

	// Remove existing destination if present
	if exists, err := s.fs.Exists(dstPath); err != nil {
		return fmt.Errorf("failed to check destination: %w", err)
	} else if exists {
		if err := s.fs.RemoveAll(dstPath); err != nil {
			return fmt.Errorf("failed to remove existing destination: %w", err)
		}
	}

	// Copy the store directory
	if err := s.fs.Copy(srcPath, dstPath); err != nil {
		return fmt.Errorf("failed to copy store: %w", err)
	}

	return nil
}

// Verify verifies the integrity of a store in the persist directory using the
// persisted verification manifest. The manifest covers meta.json, track.json,
// and every regular file under overlay/. Stores without a manifest are legacy
// snapshots and return ErrVerificationManifestMissing instead of pretending that
// checksum verification succeeded.
func (s *SnapshotManager) Verify(storeID string, persistRoot string, hasher hash.Hasher) error {
	// Validate store ID
	if err := s.fs.ValidateIdentifier(storeID); err != nil {
		return fmt.Errorf("invalid store ID: %w", err)
	}

	// Path to store in persist directory
	storePath := persistStoreDir(persistRoot, storeID)

	// Check if store exists
	exists, err := s.fs.Exists(storePath)
	if err != nil {
		return fmt.Errorf("failed to check if store exists: %w", err)
	}
	if !exists {
		return fmt.Errorf("store %q not found in persist directory at %s", storeID, storePath)
	}

	manifestPath := filepath.Join(storePath, verificationManifestName)
	data, err := s.fs.ReadFile(manifestPath)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("store %q path %s: %w", storeID, manifestPath, ErrVerificationManifestMissing)
		}
		return fmt.Errorf("store %q path %s: failed to read verification manifest: %w", storeID, manifestPath, err)
	}

	var manifest verificationManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return fmt.Errorf("store %q path %s: invalid verification manifest: %w", storeID, manifestPath, err)
	}
	if manifest.SchemaVersion != verificationManifestSchemaVersion {
		return fmt.Errorf("store %q path %s: unsupported verification manifest schema version %d", storeID, manifestPath, manifest.SchemaVersion)
	}
	if manifest.HashAlgorithm != verificationHashAlgorithm {
		return fmt.Errorf("store %q path %s: unsupported verification hash algorithm %q", storeID, manifestPath, manifest.HashAlgorithm)
	}
	if err := s.verifyManifestFiles(storeID, storePath, manifest, hasher); err != nil {
		return err
	}

	return nil
}

// ListPersistedStores returns a list of store IDs available in the persist directory.
func (s *SnapshotManager) ListPersistedStores(persistRoot string) ([]string, error) {
	storesDir := persistStoresDir(persistRoot)

	// Check if persist directory exists
	exists, err := s.fs.Exists(storesDir)
	if err != nil {
		return nil, fmt.Errorf("failed to check persist stores directory: %w", err)
	}
	if !exists {
		// No persisted stores yet
		return []string{}, nil
	}

	// Read directory entries
	entries, err := os.ReadDir(storesDir)
	if err != nil {
		if os.IsNotExist(err) {
			return []string{}, nil
		}
		return nil, fmt.Errorf("failed to read persist stores directory: %w", err)
	}

	var storeIDs []string
	for _, entry := range entries {
		if entry.IsDir() {
			storeIDs = append(storeIDs, entry.Name())
		}
	}

	return storeIDs, nil
}

func (s *SnapshotManager) writeVerificationManifest(storeID, storePath string, hasher hash.Hasher) error {
	if hasher == nil {
		hasher = hash.NewSHA256Hasher()
	}

	relPaths, err := verifiableStoreFiles(storePath)
	if err != nil {
		return err
	}

	manifest := verificationManifest{
		SchemaVersion: verificationManifestSchemaVersion,
		HashAlgorithm: verificationHashAlgorithm,
		Files:         make([]verificationManifestFile, 0, len(relPaths)),
	}
	for _, relPath := range relPaths {
		absPath := filepath.Join(storePath, filepath.FromSlash(relPath))
		sum, err := hasher.HashFile(absPath)
		if err != nil {
			return fmt.Errorf("store %q path %s: failed to hash persisted file: %w", storeID, absPath, err)
		}
		manifest.Files = append(manifest.Files, verificationManifestFile{
			Path: relPath,
			Hash: sum,
		})
	}

	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to encode verification manifest: %w", err)
	}
	data = append(data, '\n')

	manifestPath := filepath.Join(storePath, verificationManifestName)
	if err := s.fs.AtomicWrite(manifestPath, data, 0644); err != nil {
		return fmt.Errorf("path %s: failed to write verification manifest: %w", manifestPath, err)
	}

	return nil
}

func (s *SnapshotManager) verifyManifestFiles(storeID, storePath string, manifest verificationManifest, hasher hash.Hasher) error {
	if hasher == nil {
		hasher = hash.NewSHA256Hasher()
	}

	manifestPath := filepath.Join(storePath, verificationManifestName)
	expected := make(map[string]string, len(manifest.Files))
	for _, file := range manifest.Files {
		if file.Path == "" {
			return fmt.Errorf("store %q path %s: verification manifest contains an empty file path", storeID, manifestPath)
		}
		relPath := filepath.Clean(filepath.FromSlash(file.Path))
		if err := s.fs.ValidateRelPath(relPath); err != nil {
			return fmt.Errorf("store %q path %s: invalid manifest file path %q: %w", storeID, manifestPath, file.Path, err)
		}
		relPath = filepath.ToSlash(relPath)
		if relPath == verificationManifestName {
			return fmt.Errorf("store %q path %s: verification manifest must not hash itself", storeID, manifestPath)
		}
		if _, exists := expected[relPath]; exists {
			return fmt.Errorf("store %q path %s: duplicate manifest entry for %s", storeID, manifestPath, relPath)
		}
		expected[relPath] = file.Hash
	}

	for _, requiredPath := range []string{"meta.json", "track.json"} {
		if _, ok := expected[requiredPath]; !ok {
			return fmt.Errorf("store %q path %s: required file missing from verification manifest", storeID, filepath.Join(storePath, requiredPath))
		}
	}

	actualFiles, err := verifiableStoreFiles(storePath)
	if err != nil {
		return fmt.Errorf("store %q: %w", storeID, err)
	}
	actual := make(map[string]struct{}, len(actualFiles))
	for _, relPath := range actualFiles {
		actual[relPath] = struct{}{}
		if _, ok := expected[relPath]; !ok {
			return fmt.Errorf("store %q path %s: persisted file missing from verification manifest", storeID, filepath.Join(storePath, filepath.FromSlash(relPath)))
		}
	}

	for relPath, expectedHash := range expected {
		absPath := filepath.Join(storePath, filepath.FromSlash(relPath))
		if _, ok := actual[relPath]; !ok {
			return fmt.Errorf("store %q path %s: missing persisted file", storeID, absPath)
		}

		actualHash, err := hasher.HashFile(absPath)
		if err != nil {
			return fmt.Errorf("store %q path %s: failed to hash persisted file: %w", storeID, absPath, err)
		}
		if actualHash != expectedHash {
			return fmt.Errorf("store %q path %s: checksum mismatch: expected %s, got %s", storeID, absPath, expectedHash, actualHash)
		}
	}

	return nil
}

func verifiableStoreFiles(storePath string) ([]string, error) {
	var relPaths []string
	for _, relPath := range []string{"meta.json", "track.json"} {
		absPath := filepath.Join(storePath, relPath)
		info, err := os.Lstat(absPath)
		if err != nil {
			if os.IsNotExist(err) {
				return nil, fmt.Errorf("missing required file %s", absPath)
			}
			return nil, fmt.Errorf("failed to inspect required file %s: %w", absPath, err)
		}
		if !info.Mode().IsRegular() {
			return nil, fmt.Errorf("required file %s is not a regular file", absPath)
		}
		relPaths = append(relPaths, relPath)
	}

	overlayPath := filepath.Join(storePath, "overlay")
	info, err := os.Lstat(overlayPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("missing required directory %s", overlayPath)
		}
		return nil, fmt.Errorf("failed to inspect required directory %s: %w", overlayPath, err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("required path %s is not a directory", overlayPath)
	}

	if err := filepath.WalkDir(overlayPath, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return fmt.Errorf("failed to inspect path %s: %w", path, walkErr)
		}
		if path == overlayPath {
			return nil
		}
		if entry.IsDir() {
			return nil
		}

		info, err := entry.Info()
		if err != nil {
			return fmt.Errorf("failed to inspect path %s: %w", path, err)
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("unsupported persisted file type at %s", path)
		}

		relPath, err := filepath.Rel(storePath, path)
		if err != nil {
			return fmt.Errorf("failed to derive relative path for %s: %w", path, err)
		}
		relPaths = append(relPaths, filepath.ToSlash(relPath))
		return nil
	}); err != nil {
		return nil, err
	}

	sort.Strings(relPaths)
	return relPaths, nil
}
