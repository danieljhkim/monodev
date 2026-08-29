// Package fsops provides filesystem operations with safety guarantees.
//
// All filesystem mutations in monodev go through the FS interface, which
// provides abstractions for common operations along with path validation
// to prevent directory traversal attacks and other security issues.
//
// Key features:
//   - Atomic writes using temp file + rename
//   - Path validation for relative paths and identifiers
//   - Symlink-aware operations
//   - Testable via the FS interface
package fsops

import (
	"os"
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

	// Copy copies a file or directory from src to dst. Monodev-managed copies
	// reject source symlinks rather than following link targets.
	Copy(src, dst string) error

	// AtomicWrite writes data to path atomically using temp file + rename.
	AtomicWrite(path string, data []byte, perm os.FileMode) error

	// ReadFile reads the entire contents of a file.
	ReadFile(path string) ([]byte, error)

	// Exists checks if a path exists.
	Exists(path string) (bool, error)

	// ValidateRelPath validates a relative path for safety.
	ValidateRelPath(relPath string) error

	// ValidateIdentifier validates an identifier for safety.
	ValidateIdentifier(id string) error
}

// RootFS is the mutation surface used for applying overlays. Implementations
// operate relative to an opened workspace root so destination ancestors cannot
// be redirected by symlinks between planning and execution.
//
// FS remains separate because lightweight test doubles and non-overlay callers
// do not need to implement the root-confined mutation primitives.
type RootFS interface {
	CopyWithinRoot(root, relPath, src string) error
	RemoveAllWithinRoot(root, relPath string) error
	SymlinkWithinRoot(root, relPath, target string) error
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

// ReadFile reads the entire contents of a file.
func (fs *RealFS) ReadFile(path string) ([]byte, error) {
	return os.ReadFile(path)
}
