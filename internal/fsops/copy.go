package fsops

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/unix"
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

// CopyWithinRoot copies src to relPath beneath an opened workspace root.
// Destination ancestors are opened one component at a time with O_NOFOLLOW,
// so neither an existing symlink nor a concurrent replacement can redirect a
// mutation outside the workspace or into aliased Git metadata.
func (fs *RealFS) CopyWithinRoot(root, relPath, src string) error {
	if err := ValidateCopySource(src); err != nil {
		return err
	}

	parent, name, closeParent, err := fs.openConfinedParent(root, relPath, true)
	if err != nil {
		return err
	}
	defer closeParent()

	return fs.copyAt(src, parent, name, ".")
}

// RemoveAllWithinRoot removes relPath without following any destination
// ancestor or a symlink at the final path.
func (fs *RealFS) RemoveAllWithinRoot(root, relPath string) error {
	parent, name, closeParent, err := fs.openConfinedParent(root, relPath, false)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	defer closeParent()

	return removeAllAt(parent, name)
}

// SymlinkWithinRoot creates a symlink at relPath without following any
// destination ancestor. The link target is intentionally preserved as given
// for compatibility with the legacy symlink apply mode.
func (fs *RealFS) SymlinkWithinRoot(root, relPath, target string) error {
	parent, name, closeParent, err := fs.openConfinedParent(root, relPath, true)
	if err != nil {
		return err
	}
	defer closeParent()

	if err := unix.Symlinkat(target, parent, name); err != nil {
		return fmt.Errorf("failed to create destination symlink: %w", err)
	}
	return nil
}

func (fs *RealFS) openConfinedParent(root, relPath string, create bool) (int, string, func(), error) {
	if err := fs.ValidateRelPath(relPath); err != nil {
		return -1, "", func() {}, err
	}

	cleaned := filepath.Clean(relPath)
	parts := strings.Split(cleaned, string(filepath.Separator))
	for _, part := range parts {
		if part == ".git" {
			return -1, "", func() {}, fmt.Errorf("path resolves inside repository .git directory")
		}
	}

	rootFD, err := unix.Open(root, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_DIRECTORY|unix.O_NOFOLLOW, 0)
	if err != nil {
		return -1, "", func() {}, fmt.Errorf("failed to open workspace root: %w", err)
	}
	currentFD := rootFD
	closeParent := func() {
		_ = unix.Close(currentFD)
	}

	for i, part := range parts[:len(parts)-1] {
		nextFD, openErr := unix.Openat(currentFD, part, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_DIRECTORY|unix.O_NOFOLLOW, 0)
		if openErr != nil && errors.Is(openErr, unix.ENOENT) && create {
			if mkdirErr := unix.Mkdirat(currentFD, part, 0700); mkdirErr != nil && !errors.Is(mkdirErr, unix.EEXIST) {
				closeParent()
				return -1, "", func() {}, fmt.Errorf("failed to create destination ancestor %q: %w", strings.Join(parts[:i+1], string(filepath.Separator)), mkdirErr)
			}
			nextFD, openErr = unix.Openat(currentFD, part, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_DIRECTORY|unix.O_NOFOLLOW, 0)
		}
		if openErr != nil {
			closeParent()
			if errors.Is(openErr, unix.ELOOP) || errors.Is(openErr, unix.ENOTDIR) {
				return -1, "", func() {}, fmt.Errorf("unsafe symlinked destination ancestor %q", strings.Join(parts[:i+1], string(filepath.Separator)))
			}
			return -1, "", func() {}, fmt.Errorf("failed to open destination ancestor %q: %w", strings.Join(parts[:i+1], string(filepath.Separator)), openErr)
		}

		_ = unix.Close(currentFD)
		currentFD = nextFD
	}

	return currentFD, parts[len(parts)-1], closeParent, nil
}

func (fs *RealFS) copyAt(src string, parentFD int, name, relPath string) error {
	srcInfo, err := os.Lstat(src)
	if err != nil {
		return fmt.Errorf("failed to stat source: %w", err)
	}
	if srcInfo.Mode()&os.ModeSymlink != 0 {
		return unsafeSymlinkError(relPath)
	}
	if srcInfo.IsDir() {
		return fs.copyDirAt(src, parentFD, name, relPath)
	}
	if !srcInfo.Mode().IsRegular() {
		return fmt.Errorf("unsupported source file type at %q", relPath)
	}
	return copyFileAt(src, parentFD, name, srcInfo.Mode())
}

