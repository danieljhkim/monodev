package persist

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/danieljhkim/monodev/internal/hash"
	"github.com/danieljhkim/monodev/internal/state"
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

	if _, err := state.CheckSchemaVersion(manifestPath, data, verificationManifestSchemaVersion); err != nil {
		return err
	}
	var manifest verificationManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return fmt.Errorf("store %q path %s: invalid verification manifest: %w", storeID, manifestPath, err)
	}
	if manifest.HashAlgorithm != verificationHashAlgorithm {
		return fmt.Errorf("store %q path %s: unsupported verification hash algorithm %q", storeID, manifestPath, manifest.HashAlgorithm)
	}
	if err := s.verifyManifestFiles(storeID, storePath, manifest, hasher); err != nil {
		return err
	}

	return nil
}

// DiffAgainstLocalCopy compares the content about to be pulled for storeID
// (staged in persistRoot) against the store's existing local copy in
// storeRepo, if one is already present, and returns the sorted relative
// paths that were added, removed, or changed. It returns nil with no error
// when there is no local copy to compare against, since a first-time pull
// has nothing to diverge from.
//
// This check is deliberately independent of the verification manifest: the
// manifest travels with the persisted content itself, so an actor with push
// access to the persistence branch can rewrite both together and still pass
// Verify. The developer's own pre-existing local copy is the one thing a
// remote actor cannot have already rewritten, which is what makes it useful
// for surfacing tampering (or a legitimate but unexpected remote edit)
// before it lands in the working tree.
func (s *SnapshotManager) DiffAgainstLocalCopy(storeID, persistRoot string, storeRepo stores.StoreRepo, hasher hash.Hasher) ([]string, error) {
	if err := s.fs.ValidateIdentifier(storeID); err != nil {
		return nil, fmt.Errorf("invalid store ID: %w", err)
	}

	localPath := filepath.Dir(storeRepo.OverlayRoot(storeID))

	// Check the local store directory directly rather than going through
	// storeRepo's own bookkeeping: what matters here is whether there is
	// already content on disk that this pull would overwrite.
	exists, err := s.fs.Exists(localPath)
	if err != nil {
		return nil, fmt.Errorf("failed to check if store %q exists locally: %w", storeID, err)
	}
	if !exists {
		return nil, nil
	}

	if hasher == nil {
		hasher = hash.NewSHA256Hasher()
	}

	persistPath := persistStoreDir(persistRoot, storeID)

	localFiles, err := verifiableStoreFiles(localPath)
	if err != nil {
		return nil, fmt.Errorf("store %q: failed to inspect local copy: %w", storeID, err)
	}
	persistFiles, err := verifiableStoreFiles(persistPath)
	if err != nil {
		return nil, fmt.Errorf("store %q: failed to inspect persisted copy: %w", storeID, err)
	}

	localSet := make(map[string]struct{}, len(localFiles))
	for _, relPath := range localFiles {
		localSet[relPath] = struct{}{}
	}
	persistSet := make(map[string]struct{}, len(persistFiles))
	for _, relPath := range persistFiles {
		persistSet[relPath] = struct{}{}
	}

	changedSet := make(map[string]struct{})
	for relPath := range persistSet {
		if _, ok := localSet[relPath]; !ok {
			changedSet[relPath] = struct{}{}
			continue
		}
		localHash, err := hasher.HashFile(filepath.Join(localPath, filepath.FromSlash(relPath)))
		if err != nil {
			return nil, fmt.Errorf("store %q path %s: failed to hash local file: %w", storeID, relPath, err)
		}
		persistHash, err := hasher.HashFile(filepath.Join(persistPath, filepath.FromSlash(relPath)))
		if err != nil {
			return nil, fmt.Errorf("store %q path %s: failed to hash persisted file: %w", storeID, relPath, err)
		}
		if localHash != persistHash {
			changedSet[relPath] = struct{}{}
		}
	}
	for relPath := range localSet {
		if _, ok := persistSet[relPath]; !ok {
			changedSet[relPath] = struct{}{}
		}
	}

	if len(changedSet) == 0 {
		return nil, nil
	}
	changed := make([]string, 0, len(changedSet))
	for relPath := range changedSet {
		changed = append(changed, relPath)
	}
	sort.Strings(changed)
	return changed, nil
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
	if err := s.fs.AtomicWrite(manifestPath, data, 0600); err != nil {
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
