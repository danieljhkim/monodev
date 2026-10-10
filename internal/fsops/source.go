package fsops

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"golang.org/x/sys/unix"
)

// openCopySource pins the source through directory-relative, no-follow opens.
// Never canonicalize arbitrary source paths with EvalSymlinks: doing so would
// erase the very links managed snapshots must reject.
func openCopySource(path string) (*os.File, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	// macOS exposes these fixed system aliases. Use their known canonical
	// locations without following a user-controlled link or its replacement.
	if runtime.GOOS == "darwin" {
		for _, alias := range []string{"/tmp", "/var", "/etc"} {
			if absolute == alias || strings.HasPrefix(absolute, alias+"/") {
				absolute = "/private" + absolute
				break
			}
		}
	}
	fd, err := unix.Open("/", unix.O_RDONLY|unix.O_CLOEXEC|unix.O_DIRECTORY|unix.O_NOFOLLOW, 0)
	if err != nil {
		return nil, fmt.Errorf("failed to open source filesystem root: %w", err)
	}
	current := os.NewFile(uintptr(fd), "/")
	parts := strings.Split(strings.TrimPrefix(absolute, "/"), "/")
	if absolute == "/" {
		return current, nil
	}
	for i, part := range parts {
		next, err := openSourceAt(current, part, strings.Join(parts[:i+1], "/"), i < len(parts)-1)
		_ = current.Close()
		if err != nil {
			return nil, err
		}
		current = next
	}
	return current, nil
}

func openSourceAt(parent *os.File, name, relPath string, directory bool) (*os.File, error) {
	// Inspect without following first, including special files that could block
	// or have side effects on open. O_NOFOLLOW and the post-open Stat preserve
	// the invariant if the entry changes between inspection and open.
	var st unix.Stat_t
	if err := unix.Fstatat(int(parent.Fd()), name, &st, unix.AT_SYMLINK_NOFOLLOW); err != nil {
		return nil, fmt.Errorf("failed to inspect source %q: %w", relPath, err)
	}
	if isSymlinkMode(uint32(st.Mode)) {
		return nil, unsafeSymlinkError(relPath)
	}
	if !isDirectoryMode(uint32(st.Mode)) && uint32(st.Mode)&unix.S_IFMT != unix.S_IFREG {
		return nil, fmt.Errorf("unsupported source file type at %q", relPath)
	}
	flags := unix.O_RDONLY | unix.O_CLOEXEC | unix.O_NOFOLLOW | unix.O_NONBLOCK
	if directory || isDirectoryMode(uint32(st.Mode)) {
		flags |= unix.O_DIRECTORY
	}
	fd, err := unix.Openat(int(parent.Fd()), name, flags, 0)
	if err != nil {
		if errors.Is(err, unix.ELOOP) || errors.Is(err, unix.ENOTDIR) {
			return nil, fmt.Errorf("unsafe symlinked or replaced source %q: %w", relPath, err)
		}
		return nil, fmt.Errorf("failed to open source %q: %w", relPath, err)
	}
	file := os.NewFile(uintptr(fd), name)
	info, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return nil, err
	}
	if (!info.IsDir() && !info.Mode().IsRegular()) || (directory && !info.IsDir()) {
		_ = file.Close()
		return nil, fmt.Errorf("unsupported or replaced source file type at %q", relPath)
	}
	return file, nil
}

func validateSourceHandle(source *os.File, relPath string, excluded map[string]bool) error {
	info, err := source.Stat()
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return nil
	}
	return walkSourceChildren(source, relPath, excluded, func(child *os.File, _ os.FileInfo, childRel string) error {
		return validateSourceHandle(child, childRel, excluded)
	})
}

func walkSourceChildren(source *os.File, relPath string, excluded map[string]bool, visit func(*os.File, os.FileInfo, string) error) error {
	// A fresh handle to the pinned directory starts enumeration at offset zero,
	// allowing preflight and execution to traverse the same root independently.
	dir, err := openSourceAt(source, ".", relPath, true)
	if err != nil {
		return err
	}
	entries, err := dir.ReadDir(-1)
	_ = dir.Close()
	if err != nil {
		return fmt.Errorf("failed to read source directory %q: %w", relPath, err)
	}
	for _, entry := range entries {
		childRel := filepath.Join(relPath, entry.Name())
		if excluded[filepath.Clean(childRel)] {
			continue
		}
		child, err := openSourceAt(source, entry.Name(), filepath.ToSlash(childRel), false)
		if err != nil {
			return err
		}
		info, err := child.Stat()
		if err == nil {
			err = visit(child, info, childRel)
		}
		_ = child.Close()
		if err != nil {
			return err
		}
	}
	return nil
}

// OpenRegularSource opens a regular file using the managed-copy source policy.
// Snapshot checksums and transaction backups must not reopen a validated path
// with os.Open, which could follow a replacement link.
func OpenRegularSource(path string) (*os.File, error) {
	file, err := openCopySource(path)
	if err != nil {
		return nil, err
	}
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		_ = file.Close()
		if err != nil {
			return nil, err
		}
		return nil, fmt.Errorf("expected regular source file at %q", path)
	}
	return file, nil
}
