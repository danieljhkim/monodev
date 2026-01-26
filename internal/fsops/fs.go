package fsops

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// FS provides an abstraction for filesystem operations.
// All filesystem mutations in monodev must go through this interface.
type FS interface {
	// Lstat returns file info without following symlinks.
	Lstat(path string) (os.FileInfo, error)

	// Readlink reads the target of a symlink.
	Readlink(path string) (string, error)

	// MkdirAll creates a directory and all parent directories.
	MkdirAll(path string, perm os.FileMode) error

	// Remove removes a file or empty directory.
	Remove(path string) error

	// RemoveAll removes a path and all its contents.
	RemoveAll(path string) error

	// Symlink creates a symbolic link from newname to oldname.
	Symlink(oldname, newname string) error

	// Copy copies a file or directory from src to dst.
	Copy(src, dst string) error

	// AtomicWrite writes data to path atomically using temp file + rename.
	AtomicWrite(path string, data []byte, perm os.FileMode) error

	// ReadFile reads the entire contents of a file.
	ReadFile(path string) ([]byte, error)

	// Exists checks if a path exists.
	Exists(path string) (bool, error)

	// ValidateRelPath validates a relative path for safety.
	ValidateRelPath(relPath string) error
}

// RealFS implements FS using actual OS operations.
type RealFS struct{}

// NewRealFS creates a new RealFS.
func NewRealFS() *RealFS {
	return &RealFS{}
}

// Lstat returns file info without following symlinks.
func (fs *RealFS) Lstat(path string) (os.FileInfo, error) {
	return os.Lstat(path)
}

// Readlink reads the target of a symlink.
func (fs *RealFS) Readlink(path string) (string, error) {
	return os.Readlink(path)
}

// MkdirAll creates a directory and all parent directories.
func (fs *RealFS) MkdirAll(path string, perm os.FileMode) error {
	return os.MkdirAll(path, perm)
}

// Remove removes a file or empty directory.
func (fs *RealFS) Remove(path string) error {
	return os.Remove(path)
}

// RemoveAll removes a path and all its contents.
func (fs *RealFS) RemoveAll(path string) error {
	return os.RemoveAll(path)
}

// Symlink creates a symbolic link from newname to oldname.
func (fs *RealFS) Symlink(oldname, newname string) error {
	return os.Symlink(oldname, newname)
}

// Copy copies a file or directory from src to dst.
func (fs *RealFS) Copy(src, dst string) error {
	srcInfo, err := os.Lstat(src)
	if err != nil {
		return fmt.Errorf("failed to stat source: %w", err)
	}

	if srcInfo.IsDir() {
		return fs.copyDir(src, dst)
	}
	return fs.copyFile(src, dst, srcInfo.Mode())
}

// copyFile copies a single file from src to dst.
func (fs *RealFS) copyFile(src, dst string, mode os.FileMode) error {
	srcFile, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("failed to open source: %w", err)
	}
	defer func() {
		_ = srcFile.Close()
	}()

	// Create parent directory if needed
	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return fmt.Errorf("failed to create parent directory: %w", err)
	}

	dstFile, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
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
func (fs *RealFS) copyDir(src, dst string) error {
	srcInfo, err := os.Stat(src)
	if err != nil {
		return fmt.Errorf("failed to stat source directory: %w", err)
	}

	if err := os.MkdirAll(dst, srcInfo.Mode()); err != nil {
		return fmt.Errorf("failed to create destination directory: %w", err)
	}

	entries, err := os.ReadDir(src)
	if err != nil {
		return fmt.Errorf("failed to read source directory: %w", err)
	}

	for _, entry := range entries {
		srcPath := filepath.Join(src, entry.Name())
		dstPath := filepath.Join(dst, entry.Name())

		if entry.IsDir() {
			if err := fs.copyDir(srcPath, dstPath); err != nil {
				return err
			}
		} else {
			info, err := entry.Info()
			if err != nil {
				return fmt.Errorf("failed to get entry info: %w", err)
			}
			if err := fs.copyFile(srcPath, dstPath, info.Mode()); err != nil {
				return err
			}
		}
	}

	return nil
}

// AtomicWrite writes data to path atomically using temp file + rename.
func (fs *RealFS) AtomicWrite(path string, data []byte, perm os.FileMode) error {
	// Create parent directory if needed
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create parent directory: %w", err)
	}

	// Create temp file in the same directory as target
	tmpFile, err := os.CreateTemp(dir, ".monodev-tmp-*")
	if err != nil {
		return fmt.Errorf("failed to create temp file: %w", err)
	}
	tmpPath := tmpFile.Name()

	// Clean up temp file on error
	defer func() {
		if tmpFile != nil {
			_ = tmpFile.Close()
			_ = os.Remove(tmpPath)
		}
	}()

	// Write data to temp file
	if _, err := tmpFile.Write(data); err != nil {
		return fmt.Errorf("failed to write to temp file: %w", err)
	}

	// Sync to disk
	if err := tmpFile.Sync(); err != nil {
		return fmt.Errorf("failed to sync temp file: %w", err)
	}

	// Close temp file
	if err := tmpFile.Close(); err != nil {
		return fmt.Errorf("failed to close temp file: %w", err)
	}

	// Set permissions
	if err := os.Chmod(tmpPath, perm); err != nil {
		return fmt.Errorf("failed to set permissions: %w", err)
	}

	// Atomically rename temp file to target
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("failed to rename temp file: %w", err)
	}

	// Success - don't clean up temp file
	tmpFile = nil
	return nil
}

// ReadFile reads the entire contents of a file.
func (fs *RealFS) ReadFile(path string) ([]byte, error) {
	return os.ReadFile(path)
}

// Exists checks if a path exists.
func (fs *RealFS) Exists(path string) (bool, error) {
	_, err := os.Lstat(path)
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, err
}

// validateRelPath validates a relative path for safety.
// Returns an error if the path is invalid or unsafe.
func (fs *RealFS) ValidateRelPath(relPath string) error {
	// Clean the path first
	cleaned := filepath.Clean(relPath)

	// Reject empty or current directory
	if cleaned == "" || cleaned == "." {
		return fmt.Errorf("invalid path: empty or current directory")
	}

	// Reject absolute paths
	if filepath.IsAbs(cleaned) {
		return fmt.Errorf("invalid path: must be relative, got absolute path %q", cleaned)
	}

	// Reject path traversal attempts
	if strings.HasPrefix(cleaned, "..") || strings.Contains(cleaned, string(filepath.Separator)+"..") {
		return fmt.Errorf("invalid path: path traversal not allowed in %q", cleaned)
	}

	return nil
}
