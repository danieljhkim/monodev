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

// Temp names staged beside a destination while it is replaced.
const (
	copyTempPrefix  = ".monodev-copy-"
	asideTempPrefix = ".monodev-aside-"
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
	return fs.copySource(src, dst, nil)
}

// CopyExcept copies a directory like Copy, omitting source-relative exclusions.
func (fs *RealFS) CopyExcept(src, dst string, excluded map[string]bool) error {
	return fs.copySource(src, dst, excluded)
}

func (fs *RealFS) copySource(src, dst string, excluded map[string]bool) error {
	source, err := openCopySource(src)
	if err != nil {
		return err
	}
	defer func() { _ = source.Close() }()
	info, err := source.Stat()
	if err != nil {
		return err
	}
	if len(excluded) > 0 && !info.IsDir() {
		return fmt.Errorf("CopyExcept requires a directory source: %q", src)
	}
	if err := validateSourceHandle(source, ".", excluded); err != nil {
		return err
	}
	if !info.IsDir() {
		return writeFileAtomically(dst, source, privateFileMode(info.Mode()))
	}
	// Staging is created in the destination parent and then the source tree is
	// walked. A destination inside that tree would copy the staging directory
	// into itself, so reject the relationship before creating any parent.
	if err := rejectNestedDirectoryDestination(source, src, dst); err != nil {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(dst), 0700); err != nil {
		return fmt.Errorf("failed to create parent directory: %w", err)
	}
	staged, err := os.MkdirTemp(filepath.Dir(dst), ".monodev-copy-*")
	if err != nil {
		return fmt.Errorf("failed to create staged copy: %w", err)
	}
	success := false
	defer func() {
		if !success {
			_ = os.RemoveAll(staged)
		}
	}()
	if err := fs.copySourceContents(source, staged, ".", excluded); err != nil {
		return err
	}
	if err := replacePath(dst, staged); err != nil {
		return err
	}
	success = true
	return nil
}

func (fs *RealFS) copySourceContents(source *os.File, dst, relPath string, excluded map[string]bool) error {
	return walkSourceChildren(source, relPath, excluded, func(child *os.File, info os.FileInfo, childRel string) error {
		dstPath := filepath.Join(dst, filepath.Base(childRel))
		if info.IsDir() {
			if err := os.Mkdir(dstPath, 0700); err != nil {
				return err
			}
			return fs.copySourceContents(child, dstPath, childRel, excluded)
		}
		return writeFileAtomically(dstPath, child, privateFileMode(info.Mode()))
	})
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
	source, err := openCopySource(src)
	if err != nil {
		return err
	}
	defer func() { _ = source.Close() }()
	return validateSourceHandle(source, ".", nil)
}

func unsafeSymlinkError(relPath string) error {
	return fmt.Errorf("refusing to copy symlink %q: monodev-managed copies reject symlinks so link targets are never read across store boundaries", relPath)
}

// CopyWithinRoot copies src to relPath beneath an opened workspace root.
// Destination ancestors are opened one component at a time with O_NOFOLLOW,
// so neither an existing symlink nor a concurrent replacement can redirect a
// mutation outside the workspace or into aliased Git metadata.
func (fs *RealFS) CopyWithinRoot(root, relPath, src string) error {
	return fs.copyWithinRoot(root, relPath, src, "")
}

// CopyWithinRootOwned copies like CopyWithinRoot, but names every staged temp
// and moved-aside destination after owner. RemoveOwnedTempsWithinRoot can then
// sweep what an interrupted copy left behind without matching unrelated files
// that merely share monodev's temp prefixes.
func (fs *RealFS) CopyWithinRootOwned(root, relPath, src, owner string) error {
	if err := validateTempOwner(owner); err != nil {
		return err
	}
	return fs.copyWithinRoot(root, relPath, src, owner)
}

