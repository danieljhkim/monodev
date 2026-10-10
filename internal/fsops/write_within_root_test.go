package fsops

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestWriteFileWithinRoot_RefusesSymlinkedAncestor(t *testing.T) {
	root, outside := t.TempDir(), t.TempDir()
	if err := os.Symlink(outside, filepath.Join(root, "dir")); err != nil {
		t.Fatal(err)
	}
	fs := NewRealFS()
	if err := fs.WriteFileWithinRoot(root, "dir/skills/SKILL.md", []byte("x"), 0o644, true); err == nil {
		t.Fatal("write through symlinked ancestor succeeded")
	}
	if _, err := fs.PreflightFileWithinRoot(root, "dir/skills/SKILL.md"); err == nil {
		t.Fatal("preflight accepted a symlinked ancestor")
	}
	entries, _ := os.ReadDir(outside)
	if len(entries) != 0 {
		t.Fatalf("outside modified: %v", entries)
	}
}

func TestWriteFileWithinRoot_ReplacesLeafSymlinkWithoutFollowing(t *testing.T) {
	root, outside := t.TempDir(), t.TempDir()
	sentinel := filepath.Join(outside, "sentinel")
	if err := os.WriteFile(sentinel, []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(sentinel, filepath.Join(root, "SKILL.md")); err != nil {
		t.Fatal(err)
	}
	fs := NewRealFS()
	if _, err := fs.PreflightFileWithinRoot(root, "SKILL.md"); err == nil {
		t.Fatal("preflight accepted a symlinked leaf")
	}
	if err := fs.WriteFileWithinRoot(root, "SKILL.md", []byte("new"), 0o644, true); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(sentinel); string(got) != "keep" {
		t.Fatalf("sentinel = %q", got)
	}
	info, err := os.Lstat(filepath.Join(root, "SKILL.md"))
	if err != nil || !info.Mode().IsRegular() {
		t.Fatalf("leaf not replaced by a regular file: %v", err)
	}
}

func TestWriteFileWithinRoot_ExistingAndOverwrite(t *testing.T) {
	root := t.TempDir()
	fs := NewRealFS()
	if exists, err := fs.PreflightFileWithinRoot(root, "a/b/SKILL.md"); exists || err != nil {
		t.Fatalf("preflight missing = %v, %v", exists, err)
	}
	if err := fs.WriteFileWithinRoot(root, "a/b/SKILL.md", []byte("one"), 0o644, false); err != nil {
		t.Fatal(err)
	}
	if exists, err := fs.PreflightFileWithinRoot(root, "a/b/SKILL.md"); !exists || err != nil {
		t.Fatalf("preflight existing = %v, %v", exists, err)
	}
	err := fs.WriteFileWithinRoot(root, "a/b/SKILL.md", []byte("two"), 0o644, false)
	if !errors.Is(err, ErrWriteTargetExists) {
		t.Fatalf("err = %v, want ErrWriteTargetExists", err)
	}
	if err := fs.WriteFileWithinRoot(root, "a/b/SKILL.md", []byte("two"), 0o644, true); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(filepath.Join(root, "a/b/SKILL.md")); string(got) != "two" {
		t.Fatalf("content = %q", got)
	}
}
