package fsops

import (
	"errors"
	"fmt"
	"os"

	"golang.org/x/sys/unix"
)

// ErrWriteTargetExists is returned by WriteFileWithinRoot when overwrite is
// false and relPath is already occupied.
var ErrWriteTargetExists = errors.New("destination already exists")

// PreflightFileWithinRoot inspects relPath beneath root without following any
// symlink and without creating anything. It reports whether a regular file
// already occupies relPath. A missing ancestor or leaf reports (false, nil).
// A symlinked ancestor, a symlinked leaf, or a leaf that is not a regular file
// is an error, so callers can refuse before mutating any target.
func (fs *RealFS) PreflightFileWithinRoot(root, relPath string) (bool, error) {
	parent, name, closeParent, err := fs.openConfinedParent(root, relPath, false)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		return false, err
	}
	defer closeParent()

	stat, exists, err := lstatAt(parent, name)
	if err != nil || !exists {
		return false, err
	}
	if isSymlinkMode(uint32(stat.Mode)) {
		return true, fmt.Errorf("refusing to write through symlink %q", relPath)
	}
	if uint32(stat.Mode)&unix.S_IFMT != unix.S_IFREG {
		return true, fmt.Errorf("refusing to replace %q: not a regular file", relPath)
	}
	return true, nil
}

// WriteFileWithinRoot writes data to relPath beneath root with permission
// perm. Ancestors are opened one component at a time with O_NOFOLLOW (and
// created when missing), and the data is staged beside the destination and
// swapped in under that parent descriptor, so a symlink at an ancestor or at
// the leaf is refused or replaced, never followed. When overwrite is false and
// relPath is occupied, it returns ErrWriteTargetExists.
func (fs *RealFS) WriteFileWithinRoot(root, relPath string, data []byte, perm os.FileMode, overwrite bool) error {
	parent, name, closeParent, err := fs.openConfinedParent(root, relPath, true)
	if err != nil {
		return err
	}
	defer closeParent()

	if !overwrite {
		if _, exists, err := lstatAt(parent, name); err != nil {
			return err
		} else if exists {
			return fmt.Errorf("%w: %s", ErrWriteTargetExists, relPath)
		}
	}

	tmpName, tmpFD, err := createExclusiveAt(parent, copyTempPrefix, unix.O_CREAT|unix.O_EXCL|unix.O_WRONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW, uint32(perm))
	if err != nil {
		return err
	}
	file := os.NewFile(uintptr(tmpFD), tmpName)
	if file == nil {
		_ = unix.Close(tmpFD)
		_ = unix.Unlinkat(parent, tmpName, 0)
		return fmt.Errorf("failed to create destination file handle")
	}
	success := false
	defer func() {
		_ = file.Close()
		if !success {
			_ = unix.Unlinkat(parent, tmpName, 0)
		}
	}()

	if _, err := file.Write(data); err != nil {
		return fmt.Errorf("failed to write file contents: %w", err)
	}
	if err := file.Chmod(perm); err != nil {
		return fmt.Errorf("failed to set destination permissions: %w", err)
	}
	if err := file.Sync(); err != nil {
		return fmt.Errorf("failed to sync staged file: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("failed to close staged file: %w", err)
	}
	if err := replaceAt(parent, name, tmpName, ""); err != nil {
		return err
	}
	success = true
	return nil
}
