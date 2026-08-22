package persist

import (
	"fmt"
	"os"
	"path/filepath"
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
