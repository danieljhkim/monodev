package fsops

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/sys/unix"
)

// Copy copies a file or directory from src to dst.
//
// Monodev-managed copies reject symlinks instead of following or preserving
// them. Store snapshots cross a trust boundary, so link targets must never be
// read implicitly while copying store content.
//
// File and directory replacements are staged beside the destination and then
// swapped into place, so a failed copy never truncates or partially overwrites
// the live destination.
func (fs *RealFS) Copy(src, dst string) error {
	if err := ValidateCopySource(src); err != nil {
		return err
	}

	srcInfo, err := os.Lstat(src)
	if err != nil {
		return fmt.Errorf("failed to stat source: %w", err)
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

	return writeFileAtomically(dst, srcFile, privateFileMode(mode))
}

// writeFileAtomically writes r to dst via a sibling temp file + rename so a
// failed copy cannot leave dst truncated. Existing destinations stay intact
// until the staged file is complete.
func writeFileAtomically(dst string, r io.Reader, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0700); err != nil {
		return fmt.Errorf("failed to create parent directory: %w", err)
	}

	tmpFile, err := os.CreateTemp(filepath.Dir(dst), ".monodev-copy-*")
	if err != nil {
		return fmt.Errorf("failed to create staged copy: %w", err)
	}
	tmpPath := tmpFile.Name()
	success := false
	defer func() {
		_ = tmpFile.Close()
		if !success {
			_ = os.Remove(tmpPath)
		}
	}()

	if _, err := io.Copy(tmpFile, r); err != nil {
		return fmt.Errorf("failed to copy file contents: %w", err)
	}
	if err := tmpFile.Sync(); err != nil {
		return fmt.Errorf("failed to sync staged copy: %w", err)
	}
	if err := tmpFile.Chmod(mode); err != nil {
		return fmt.Errorf("failed to set destination permissions: %w", err)
	}
	if err := tmpFile.Close(); err != nil {
		return fmt.Errorf("failed to close staged copy: %w", err)
	}

	if err := replacePath(dst, tmpPath); err != nil {
		return err
	}
	success = true
	return nil
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

	if err := os.MkdirAll(filepath.Dir(dst), 0700); err != nil {
		return fmt.Errorf("failed to create parent directory: %w", err)
	}

	staged, err := os.MkdirTemp(filepath.Dir(dst), ".monodev-copy-*")
	if err != nil {
		return fmt.Errorf("failed to create staged copy: %w", err)
	}
	stagedReady := true
	defer func() {
		if stagedReady {
			_ = os.RemoveAll(staged)
		}
	}()

	if err := fs.copyDirContents(src, staged, root); err != nil {
		return err
	}
	if err := replacePath(dst, staged); err != nil {
		return err
	}
	stagedReady = false
	return nil
}

