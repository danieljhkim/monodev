package fsops

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// Copy copies a file or directory from src to dst.
//
// Monodev-managed copies reject symlinks instead of following or preserving
// them. Store snapshots cross a trust boundary, so link targets must never be
// read implicitly while copying store content.
func (fs *RealFS) Copy(src, dst string) error {
	if err := ValidateCopySource(src); err != nil {
		return err
	}

	srcInfo, err := os.Lstat(src)
	if err != nil {
		return fmt.Errorf("failed to stat source: %w", err)
	}

	// Check if destination exists and remove it if type mismatch
	dstInfo, err := os.Lstat(dst)
	if err == nil {
		// Destination exists - check for type mismatch
		if srcInfo.IsDir() != dstInfo.IsDir() {
			// Source and destination types don't match, remove destination
			if err := os.RemoveAll(dst); err != nil {
				return fmt.Errorf("failed to remove existing destination: %w", err)
			}
		}
	} else if !os.IsNotExist(err) {
		// Error other than "not exists"
		return fmt.Errorf("failed to stat destination: %w", err)
	}

	if srcInfo.IsDir() {
		return fs.copyDir(src, dst, filepath.Clean(src))
	}
	return fs.copyFile(src, dst, srcInfo.Mode(), ".")
}

// copyFile copies a single file from src to dst.
func (fs *RealFS) copyFile(src, dst string, mode os.FileMode, relPath string) error {
	// Defensive check: verify source is not a directory
	srcInfo, err := os.Lstat(src)
	if err != nil {
		return fmt.Errorf("failed to stat source: %w", err)
	}
	if srcInfo.Mode()&os.ModeSymlink != 0 {
		return unsafeSymlinkError(relPath)
	}
	if srcInfo.IsDir() {
		return fmt.Errorf("copyFile called on directory %q - this is a bug", src)
	}
	if !srcInfo.Mode().IsRegular() {
		return fmt.Errorf("unsupported source file type at %q", relPath)
	}

	srcFile, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("failed to open source: %w", err)
	}
	defer func() {
		_ = srcFile.Close()
	}()

	// Create parent directory if needed
	if err := os.MkdirAll(filepath.Dir(dst), 0700); err != nil {
		return fmt.Errorf("failed to create parent directory: %w", err)
	}

	if dstInfo, err := os.Lstat(dst); err == nil {
		if dstInfo.Mode()&os.ModeSymlink != 0 || dstInfo.IsDir() {
			if err := os.RemoveAll(dst); err != nil {
				return fmt.Errorf("failed to remove existing destination: %w", err)
			}
		}
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("failed to stat destination: %w", err)
	}

	dstFile, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, privateFileMode(mode))
	if err != nil {
		return fmt.Errorf("failed to create destination: %w", err)
	}
	defer func() {
		_ = dstFile.Close()
	}()

	if _, err := io.Copy(dstFile, srcFile); err != nil {
		return fmt.Errorf("failed to copy file contents: %w", err)
	}

	return dstFile.Sync()
}

// copyDir recursively copies a directory from src to dst.
func (fs *RealFS) copyDir(src, dst, root string) error {
	srcInfo, err := os.Lstat(src)
	if err != nil {
		return fmt.Errorf("failed to stat source directory: %w", err)
	}
	if srcInfo.Mode()&os.ModeSymlink != 0 {
		relPath, relErr := copyRelPath(root, src)
		if relErr != nil {
			return relErr
		}
		return unsafeSymlinkError(relPath)
	}

	if dstInfo, err := os.Lstat(dst); err == nil {
		if dstInfo.Mode()&os.ModeSymlink != 0 || !dstInfo.IsDir() {
			if err := os.RemoveAll(dst); err != nil {
				return fmt.Errorf("failed to remove existing destination: %w", err)
			}
		}
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("failed to stat destination: %w", err)
	}

	if err := os.MkdirAll(dst, 0700); err != nil {
		return fmt.Errorf("failed to create destination directory: %w", err)
	}

	entries, err := os.ReadDir(src)
	if err != nil {
		return fmt.Errorf("failed to read source directory: %w", err)
	}

	for _, entry := range entries {
		srcPath := filepath.Join(src, entry.Name())
		dstPath := filepath.Join(dst, entry.Name())
		relPath, err := copyRelPath(root, srcPath)
		if err != nil {
			return err
		}

		info, err := os.Lstat(srcPath)
		if err != nil {
			return fmt.Errorf("failed to get entry info for %q: %w", relPath, err)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return unsafeSymlinkError(relPath)
		}

		if info.IsDir() {
			if err := fs.copyDir(srcPath, dstPath, root); err != nil {
				return err
			}
		} else {
			if err := fs.copyFile(srcPath, dstPath, info.Mode(), relPath); err != nil {
				return err
			}
		}
	}

	return nil
}

// privateFileMode preserves owner read/write and execute intent while
// preventing copied store content from granting group or other access.
func privateFileMode(mode os.FileMode) os.FileMode {
	return mode.Perm() & 0700
}

// ValidateCopySource enforces monodev's managed-copy symlink policy before any
// destination mutation happens. Symlinks are rejected by relative path so store
// snapshot operations never read link targets across local/persist boundaries.
func ValidateCopySource(src string) error {
	root := filepath.Clean(src)
	return validateCopySource(root, root)
}

func validateCopySource(root, path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("failed to stat source: %w", err)
	}

	relPath, err := copyRelPath(root, path)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return unsafeSymlinkError(relPath)
	}
	if !info.IsDir() {
		if !info.Mode().IsRegular() {
			return fmt.Errorf("unsupported source file type at %q", relPath)
		}
		return nil
	}

	entries, err := os.ReadDir(path)
	if err != nil {
		return fmt.Errorf("failed to read source directory %q: %w", relPath, err)
	}
	for _, entry := range entries {
		if err := validateCopySource(root, filepath.Join(path, entry.Name())); err != nil {
			return err
		}
	}
	return nil
}

func copyRelPath(root, path string) (string, error) {
	relPath, err := filepath.Rel(root, path)
	if err != nil {
		return "", fmt.Errorf("failed to derive relative path for %s: %w", path, err)
	}
	return filepath.ToSlash(relPath), nil
}

func unsafeSymlinkError(relPath string) error {
	return fmt.Errorf("refusing to copy symlink %q: monodev-managed copies reject symlinks so link targets are never read across store boundaries", relPath)
}