func copyFileAt(src string, parentFD int, name string, mode os.FileMode) error {
	srcFile, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("failed to open source: %w", err)
	}
	defer func() { _ = srcFile.Close() }()

	dstInfo, exists, err := lstatAt(parentFD, name)
	if err != nil {
		return err
	}
	if exists && (isSymlinkMode(uint32(dstInfo.Mode)) || isDirectoryMode(uint32(dstInfo.Mode))) {
		if err := removeAllAt(parentFD, name); err != nil {
			return fmt.Errorf("failed to replace existing destination: %w", err)
		}
		exists = false
	}

	dstFD, err := unix.Openat(parentFD, name, unix.O_CREAT|unix.O_WRONLY|unix.O_TRUNC|unix.O_CLOEXEC|unix.O_NOFOLLOW, uint32(privateFileMode(mode)))
	if err != nil {
		return fmt.Errorf("failed to create destination: %w", err)
	}
	dstFile := os.NewFile(uintptr(dstFD), name)
	if dstFile == nil {
		_ = unix.Close(dstFD)
		return fmt.Errorf("failed to create destination file handle")
	}
	defer func() { _ = dstFile.Close() }()

	if _, err := io.Copy(dstFile, srcFile); err != nil {
		return fmt.Errorf("failed to copy file contents: %w", err)
	}
	if !exists {
		if err := dstFile.Chmod(privateFileMode(mode)); err != nil {
			return fmt.Errorf("failed to set destination permissions: %w", err)
		}
	}
	return dstFile.Sync()
}

func (fs *RealFS) copyDirAt(src string, parentFD int, name, relPath string) error {
	dstInfo, exists, err := lstatAt(parentFD, name)
	if err != nil {
		return err
	}
	if exists && (isSymlinkMode(uint32(dstInfo.Mode)) || !isDirectoryMode(uint32(dstInfo.Mode))) {
		if err := removeAllAt(parentFD, name); err != nil {
			return fmt.Errorf("failed to replace existing destination: %w", err)
		}
		exists = false
	}
	if !exists {
		if err := unix.Mkdirat(parentFD, name, 0700); err != nil {
			return fmt.Errorf("failed to create destination directory: %w", err)
		}
	}

	dstFD, err := unix.Openat(parentFD, name, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_DIRECTORY|unix.O_NOFOLLOW, 0)
	if err != nil {
		return fmt.Errorf("failed to open destination directory: %w", err)
	}
	defer func() { _ = unix.Close(dstFD) }()

	entries, err := os.ReadDir(src)
	if err != nil {
		return fmt.Errorf("failed to read source directory %q: %w", relPath, err)
	}
	for _, entry := range entries {
		childRel := filepath.Join(relPath, entry.Name())
		if err := fs.copyAt(filepath.Join(src, entry.Name()), dstFD, entry.Name(), childRel); err != nil {
			return err
		}
	}
	return nil
}

func lstatAt(parentFD int, name string) (*unix.Stat_t, bool, error) {
	var stat unix.Stat_t
	err := unix.Fstatat(parentFD, name, &stat, unix.AT_SYMLINK_NOFOLLOW)
	if err == nil {
		return &stat, true, nil
	}
	if errors.Is(err, unix.ENOENT) {
		return nil, false, nil
	}
	return nil, false, fmt.Errorf("failed to inspect destination %q: %w", name, err)
}

func removeAllAt(parentFD int, name string) error {
	info, exists, err := lstatAt(parentFD, name)
	if err != nil || !exists {
		return err
	}
	if !isDirectoryMode(uint32(info.Mode)) || isSymlinkMode(uint32(info.Mode)) {
		if err := unix.Unlinkat(parentFD, name, 0); err != nil && !errors.Is(err, unix.ENOENT) {
			return err
		}
		return nil
	}

	dirFD, err := unix.Openat(parentFD, name, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_DIRECTORY|unix.O_NOFOLLOW, 0)
	if err != nil {
		if errors.Is(err, unix.ELOOP) || errors.Is(err, unix.ENOTDIR) {
			return unix.Unlinkat(parentFD, name, 0)
		}
		return err
	}
	dirFile := os.NewFile(uintptr(dirFD), name)
	if dirFile == nil {
		_ = unix.Close(dirFD)
		return fmt.Errorf("failed to open destination directory handle")
	}
	entries, readErr := dirFile.ReadDir(-1)
	if readErr != nil {
		_ = dirFile.Close()
		return readErr
	}
	for _, entry := range entries {
		if err := removeAllAt(dirFD, entry.Name()); err != nil {
			_ = dirFile.Close()
			return err
		}
	}
	if err := dirFile.Close(); err != nil {
		return err
	}
	if err := unix.Unlinkat(parentFD, name, unix.AT_REMOVEDIR); err != nil && !errors.Is(err, unix.ENOENT) {
		return err
	}
	return nil
}

func isDirectoryMode(mode uint32) bool {
	return mode&unix.S_IFMT == unix.S_IFDIR
}

func isSymlinkMode(mode uint32) bool {
	return mode&unix.S_IFMT == unix.S_IFLNK
}