func (fs *RealFS) copyDirContents(src, dst, root string) error {
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
			if err := os.MkdirAll(dstPath, 0700); err != nil {
				return fmt.Errorf("failed to create destination directory: %w", err)
			}
			if err := fs.copyDirContents(srcPath, dstPath, root); err != nil {
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

// replacePath swaps staged onto dst. The live destination is moved aside only
// after staged is complete, and restored if the final rename fails.
func replacePath(dst, staged string) error {
	_, err := os.Lstat(dst)
	if os.IsNotExist(err) {
		if err := os.Rename(staged, dst); err != nil {
			return fmt.Errorf("failed to move staged copy into place: %w", err)
		}
		return nil
	}
	if err != nil {
		return fmt.Errorf("failed to stat destination: %w", err)
	}

	aside, err := reserveSiblingPath(dst, ".monodev-aside-")
	if err != nil {
		return err
	}
	if err := os.Rename(dst, aside); err != nil {
		return fmt.Errorf("failed to move existing destination aside: %w", err)
	}
	if err := os.Rename(staged, dst); err != nil {
		if restoreErr := os.Rename(aside, dst); restoreErr != nil {
			return fmt.Errorf("failed to move staged copy into place: %w; additionally failed to restore existing destination from %s: %v", err, aside, restoreErr)
		}
		return fmt.Errorf("failed to move staged copy into place; existing destination was restored: %w", err)
	}
	if err := os.RemoveAll(aside); err != nil {
		return fmt.Errorf("failed to remove replaced destination backup: %w", err)
	}
	return nil
}

func reserveSiblingPath(path, prefix string) (string, error) {
	reserved, err := os.MkdirTemp(filepath.Dir(path), prefix)
	if err != nil {
		return "", fmt.Errorf("failed to reserve replacement path: %w", err)
	}
	if err := os.Remove(reserved); err != nil {
		return "", fmt.Errorf("failed to reserve replacement path: %w", err)
	}
	return reserved, nil
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

	tmpName, tmpFD, err := createExclusiveAt(parentFD, ".monodev-copy-", unix.O_CREAT|unix.O_EXCL|unix.O_WRONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW, uint32(privateFileMode(mode)))
	if err != nil {
		return err
	}
	dstFile := os.NewFile(uintptr(tmpFD), tmpName)
	if dstFile == nil {
		_ = unix.Close(tmpFD)
		_ = unix.Unlinkat(parentFD, tmpName, 0)
		return fmt.Errorf("failed to create destination file handle")
	}
	success := false
	defer func() {
		_ = dstFile.Close()
		if !success {
			_ = unix.Unlinkat(parentFD, tmpName, 0)
		}
	}()

	if _, err := io.Copy(dstFile, srcFile); err != nil {
		return fmt.Errorf("failed to copy file contents: %w", err)
	}
	if err := dstFile.Chmod(privateFileMode(mode)); err != nil {
		return fmt.Errorf("failed to set destination permissions: %w", err)
	}
	if err := dstFile.Sync(); err != nil {
		return fmt.Errorf("failed to sync staged copy: %w", err)
	}
	if err := dstFile.Close(); err != nil {
		return fmt.Errorf("failed to close staged copy: %w", err)
	}

	if err := replaceAt(parentFD, name, tmpName); err != nil {
		return err
	}
	success = true
	return nil
}

func (fs *RealFS) copyDirAt(src string, parentFD int, name, relPath string) error {
	tmpName, err := mkdirExclusiveAt(parentFD, ".monodev-copy-")
	if err != nil {
		return err
	}
	success := false
	defer func() {
		if !success {
			_ = removeAllAt(parentFD, tmpName)
		}
	}()

	tmpFD, err := unix.Openat(parentFD, tmpName, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_DIRECTORY|unix.O_NOFOLLOW, 0)
	if err != nil {
		return fmt.Errorf("failed to open staged destination directory: %w", err)
	}
	defer func() { _ = unix.Close(tmpFD) }()

	entries, err := os.ReadDir(src)
	if err != nil {
		return fmt.Errorf("failed to read source directory %q: %w", relPath, err)
	}
	for _, entry := range entries {
		childRel := filepath.Join(relPath, entry.Name())
		if err := fs.copyAt(filepath.Join(src, entry.Name()), tmpFD, entry.Name(), childRel); err != nil {
			return err
		}
	}
	if err := replaceAt(parentFD, name, tmpName); err != nil {
		return err
	}
	success = true
	return nil
}

func replaceAt(parentFD int, name, stagedName string) error {
	_, exists, err := lstatAt(parentFD, name)
	if err != nil {
		return err
	}
	if !exists {
		if err := unix.Renameat(parentFD, stagedName, parentFD, name); err != nil {
			return fmt.Errorf("failed to move staged copy into place: %w", err)
		}
		return nil
	}

	asideName, err := reserveNameAt(parentFD, ".monodev-aside-")
	if err != nil {
		return err
	}
	if err := unix.Renameat(parentFD, name, parentFD, asideName); err != nil {
		return fmt.Errorf("failed to move existing destination aside: %w", err)
	}
	if err := unix.Renameat(parentFD, stagedName, parentFD, name); err != nil {
		if restoreErr := unix.Renameat(parentFD, asideName, parentFD, name); restoreErr != nil {
			return fmt.Errorf("failed to move staged copy into place: %w; additionally failed to restore existing destination: %v", err, restoreErr)
		}
		return fmt.Errorf("failed to move staged copy into place; existing destination was restored: %w", err)
	}
	if err := removeAllAt(parentFD, asideName); err != nil {
		return fmt.Errorf("failed to remove replaced destination backup: %w", err)
	}
	return nil
}

func createExclusiveAt(parentFD int, prefix string, flags int, perm uint32) (string, int, error) {
	for i := 0; i < 10000; i++ {
		name := fmt.Sprintf("%s%d-%d", prefix, os.Getpid(), time.Now().UnixNano()+int64(i))
		fd, err := unix.Openat(parentFD, name, flags, perm)
		if err == nil {
			return name, fd, nil
		}
		if !errors.Is(err, unix.EEXIST) {
			return "", -1, fmt.Errorf("failed to create staged copy: %w", err)
		}
	}
	return "", -1, fmt.Errorf("failed to allocate staged copy name")
}

func mkdirExclusiveAt(parentFD int, prefix string) (string, error) {
	for i := 0; i < 10000; i++ {
		name := fmt.Sprintf("%s%d-%d", prefix, os.Getpid(), time.Now().UnixNano()+int64(i))
		err := unix.Mkdirat(parentFD, name, 0700)
		if err == nil {
			return name, nil
		}
		if !errors.Is(err, unix.EEXIST) {
			return "", fmt.Errorf("failed to create staged copy: %w", err)
		}
	}
	return "", fmt.Errorf("failed to allocate staged copy name")
}

func reserveNameAt(parentFD int, prefix string) (string, error) {
	name, err := mkdirExclusiveAt(parentFD, prefix)
	if err != nil {
		return "", err
	}
	if err := unix.Unlinkat(parentFD, name, unix.AT_REMOVEDIR); err != nil {
		return "", fmt.Errorf("failed to reserve replacement path: %w", err)
	}
	return name, nil
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
