package persist

import (
	"fmt"
	"os"
)

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