func (fs *RealFS) copyWithinRoot(root, relPath, src, owner string) error {
	source, err := openCopySource(src)
	if err != nil {
		return err
	}
	defer func() { _ = source.Close() }()
	if err := validateSourceHandle(source, ".", nil); err != nil {
		return err
	}
	info, err := source.Stat()
	if err != nil {
		return fmt.Errorf("failed to stat source: %w", err)
	}
	if info.IsDir() {
		// openConfinedParent creates missing destination ancestors before
		// copyDirAt stages inside the final parent. Reject a destination
		// inside the source first, or that staging directory is walked.
		if err := fs.ValidateRelPath(relPath); err != nil {
			return err
		}
		if err := rejectNestedDirectoryDestination(source, src, filepath.Join(root, relPath)); err != nil {
			return err
		}
	}
	parent, name, closeParent, err := fs.openConfinedParent(root, relPath, true)
	if err != nil {
		return err
	}
	defer closeParent()
	return fs.copyAt(source, parent, name, ".", owner)
}

// RemoveOwnedTempsWithinRoot removes the temps CopyWithinRootOwned staged for
// owner directly beneath dirRel. The directory is opened one component at a
// time without following symlinks, so a redirected ancestor is refused rather
// than swept. Entries for other owners, unowned temps, and unrelated files that
// share the temp prefixes are left untouched.
func (fs *RealFS) RemoveOwnedTempsWithinRoot(root, dirRel, owner string) error {
	if err := validateTempOwner(owner); err != nil {
		return err
	}
	dirFD, closeDir, err := fs.openConfinedDir(root, dirRel)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	defer closeDir()

	listFD, err := unix.Openat(dirFD, ".", unix.O_RDONLY|unix.O_CLOEXEC|unix.O_DIRECTORY|unix.O_NOFOLLOW, 0)
	if err != nil {
		return fmt.Errorf("failed to open temp directory: %w", err)
	}
	dirFile := os.NewFile(uintptr(listFD), dirRel)
	if dirFile == nil {
		_ = unix.Close(listFD)
		return fmt.Errorf("failed to open temp directory handle")
	}
	entries, err := dirFile.ReadDir(-1)
	_ = dirFile.Close()
	if err != nil {
		return err
	}

	copyPrefix := ownedTempPrefix(copyTempPrefix, owner)
	asidePrefix := ownedTempPrefix(asideTempPrefix, owner)
	var errs []error
	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasPrefix(name, copyPrefix) && !strings.HasPrefix(name, asidePrefix) {
			continue
		}
		if err := removeAllAt(dirFD, name); err != nil {
			errs = append(errs, fmt.Errorf("failed to remove owned temp %q: %w", name, err))
		}
	}
	return errors.Join(errs...)
}

// openConfinedDir opens dirRel beneath root without following any symlinked
// component. "." opens root itself.
func (fs *RealFS) openConfinedDir(root, dirRel string) (int, func(), error) {
	if filepath.Clean(dirRel) == "." {
		rootFD, err := unix.Open(root, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_DIRECTORY|unix.O_NOFOLLOW, 0)
		if err != nil {
			return -1, func() {}, fmt.Errorf("failed to open workspace root: %w", err)
		}
		return rootFD, func() { _ = unix.Close(rootFD) }, nil
	}
	parent, name, closeParent, err := fs.openConfinedParent(root, dirRel, false)
	if err != nil {
		return -1, func() {}, err
	}
	defer closeParent()
	dirFD, err := unix.Openat(parent, name, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_DIRECTORY|unix.O_NOFOLLOW, 0)
	if err != nil {
		if errors.Is(err, unix.ELOOP) || errors.Is(err, unix.ENOTDIR) {
			return -1, func() {}, fmt.Errorf("unsafe symlinked destination ancestor %q", filepath.Clean(dirRel))
		}
		return -1, func() {}, fmt.Errorf("failed to open destination directory %q: %w", filepath.Clean(dirRel), err)
	}
	return dirFD, func() { _ = unix.Close(dirFD) }, nil
}

