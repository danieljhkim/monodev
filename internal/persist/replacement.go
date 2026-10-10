package persist

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func (s *SnapshotManager) stageStoreReplacement(srcPath, dstPath string) (string, error) {
	dstParent := filepath.Dir(dstPath)
	if err := s.fs.MkdirAll(dstParent, 0700); err != nil {
		return "", fmt.Errorf("failed to create destination parent: %w", err)
	}

	stagedPath, err := os.MkdirTemp(dstParent, replacementTempPrefix(filepath.Base(dstPath), "replacement"))
	if err != nil {
		return "", fmt.Errorf("failed to create staged replacement directory: %w", err)
	}

	if err := s.fs.Copy(srcPath, stagedPath); err != nil {
		_ = s.fs.RemoveAll(stagedPath)
		return "", fmt.Errorf("failed to copy store into staged replacement: %w", err)
	}

	return stagedPath, nil
}

func (s *SnapshotManager) replaceWithStagedStore(dstPath, stagedPath string) error {
	dstExists, err := s.fs.Exists(dstPath)
	if err != nil {
		return fmt.Errorf("failed to check destination: %w", err)
	}
	if !dstExists {
		if err := os.Rename(stagedPath, dstPath); err != nil {
			return fmt.Errorf("failed to move staged store into place: %w", err)
		}
		return nil
	}

	backupPath, err := reserveReplacementPath(filepath.Dir(dstPath), filepath.Base(dstPath), "backup")
	if err != nil {
		return err
	}

	// Directory replacement cannot be a single atomic rename on every platform.
	// Keep the old store as a sibling backup until the complete staged store is
	// in place, then remove the backup.
	if err := os.Rename(dstPath, backupPath); err != nil {
		return fmt.Errorf("failed to move existing store aside: %w", err)
	}

	if err := os.Rename(stagedPath, dstPath); err != nil {
		if restoreErr := os.Rename(backupPath, dstPath); restoreErr != nil {
			return fmt.Errorf("failed to move staged store into place: %w; additionally failed to restore existing store from %s: %v", err, backupPath, restoreErr)
		}
		return fmt.Errorf("failed to move staged store into place; existing store was restored: %w", err)
	}

	if err := s.fs.RemoveAll(backupPath); err != nil {
		return fmt.Errorf("failed to remove previous store backup: %w", err)
	}

	return nil
}

func reserveReplacementPath(parent, storeID, purpose string) (string, error) {
	path, err := os.MkdirTemp(parent, replacementTempPrefix(storeID, purpose))
	if err != nil {
		return "", fmt.Errorf("failed to reserve %s store path: %w", purpose, err)
	}
	if err := os.Remove(path); err != nil {
		return "", fmt.Errorf("failed to reserve %s store path: %w", purpose, err)
	}
	return path, nil
}

func replacementTempPrefix(storeID, purpose string) string {
	return fmt.Sprintf(".monodev-%s-%s-", storeID, purpose)
}

// swappedStore records one completed directory swap so it can be undone.
// backupPath is empty when the destination did not exist before the swap.
type swappedStore struct {
	dstPath    string
	backupPath string
}

// swapInStagedStore moves stagedPath into dstPath, keeping any existing
// destination as a sibling backup. Unlike replaceWithStagedStore it leaves the
// backup in place so the caller can undo the swap until the whole batch lands.
func (s *SnapshotManager) swapInStagedStore(dstPath, stagedPath string) (swappedStore, error) {
	dstExists, err := s.fs.Exists(dstPath)
	if err != nil {
		return swappedStore{}, fmt.Errorf("failed to check destination: %w", err)
	}
	if !dstExists {
		if err := os.Rename(stagedPath, dstPath); err != nil {
			return swappedStore{}, fmt.Errorf("failed to move staged store into place: %w", err)
		}
		return swappedStore{dstPath: dstPath}, nil
	}

	backupPath, err := reserveReplacementPath(filepath.Dir(dstPath), filepath.Base(dstPath), "backup")
	if err != nil {
		return swappedStore{}, err
	}
	if err := os.Rename(dstPath, backupPath); err != nil {
		return swappedStore{}, fmt.Errorf("failed to move existing store aside: %w", err)
	}
	if err := os.Rename(stagedPath, dstPath); err != nil {
		if restoreErr := os.Rename(backupPath, dstPath); restoreErr != nil {
			return swappedStore{}, fmt.Errorf("failed to move staged store into place: %w; additionally failed to restore existing store from %s: %v", err, backupPath, restoreErr)
		}
		return swappedStore{}, fmt.Errorf("failed to move staged store into place; existing store was restored: %w", err)
	}
	return swappedStore{dstPath: dstPath, backupPath: backupPath}, nil
}

// rollbackSwaps undoes completed swaps in reverse order. It keeps going after a
// failure so one stuck store does not strand the others, and reports every
// store it could not restore.
func (s *SnapshotManager) rollbackSwaps(swapped []swappedStore) error {
	var failures []string
	for i := len(swapped) - 1; i >= 0; i-- {
		swap := swapped[i]
		if err := s.fs.RemoveAll(swap.dstPath); err != nil {
			failures = append(failures, fmt.Sprintf("%s: %v", swap.dstPath, err))
			continue
		}
		if swap.backupPath == "" {
			continue
		}
		if err := os.Rename(swap.backupPath, swap.dstPath); err != nil {
			failures = append(failures, fmt.Sprintf("%s (previous copy kept at %s): %v", swap.dstPath, swap.backupPath, err))
		}
	}
	if len(failures) > 0 {
		return fmt.Errorf("%s", strings.Join(failures, "; "))
	}
	return nil
}