// validateTempOwner keeps owner tags to ASCII letters and digits so an owned
// prefix can never contain a path separator or extend another owner's prefix.
func validateTempOwner(owner string) error {
	if owner == "" || len(owner) > 64 {
		return fmt.Errorf("invalid temp owner %q", owner)
	}
	for _, r := range owner {
		if (r < '0' || r > '9') && (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') {
			return fmt.Errorf("invalid temp owner %q", owner)
		}
	}
	return nil
}

// ownedTempPrefix returns prefix tagged with owner. An empty owner keeps the
// shared prefix used by unowned copies.
func ownedTempPrefix(prefix, owner string) string {
	if owner == "" {
		return prefix
	}
	return prefix + owner + "-"
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

// RestoreTreeWithinRoot recreates the transaction backup at src at relPath
// beneath root, replacing whatever occupies relPath. Unlike CopyWithinRoot,
// symlinks inside the backup are recreated with their recorded targets rather
// than refused: a backup is monodev's own record of the workspace, and link
// targets are copied as strings, never followed. Destination ancestors are
// opened one component at a time with O_NOFOLLOW and the tree is staged and
// swapped beneath that parent descriptor, so an ancestor replaced after an
// earlier confined step is refused instead of redirecting the restore outside
// the workspace. A non-empty owner tags staged temps like CopyWithinRootOwned.
func (fs *RealFS) RestoreTreeWithinRoot(root, relPath, src, owner string) error {
	if owner != "" {
		if err := validateTempOwner(owner); err != nil {
			return err
		}
	}
	cleanSrc := filepath.Clean(src)
	srcParent, err := openCopySource(filepath.Dir(cleanSrc))
	if err != nil {
		return err
	}
	defer func() { _ = srcParent.Close() }()
	parent, name, closeParent, err := fs.openConfinedParent(root, relPath, true)
	if err != nil {
		return err
	}
	defer closeParent()
	return restoreBackupAt(srcParent, filepath.Base(cleanSrc), ".", parent, name, owner)
}

// restoreBackupAt stages backup entry srcName of srcDir beside dstName in
// dstParent and swaps it into place. Directories are rebuilt entry by entry
// inside a staged directory, so nothing is written through a path name.
func restoreBackupAt(srcDir *os.File, srcName, relPath string, dstParent int, dstName, owner string) error {
	var st unix.Stat_t
	if err := unix.Fstatat(int(srcDir.Fd()), srcName, &st, unix.AT_SYMLINK_NOFOLLOW); err != nil {
		return fmt.Errorf("failed to inspect backup %q: %w", relPath, err)
	}
	if isSymlinkMode(uint32(st.Mode)) {
		target, err := readlinkAt(int(srcDir.Fd()), srcName)
		if err != nil {
			return fmt.Errorf("failed to read backup symlink %q: %w", relPath, err)
		}
		return symlinkReplaceAt(target, dstParent, dstName, owner)
	}

	source, err := openSourceAt(srcDir, srcName, relPath, false)
	if err != nil {
		return err
	}
	defer func() { _ = source.Close() }()
	info, err := source.Stat()
	if err != nil {
		return fmt.Errorf("failed to stat backup %q: %w", relPath, err)
	}
	if !info.IsDir() {
		return copyFileAt(source, dstParent, dstName, info.Mode(), owner)
	}

	entries, err := source.ReadDir(-1)
	if err != nil {
		return fmt.Errorf("failed to read backup directory %q: %w", relPath, err)
	}
	tmpName, err := mkdirExclusiveAt(dstParent, ownedTempPrefix(copyTempPrefix, owner))
	if err != nil {
		return err
	}
	success := false
	defer func() {
		if !success {
			_ = removeAllAt(dstParent, tmpName)
		}
	}()
	tmpFD, err := unix.Openat(dstParent, tmpName, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_DIRECTORY|unix.O_NOFOLLOW, 0)
	if err != nil {
		return fmt.Errorf("failed to open staged destination directory: %w", err)
	}
	defer func() { _ = unix.Close(tmpFD) }()

	for _, entry := range entries {
		childRel := filepath.Join(relPath, entry.Name())
		if err := restoreBackupAt(source, entry.Name(), childRel, tmpFD, entry.Name(), owner); err != nil {
			return err
		}
	}
	if err := replaceAt(dstParent, dstName, tmpName, owner); err != nil {
		return err
	}
	success = true
	return nil
}

// symlinkReplaceAt creates a link to target under a reserved temp name in
// parentFD and swaps it onto name, replacing rather than following any entry
// already there.
func symlinkReplaceAt(target string, parentFD int, name, owner string) error {
	tmpName, err := reserveNameAt(parentFD, ownedTempPrefix(copyTempPrefix, owner))
	if err != nil {
		return err
	}
	if err := unix.Symlinkat(target, parentFD, tmpName); err != nil {
		return fmt.Errorf("failed to create destination symlink: %w", err)
	}
	if err := replaceAt(parentFD, name, tmpName, owner); err != nil {
		_ = unix.Unlinkat(parentFD, tmpName, 0)
		return err
	}
	return nil
}

func readlinkAt(dirFD int, name string) (string, error) {
	for size := 256; ; size *= 2 {
		buf := make([]byte, size)
		n, err := unix.Readlinkat(dirFD, name, buf)
		if err != nil {
			return "", err
		}
		if n < size {
			return string(buf[:n]), nil
		}
	}
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

	for i, part := range parts {
		if err := rejectGitAliasAt(currentFD, part); err != nil {
			closeParent()
			return -1, "", func() {}, err
		}
		if i == len(parts)-1 {
			break
		}
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

// rejectGitAliasAt fails when name inside dirFD is the same filesystem object
// as the sibling .git entry. This catches aliases the lexical ".git" check
// cannot see, such as ".GIT" on a case-insensitive volume, without rejecting
// ordinary names like ".github" or a distinct ".GIT" on a case-sensitive one.
// A name that does not exist cannot alias an existing .git entry.
func rejectGitAliasAt(dirFD int, name string) error {
	var gitStat unix.Stat_t
	if err := unix.Fstatat(dirFD, ".git", &gitStat, unix.AT_SYMLINK_NOFOLLOW); err != nil {
		if errors.Is(err, unix.ENOENT) {
			return nil
		}
		return fmt.Errorf("failed to inspect repository .git entry: %w", err)
	}
	var nameStat unix.Stat_t
	if err := unix.Fstatat(dirFD, name, &nameStat, unix.AT_SYMLINK_NOFOLLOW); err != nil {
		if errors.Is(err, unix.ENOENT) {
			return nil
		}
		return fmt.Errorf("failed to inspect destination component %q: %w", name, err)
	}
	if nameStat.Dev == gitStat.Dev && nameStat.Ino == gitStat.Ino {
		return fmt.Errorf("path resolves inside repository .git directory")
	}
	return nil
}

func (fs *RealFS) copyAt(source *os.File, parentFD int, name, relPath, owner string) error {
	info, err := source.Stat()
	if err != nil {
		return fmt.Errorf("failed to stat source: %w", err)
	}
	if info.IsDir() {
		return fs.copyDirAt(source, parentFD, name, relPath, owner)
	}
	return copyFileAt(source, parentFD, name, info.Mode(), owner)
}

func copyFileAt(srcFile *os.File, parentFD int, name string, mode os.FileMode, owner string) error {
	tmpName, tmpFD, err := createExclusiveAt(parentFD, ownedTempPrefix(copyTempPrefix, owner), unix.O_CREAT|unix.O_EXCL|unix.O_WRONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW, uint32(privateFileMode(mode)))
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

	if err := replaceAt(parentFD, name, tmpName, owner); err != nil {
		return err
	}
	success = true
	return nil
}

func (fs *RealFS) copyDirAt(source *os.File, parentFD int, name, relPath, owner string) error {
	tmpName, err := mkdirExclusiveAt(parentFD, ownedTempPrefix(copyTempPrefix, owner))
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

	if err := walkSourceChildren(source, relPath, nil, func(child *os.File, _ os.FileInfo, childRel string) error {
		return fs.copyAt(child, tmpFD, filepath.Base(childRel), childRel, owner)
	}); err != nil {
		return err
	}
	if err := replaceAt(parentFD, name, tmpName, owner); err != nil {
		return err
	}
	success = true
	return nil
}

func replaceAt(parentFD int, name, stagedName, owner string) error {
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

	asideName, err := reserveNameAt(parentFD, ownedTempPrefix(asideTempPrefix, owner))
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

// rejectNestedDirectoryDestination stops a directory copy whose staging parent
// would sit inside the source tree. Same-path replacement stays allowed: it
// stages beside the source, in the source's parent, and then swaps the
// finished tree into place. Nothing is created here.
//
// The nearest existing ancestor is resolved before the comparison. EvalSymlinks
// is used only to learn where the destination write would land, including
// symlink ancestors and the macOS /tmp, /var and /etc aliases. It is not a
// source canonicalization: the copy still reads the pinned source descriptor.
func rejectNestedDirectoryDestination(source *os.File, src, dst string) error {
	var srcStat unix.Stat_t
	if err := unix.Fstat(int(source.Fd()), &srcStat); err != nil {
		return fmt.Errorf("failed to stat copy source: %w", err)
	}
	if !isDirectoryMode(uint32(srcStat.Mode)) {
		return nil
	}

	absDst, err := filepath.Abs(dst)
	if err != nil {
		return fmt.Errorf("failed to resolve copy destination: %w", err)
	}
	stagingParent := filepath.Dir(filepath.Clean(absDst))
	inside, err := pathIsInsideDirectory(srcStat, stagingParent)
	if err != nil {
		return err
	}
	if inside {
		return fmt.Errorf("refusing to copy directory %q into its own descendant %q", src, dst)
	}
	return nil
}

// pathIsInsideDirectory reports whether path names the directory in srcStat
// or a location inside it. Components that do not exist yet count as children
// of the nearest existing ancestor. Symlinks on that ancestor are resolved so
// an alias cannot hide containment. The path is not created.
func pathIsInsideDirectory(srcStat unix.Stat_t, path string) (bool, error) {
	current, err := filepath.Abs(path)
	if err != nil {
		return false, fmt.Errorf("failed to resolve destination ancestor: %w", err)
	}
	current = filepath.Clean(current)
	for {
		_, err := os.Lstat(current)
		if err == nil {
			break
		}
		if !os.IsNotExist(err) {
			return false, fmt.Errorf("failed to inspect destination ancestor %q: %w", current, err)
		}
		parent := filepath.Dir(current)
		if parent == current {
			return false, fmt.Errorf("failed to inspect destination ancestor %q: no existing ancestor", path)
		}
		current = parent
	}

	resolved, err := filepath.EvalSymlinks(current)
	if err != nil {
		return false, fmt.Errorf("failed to resolve destination ancestor %q: %w", current, err)
	}
	resolved = filepath.Clean(resolved)
	for {
		var st unix.Stat_t
		if err := unix.Stat(resolved, &st); err != nil {
			return false, fmt.Errorf("failed to inspect destination ancestor %q: %w", resolved, err)
		}
		if st.Dev == srcStat.Dev && st.Ino == srcStat.Ino {
			return true, nil
		}
		parent := filepath.Dir(resolved)
		if parent == resolved {
			return false, nil
		}
		resolved = parent
	}
}

func isDirectoryMode(mode uint32) bool {
	return mode&unix.S_IFMT == unix.S_IFDIR
}

func isSymlinkMode(mode uint32) bool {
	return mode&unix.S_IFMT == unix.S_IFLNK
}
