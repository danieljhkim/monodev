package fsops

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"
)

func TestRealFS_ValidateRelPath(t *testing.T) {
	fs := &RealFS{}

	tests := []struct {
		name      string
		path      string
		wantError bool
	}{
		{
			name:      "valid relative path",
			path:      "foo/bar/baz.txt",
			wantError: false,
		},
		{
			name:      "valid single file",
			path:      "file.txt",
			wantError: false,
		},
		{
			name:      "empty path",
			path:      "",
			wantError: true,
		},
		{
			name:      "current directory",
			path:      ".",
			wantError: true,
		},
		{
			name:      "absolute path",
			path:      "/etc/hosts",
			wantError: true,
		},
		{
			name:      "parent directory traversal",
			path:      "../etc/hosts",
			wantError: true,
		},
		{
			name:      "traversal in middle",
			path:      "foo/../../../etc/hosts",
			wantError: true,
		},
		{
			name:      "path with dot prefix",
			path:      ".hidden/file.txt",
			wantError: false,
		},
		{
			name:      "deeply nested path",
			path:      "a/b/c/d/e/f/g.txt",
			wantError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := fs.ValidateRelPath(tt.path)
			if (err != nil) != tt.wantError {
				t.Errorf("ValidateRelPath(%q) error = %v, wantError %v", tt.path, err, tt.wantError)
			}
		})
	}
}

func TestValidatePathOutsideGitDir(t *testing.T) {
	tests := []struct {
		name      string
		target    string
		wantError bool
	}{
		{
			name:      "repository file",
			target:    "/repo/.github/workflows/test.yml",
			wantError: false,
		},
		{
			name:      "git directory itself",
			target:    "/repo/.git",
			wantError: true,
		},
		{
			name:      "git hook",
			target:    "/repo/.git/hooks/pre-commit",
			wantError: true,
		},
		{
			name:      "similarly named directory",
			target:    "/repo/.git-hooks/pre-commit",
			wantError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidatePathOutsideGitDir("/repo", tt.target)
			if (err != nil) != tt.wantError {
				t.Errorf("ValidatePathOutsideGitDir(%q) error = %v, wantError %v", tt.target, err, tt.wantError)
			}
		})
	}
}

func TestRealFS_ValidateIdentifier(t *testing.T) {
	fs := &RealFS{}

	tests := []struct {
		name      string
		id        string
		wantError bool
	}{
		{
			name:      "valid simple identifier",
			id:        "my-store",
			wantError: false,
		},
		{
			name:      "valid with underscores",
			id:        "my_store_123",
			wantError: false,
		},
		{
			name:      "valid alphanumeric",
			id:        "store123",
			wantError: false,
		},
		{
			name:      "empty identifier",
			id:        "",
			wantError: true,
		},
		{
			name:      "current directory",
			id:        ".",
			wantError: true,
		},
		{
			name:      "parent directory",
			id:        "..",
			wantError: true,
		},
		{
			name:      "path with separator",
			id:        "store/subdir",
			wantError: true,
		},
		{
			name:      "path with backslash",
			id:        "store\\subdir",
			wantError: true,
		},
		{
			name:      "absolute path",
			id:        "/etc/hosts",
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := fs.ValidateIdentifier(tt.id)
			if (err != nil) != tt.wantError {
				t.Errorf("ValidateIdentifier(%q) error = %v, wantError %v", tt.id, err, tt.wantError)
			}
		})
	}
}

func TestRealFS_Exists(t *testing.T) {
	fs := &RealFS{}

	tmpDir, err := os.MkdirTemp("", "fsops-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer func() {
		if err := os.RemoveAll(tmpDir); err != nil {
			t.Errorf("failed to remove temp dir: %v", err)
		}
	}()

	t.Run("existing file", func(t *testing.T) {
		testFile := filepath.Join(tmpDir, "exists.txt")
		if err := os.WriteFile(testFile, []byte("test"), 0644); err != nil {
			t.Fatalf("failed to create test file: %v", err)
		}

		exists, err := fs.Exists(testFile)
		if err != nil {
			t.Errorf("Exists returned error: %v", err)
		}
		if !exists {
			t.Error("Exists should return true for existing file")
		}
	})

	t.Run("non-existing file", func(t *testing.T) {
		nonExistent := filepath.Join(tmpDir, "does-not-exist.txt")
		exists, err := fs.Exists(nonExistent)
		if err != nil {
			t.Errorf("Exists returned error: %v", err)
		}
		if exists {
			t.Error("Exists should return false for non-existing file")
		}
	})

	t.Run("existing directory", func(t *testing.T) {
		exists, err := fs.Exists(tmpDir)
		if err != nil {
			t.Errorf("Exists returned error: %v", err)
		}
		if !exists {
			t.Error("Exists should return true for existing directory")
		}
	})
}

func TestRealFS_MkdirAll(t *testing.T) {
	fs := &RealFS{}

	tmpDir, err := os.MkdirTemp("", "fsops-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer func() {
		if err := os.RemoveAll(tmpDir); err != nil {
			t.Errorf("failed to remove temp dir: %v", err)
		}
	}()

	t.Run("create nested directories", func(t *testing.T) {
		nestedPath := filepath.Join(tmpDir, "a", "b", "c")
		err := fs.MkdirAll(nestedPath, 0755)
		if err != nil {
			t.Fatalf("MkdirAll failed: %v", err)
		}

		// Verify directory exists
		if _, err := os.Stat(nestedPath); os.IsNotExist(err) {
			t.Error("Nested directory was not created")
		}
	})

	t.Run("idempotent operation", func(t *testing.T) {
		dirPath := filepath.Join(tmpDir, "existing")

		// Create once
		if err := fs.MkdirAll(dirPath, 0755); err != nil {
			t.Fatalf("First MkdirAll failed: %v", err)
		}

		// Create again - should not fail
		if err := fs.MkdirAll(dirPath, 0755); err != nil {
			t.Errorf("Second MkdirAll should not fail: %v", err)
		}
	})
}

func TestRealFS_AtomicWrite(t *testing.T) {
	fs := &RealFS{}

	tmpDir, err := os.MkdirTemp("", "fsops-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer func() {
		if err := os.RemoveAll(tmpDir); err != nil {
			t.Errorf("failed to remove temp dir: %v", err)
		}
	}()

	t.Run("write to new file", func(t *testing.T) {
		testFile := filepath.Join(tmpDir, "atomic-new.txt")
		content := []byte("atomic content")

		err := fs.AtomicWrite(testFile, content, 0644)
		if err != nil {
			t.Fatalf("AtomicWrite failed: %v", err)
		}

		// Verify file exists and has correct content
		readContent, err := os.ReadFile(testFile)
		if err != nil {
			t.Fatalf("failed to read written file: %v", err)
		}
		if string(readContent) != string(content) {
			t.Errorf("File content mismatch: got %q, want %q", readContent, content)
		}
	})

	t.Run("overwrite existing file", func(t *testing.T) {
		testFile := filepath.Join(tmpDir, "atomic-overwrite.txt")

		// Write initial content
		initialContent := []byte("initial")
		if err := os.WriteFile(testFile, initialContent, 0644); err != nil {
			t.Fatalf("failed to create initial file: %v", err)
		}

		// Overwrite with atomic write
		newContent := []byte("overwritten")
		err := fs.AtomicWrite(testFile, newContent, 0644)
		if err != nil {
			t.Fatalf("AtomicWrite failed: %v", err)
		}

		// Verify new content
		readContent, err := os.ReadFile(testFile)
		if err != nil {
			t.Fatalf("failed to read file: %v", err)
		}
		if string(readContent) != string(newContent) {
			t.Errorf("File content not updated: got %q, want %q", readContent, newContent)
		}
	})
}

func TestRealFS_ReadFile(t *testing.T) {
	fs := &RealFS{}

	tmpDir, err := os.MkdirTemp("", "fsops-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer func() {
		if err := os.RemoveAll(tmpDir); err != nil {
			t.Errorf("failed to remove temp dir: %v", err)
		}
	}()

	t.Run("read existing file", func(t *testing.T) {
		testFile := filepath.Join(tmpDir, "read-test.txt")
		content := []byte("test content")
		if err := os.WriteFile(testFile, content, 0644); err != nil {
			t.Fatalf("failed to create test file: %v", err)
		}

		readContent, err := fs.ReadFile(testFile)
		if err != nil {
			t.Fatalf("ReadFile failed: %v", err)
		}
		if string(readContent) != string(content) {
			t.Errorf("ReadFile content mismatch: got %q, want %q", readContent, content)
		}
	})

	t.Run("read non-existing file", func(t *testing.T) {
		nonExistent := filepath.Join(tmpDir, "does-not-exist.txt")
		_, err := fs.ReadFile(nonExistent)
		if err == nil {
			t.Error("ReadFile should return error for non-existing file")
		}
	})
}

func TestRealFS_Remove(t *testing.T) {
	fs := &RealFS{}

	tmpDir, err := os.MkdirTemp("", "fsops-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer func() {
		if err := os.RemoveAll(tmpDir); err != nil {
			t.Errorf("failed to remove temp dir: %v", err)
		}
	}()

	t.Run("remove existing file", func(t *testing.T) {
		testFile := filepath.Join(tmpDir, "remove-me.txt")
		if err := os.WriteFile(testFile, []byte("test"), 0644); err != nil {
			t.Fatalf("failed to create test file: %v", err)
		}

		err := fs.Remove(testFile)
		if err != nil {
			t.Fatalf("Remove failed: %v", err)
		}

		// Verify file is gone
		if _, err := os.Stat(testFile); !os.IsNotExist(err) {
			t.Error("File should have been removed")
		}
	})
}

type failAfterReader struct {
	data []byte
	off  int
}

func (r *failAfterReader) Read(p []byte) (int, error) {
	if r.off >= len(r.data) {
		return 0, errors.New("injected copy failure")
	}
	n := copy(p, r.data[r.off:])
	r.off += n
	return n, errors.New("injected copy failure")
}

func TestWriteFileAtomically_FailureDoesNotTruncateDestination(t *testing.T) {
	dst := filepath.Join(t.TempDir(), "dest.txt")
	original := "original-destination-bytes"
	if err := os.WriteFile(dst, []byte(original), 0600); err != nil {
		t.Fatalf("failed to write destination: %v", err)
	}

	err := writeFileAtomically(dst, &failAfterReader{data: []byte("partial-new-content")}, 0600)
	if err == nil {
		t.Fatal("writeFileAtomically succeeded, want injected copy failure")
	}
	if !strings.Contains(err.Error(), "injected copy failure") {
		t.Fatalf("error = %v, want injected copy failure", err)
	}

	got, readErr := os.ReadFile(dst)
	if readErr != nil {
		t.Fatalf("failed to read destination after failed copy: %v", readErr)
	}
	if string(got) != original {
		t.Fatalf("destination content = %q, want original %q", got, original)
	}
}

func TestRealFS_Copy(t *testing.T) {
	fs := &RealFS{}

	t.Run("copies regular directory contents", func(t *testing.T) {
		tmpDir := t.TempDir()
		src := filepath.Join(tmpDir, "src")
		dst := filepath.Join(tmpDir, "dst")

		if err := os.MkdirAll(filepath.Join(src, "nested"), 0755); err != nil {
			t.Fatalf("failed to create source directory: %v", err)
		}
		if err := os.WriteFile(filepath.Join(src, "nested", "file.txt"), []byte("regular content"), 0644); err != nil {
			t.Fatalf("failed to write source file: %v", err)
		}

		if err := fs.Copy(src, dst); err != nil {
			t.Fatalf("Copy failed: %v", err)
		}

		content, err := os.ReadFile(filepath.Join(dst, "nested", "file.txt"))
		if err != nil {
			t.Fatalf("failed to read copied file: %v", err)
		}
		if string(content) != "regular content" {
			t.Fatalf("copied content = %q, want %q", content, "regular content")
		}
	})

	t.Run("rejects symlink without copying target contents", func(t *testing.T) {
		tmpDir := t.TempDir()
		src := filepath.Join(tmpDir, "src")
		dst := filepath.Join(tmpDir, "dst")
		outside := filepath.Join(tmpDir, "outside-secret.txt")

		if err := os.MkdirAll(filepath.Join(src, "nested"), 0755); err != nil {
			t.Fatalf("failed to create source directory: %v", err)
		}
		if err := os.WriteFile(filepath.Join(src, "nested", "safe.txt"), []byte("safe content"), 0644); err != nil {
			t.Fatalf("failed to write safe source file: %v", err)
		}
		if err := os.WriteFile(outside, []byte("do-not-copy"), 0644); err != nil {
			t.Fatalf("failed to write outside file: %v", err)
		}
		requireSymlink(t, outside, filepath.Join(src, "nested", "leak.txt"))

		err := fs.Copy(src, dst)
		if err == nil {
			t.Fatal("Copy succeeded, want symlink rejection")
		}
		if !strings.Contains(err.Error(), "nested/leak.txt") || !strings.Contains(err.Error(), "symlink") {
			t.Fatalf("Copy error %q should name the offending symlink path", err)
		}

		if _, err := os.Lstat(dst); !os.IsNotExist(err) {
			t.Fatalf("destination should not be created after symlink rejection, stat error: %v", err)
		}
	})
}

func TestRealFS_CopyExceptReplacesDirectoryWithoutExcludedDescendants(t *testing.T) {
	fs := &RealFS{}
	tmpDir := t.TempDir()
	src := filepath.Join(tmpDir, "src")
	dst := filepath.Join(tmpDir, "dst")
	if err := os.MkdirAll(filepath.Join(src, "nested"), 0755); err != nil {
		t.Fatalf("create source directory: %v", err)
	}
	if err := os.WriteFile(filepath.Join(src, "kept.txt"), []byte("kept"), 0644); err != nil {
		t.Fatalf("write kept source: %v", err)
	}
	if err := os.WriteFile(filepath.Join(src, "nested", "ignored.log"), []byte("ignored"), 0644); err != nil {
		t.Fatalf("write ignored source: %v", err)
	}
	if err := os.MkdirAll(dst, 0755); err != nil {
		t.Fatalf("create destination directory: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dst, "stale.txt"), []byte("stale"), 0644); err != nil {
		t.Fatalf("write stale destination: %v", err)
	}

	if err := fs.CopyExcept(src, dst, map[string]bool{filepath.Join("nested", "ignored.log"): true}); err != nil {
		t.Fatalf("CopyExcept failed: %v", err)
	}
	if got, err := os.ReadFile(filepath.Join(dst, "kept.txt")); err != nil || string(got) != "kept" {
		t.Fatalf("kept snapshot = %q, %v", got, err)
	}
	if _, err := os.Stat(filepath.Join(dst, "nested", "ignored.log")); !os.IsNotExist(err) {
		t.Fatalf("excluded descendant persisted, stat error = %v", err)
	}
	if _, err := os.Stat(filepath.Join(dst, "stale.txt")); !os.IsNotExist(err) {
		t.Fatalf("stale destination content survived replacement, stat error = %v", err)
	}
}

func TestRealFS_CopyWithinRoot_AllowsNestedDestinationAndReplacesFinalSymlink(t *testing.T) {
	fs := NewRealFS()
	root := t.TempDir()
	sourceDir := t.TempDir()
	source := filepath.Join(sourceDir, "source.txt")
	if err := os.WriteFile(source, []byte("replacement"), 0640); err != nil {
		t.Fatalf("failed to write source: %v", err)
	}

	outside := filepath.Join(t.TempDir(), "outside.txt")
	if err := os.WriteFile(outside, []byte("preserve"), 0600); err != nil {
		t.Fatalf("failed to write outside file: %v", err)
	}
	finalPath := filepath.Join(root, "nested", "path", "file.txt")
	if err := os.MkdirAll(filepath.Dir(finalPath), 0700); err != nil {
		t.Fatalf("failed to create destination parent: %v", err)
	}
	requireSymlink(t, outside, finalPath)

	if err := fs.CopyWithinRoot(root, "nested/path/file.txt", source); err != nil {
		t.Fatalf("CopyWithinRoot failed: %v", err)
	}
	info, err := os.Lstat(finalPath)
	if err != nil {
		t.Fatalf("failed to inspect copied file: %v", err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		t.Fatal("final-path symlink was not replaced")
	}
	content, err := os.ReadFile(finalPath)
	if err != nil || string(content) != "replacement" {
		t.Fatalf("copied content = %q, error = %v", content, err)
	}
	outsideContent, err := os.ReadFile(outside)
	if err != nil || string(outsideContent) != "preserve" {
		t.Fatalf("outside file content = %q, error = %v; want preserved", outsideContent, err)
	}
}

func TestRealFS_RootConfinedFinalPathOperationsDoNotFollowSymlinks(t *testing.T) {
	fs := NewRealFS()
	root := t.TempDir()
	outside := filepath.Join(t.TempDir(), "outside.txt")
	if err := os.WriteFile(outside, []byte("preserve"), 0600); err != nil {
		t.Fatalf("failed to write outside file: %v", err)
	}
	link := filepath.Join(root, "replace-me")
	requireSymlink(t, outside, link)

	if err := fs.RemoveAllWithinRoot(root, "replace-me"); err != nil {
		t.Fatalf("RemoveAllWithinRoot failed: %v", err)
	}
	if _, err := os.Lstat(link); !os.IsNotExist(err) {
		t.Fatalf("final symlink still exists, lstat error: %v", err)
	}
	content, err := os.ReadFile(outside)
	if err != nil || string(content) != "preserve" {
		t.Fatalf("outside target content = %q, error = %v; want preserved", content, err)
	}

	if err := fs.SymlinkWithinRoot(root, "nested/link", outside); err != nil {
		t.Fatalf("SymlinkWithinRoot failed for nested destination: %v", err)
	}
	target, err := os.Readlink(filepath.Join(root, "nested", "link"))
	if err != nil || target != outside {
		t.Fatalf("created symlink target = %q, error = %v; want %q", target, err, outside)
	}
}

func TestRealFS_RestoreTreeWithinRoot_RecreatesNestedSymlinksAndReplacesLeafLink(t *testing.T) {
	fs := NewRealFS()
	root := t.TempDir()
	outside := t.TempDir()
	writeCopyFixture(t, filepath.Join(outside, "target.txt"), "outside sentinel")
	backup := filepath.Join(t.TempDir(), "backup", "dir")
	writeCopyFixture(t, filepath.Join(backup, "file.txt"), "original file")
	writeCopyFixture(t, filepath.Join(backup, "sub", "deep.txt"), "original deep")
	requireSymlink(t, "../file.txt", filepath.Join(backup, "sub", "link"))
	requireSymlink(t, filepath.Join(outside, "target.txt"), filepath.Join(backup, "abs-link"))

	// The leaf is a symlink to outside: restore must replace it, not write
	// through it.
	if err := os.MkdirAll(filepath.Join(root, "nested"), 0700); err != nil {
		t.Fatal(err)
	}
	requireSymlink(t, outside, filepath.Join(root, "nested", "dir"))
	before := directorySnapshot(t, outside)

	if err := fs.RestoreTreeWithinRoot(root, "nested/dir", backup, "txnA"); err != nil {
		t.Fatalf("RestoreTreeWithinRoot failed: %v", err)
	}
	if got, want := directorySnapshot(t, filepath.Join(root, "nested", "dir")), directorySnapshot(t, backup); got != want {
		t.Fatalf("restored tree:\n%s\nwant:\n%s", got, want)
	}
	if after := directorySnapshot(t, outside); after != before {
		t.Fatalf("outside tree changed:\n%s", after)
	}
	assertNoStagingEntries(t, root)

	if err := fs.RestoreTreeWithinRoot(root, "file.txt", filepath.Join(backup, "file.txt"), ""); err != nil {
		t.Fatalf("RestoreTreeWithinRoot(file) failed: %v", err)
	}
	requireFixtureContent(t, filepath.Join(root, "file.txt"), "original file")
	if err := fs.RestoreTreeWithinRoot(root, "top-link", filepath.Join(backup, "sub", "link"), ""); err != nil {
		t.Fatalf("RestoreTreeWithinRoot(symlink) failed: %v", err)
	}
	if target, err := os.Readlink(filepath.Join(root, "top-link")); err != nil || target != "../file.txt" {
		t.Fatalf("restored symlink target = %q, error = %v", target, err)
	}
	assertNoStagingEntries(t, root)
}

func TestRealFS_RestoreTreeWithinRoot_RefusesReplacedAncestor(t *testing.T) {
	fs := NewRealFS()
	backup := filepath.Join(t.TempDir(), "backup")
	writeCopyFixture(t, filepath.Join(backup, "file.txt"), "original file")
	writeCopyFixture(t, filepath.Join(backup, "dir", "a.txt"), "original a")
	requireSymlink(t, "a.txt", filepath.Join(backup, "dir", "link"))

	for _, tc := range []struct{ name, relPath, src string }{
		{"file", "nested/file.txt", filepath.Join(backup, "file.txt")},
		{"directory", "nested/dir", filepath.Join(backup, "dir")},
		{"symlink", "nested/link", filepath.Join(backup, "dir", "link")},
		{"deep", "nested/deeper/dir", filepath.Join(backup, "dir")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			outside := t.TempDir()
			writeCopyFixture(t, filepath.Join(outside, "unrelated.txt"), "outside sentinel")
			writeCopyFixture(t, filepath.Join(outside, "deeper", "unrelated.txt"), "outside sentinel")
			requireSymlink(t, outside, filepath.Join(root, "nested"))
			before := directorySnapshot(t, outside)

			err := fs.RestoreTreeWithinRoot(root, tc.relPath, tc.src, "txnA")
			if err == nil || !strings.Contains(err.Error(), "symlinked destination ancestor") {
				t.Fatalf("RestoreTreeWithinRoot error = %v, want symlinked ancestor refusal", err)
			}
			if after := directorySnapshot(t, outside); after != before {
				t.Fatalf("outside tree changed:\n%s", after)
			}
		})
	}

	root := t.TempDir()
	if err := fs.RestoreTreeWithinRoot(root, ".git/config", filepath.Join(backup, "file.txt"), ""); err == nil {
		t.Fatal("RestoreTreeWithinRoot into .git succeeded, want refusal")
	}
	if err := fs.RestoreTreeWithinRoot(root, "file.txt", filepath.Join(backup, "file.txt"), "a/b"); err == nil {
		t.Fatal("RestoreTreeWithinRoot accepted an invalid owner")
	}
}

func TestRealFS_DirectoryCopyRejectsNestedDestination(t *testing.T) {
	fs := NewRealFS()
	for _, method := range []string{"Copy", "CopyExcept", "CopyWithinRoot"} {
		t.Run(method+"/missing-parent", func(t *testing.T) {
			base := t.TempDir()
			src := filepath.Join(base, "src")
			writeCopyFixture(t, filepath.Join(src, "nested", "file.txt"), "original")
			before := directorySnapshot(t, base)

			err := copyDirectoryMethod(fs, method, src, filepath.Join(src, "backup", "snapshot"), base)
			if err == nil || !strings.Contains(err.Error(), "descendant") {
				t.Fatalf("error = %v, want descendant rejection", err)
			}
			if after := directorySnapshot(t, base); after != before {
				t.Fatalf("tree changed:\n%s", after)
			}
		})

		t.Run(method+"/existing-destination", func(t *testing.T) {
			base := t.TempDir()
			src := filepath.Join(base, "src")
			writeCopyFixture(t, filepath.Join(src, "nested", "file.txt"), "original")
			writeCopyFixture(t, filepath.Join(src, "backup", "keep.txt"), "keep")
			before := directorySnapshot(t, base)

			err := copyDirectoryMethod(fs, method, src, filepath.Join(src, "backup", "snapshot"), base)
			if err == nil || !strings.Contains(err.Error(), "descendant") {
				t.Fatalf("error = %v, want descendant rejection", err)
			}
			if after := directorySnapshot(t, base); after != before {
				t.Fatalf("source or destination changed:\n%s", after)
			}
		})

		t.Run(method+"/symlink-ancestor", func(t *testing.T) {
			base := t.TempDir()
			src := filepath.Join(base, "src")
			writeCopyFixture(t, filepath.Join(src, "nested", "file.txt"), "original")
			requireSymlink(t, src, filepath.Join(base, "alias"))
			before := directorySnapshot(t, base)

			err := copyDirectoryMethod(fs, method, src, filepath.Join(base, "alias", "backup", "snapshot"), base)
			if err == nil || !strings.Contains(err.Error(), "descendant") {
				t.Fatalf("error = %v, want descendant rejection", err)
			}
			if after := directorySnapshot(t, base); after != before {
				t.Fatalf("aliased destination mutated the tree:\n%s", after)
			}
		})

		t.Run(method+"/disjoint", func(t *testing.T) {
			base := t.TempDir()
			src := filepath.Join(base, "src")
			writeCopyFixture(t, filepath.Join(src, "nested", "file.txt"), "original")

			if err := copyDirectoryMethod(fs, method, src, filepath.Join(base, "dst"), base); err != nil {
				t.Fatalf("disjoint copy failed: %v", err)
			}
			got, err := os.ReadFile(filepath.Join(base, "dst", "nested", "file.txt"))
			if err != nil || string(got) != "original" {
				t.Fatalf("copied content = %q, error = %v", got, err)
			}
			if got, err = os.ReadFile(filepath.Join(src, "nested", "file.txt")); err != nil || string(got) != "original" {
				t.Fatalf("source content = %q, error = %v", got, err)
			}
			assertNoStagingEntries(t, base)
		})

		t.Run(method+"/same-path", func(t *testing.T) {
			base := t.TempDir()
			src := filepath.Join(base, "src")
			writeCopyFixture(t, filepath.Join(src, "nested", "file.txt"), "original")
			before := directorySnapshot(t, src)

			if err := copyDirectoryMethod(fs, method, src, src, base); err != nil {
				t.Fatalf("same-path replacement failed: %v", err)
			}
			if after := directorySnapshot(t, src); after != before {
				t.Fatalf("same-path replacement changed source:\n%s", after)
			}
			assertNoStagingEntries(t, base)
		})

		t.Run(method+"/case-alias", func(t *testing.T) {
			base := t.TempDir()
			src := filepath.Join(base, "Source")
			writeCopyFixture(t, filepath.Join(src, "nested", "file.txt"), "original")
			folded := filepath.Join(base, "source")
			srcInfo, err := os.Stat(src)
			foldedInfo, foldedErr := os.Stat(folded)
			if foldedErr != nil || err != nil || !os.SameFile(srcInfo, foldedInfo) {
				t.Skip("filesystem preserves directory case")
			}
			before := directorySnapshot(t, base)

			err = copyDirectoryMethod(fs, method, src, filepath.Join(folded, "backup"), base)
			if err == nil || !strings.Contains(err.Error(), "descendant") {
				t.Fatalf("error = %v, want descendant rejection", err)
			}
			if after := directorySnapshot(t, base); after != before {
				t.Fatalf("case alias mutated the tree:\n%s", after)
			}
			if err := copyDirectoryMethod(fs, method, src, folded, base); err != nil {
				t.Fatalf("case-alias same-path replacement failed: %v", err)
			}
			got, readErr := os.ReadFile(filepath.Join(src, "nested", "file.txt"))
			if readErr != nil || string(got) != "original" {
				t.Fatalf("case-alias same-path content = %q, error = %v", got, readErr)
			}
			assertNoStagingEntries(t, base)
		})
	}

	t.Run("platform-alias", func(t *testing.T) {
		if runtime.GOOS != "darwin" {
			t.Skip("platform alias fixture is macOS /private")
		}
		realBase, err := filepath.EvalSymlinks(t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		if !strings.HasPrefix(realBase, "/private/") {
			t.Skip("temp dir is not under a /private alias")
		}
		aliasBase := strings.TrimPrefix(realBase, "/private")
		src := filepath.Join(realBase, "src")
		writeCopyFixture(t, filepath.Join(src, "nested", "file.txt"), "original")
		aliasedSrc := filepath.Join(aliasBase, "src")

		before := directorySnapshot(t, realBase)
		err = fs.Copy(src, filepath.Join(aliasedSrc, "backup", "snapshot"))
		if err == nil || !strings.Contains(err.Error(), "descendant") {
			t.Fatalf("Copy through /var alias error = %v", err)
		}
		err = fs.CopyExcept(aliasedSrc, filepath.Join(src, "backup", "snapshot"), map[string]bool{"backup": true})
		if err == nil || !strings.Contains(err.Error(), "descendant") {
			t.Fatalf("CopyExcept through /private alias error = %v", err)
		}
		err = fs.CopyWithinRoot(aliasBase, filepath.Join("src", "backup", "snapshot"), src)
		if err == nil || !strings.Contains(err.Error(), "descendant") {
			t.Fatalf("CopyWithinRoot through /var alias error = %v", err)
		}
		if after := directorySnapshot(t, realBase); after != before {
			t.Fatalf("platform alias copy mutated the tree:\n%s", after)
		}

		if err := fs.Copy(src, aliasedSrc); err != nil {
			t.Fatalf("same-path replacement through platform alias failed: %v", err)
		}
		got, readErr := os.ReadFile(filepath.Join(src, "nested", "file.txt"))
		if readErr != nil || string(got) != "original" {
			t.Fatalf("alias same-path content = %q, error = %v", got, readErr)
		}
		assertNoStagingEntries(t, realBase)
	})
}

func copyDirectoryMethod(fs *RealFS, method, src, dst, root string) error {
	switch method {
	case "Copy":
		return fs.Copy(src, dst)
	case "CopyExcept":
		// Excluding the nested destination must not bypass the guard.
		rel, err := filepath.Rel(src, dst)
		excluded := map[string]bool{"ignored": true}
		if err == nil && rel != "." && !strings.HasPrefix(rel, "..") {
			excluded[rel] = true
			if top, _, ok := strings.Cut(rel, string(filepath.Separator)); ok {
				excluded[top] = true
			}
		}
		return fs.CopyExcept(src, dst, excluded)
	case "CopyWithinRoot":
		rel, err := filepath.Rel(root, dst)
		if err != nil {
			return err
		}
		return fs.CopyWithinRoot(root, rel, src)
	default:
		return errors.New("unknown copy method " + method)
	}
}

func writeCopyFixture(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatalf("failed to create parent for %s: %v", path, err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write %s: %v", path, err)
	}
}

func directorySnapshot(t *testing.T, root string) string {
	t.Helper()
	var lines []string
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		switch {
		case entry.Type()&os.ModeSymlink != 0:
			target, err := os.Readlink(path)
			if err != nil {
				return err
			}
			lines = append(lines, rel+" -> "+target)
		case entry.IsDir():
			lines = append(lines, rel+"/")
		default:
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			lines = append(lines, rel+"="+string(data))
		}
		return nil
	})
	if err != nil {
		t.Fatalf("snapshot %s: %v", root, err)
	}
	sort.Strings(lines)
	return strings.Join(lines, "\n")
}

func assertNoStagingEntries(t *testing.T, root string) {
	t.Helper()
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if strings.HasPrefix(entry.Name(), ".monodev-") {
			t.Fatalf("staging entry created: %s", path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func writeOwnedTempFixture(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatalf("failed to create fixture parent: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatalf("failed to write fixture: %v", err)
	}
}

func requireFixtureContent(t *testing.T, path, want string) {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil || string(got) != want {
		t.Fatalf("%s content = %q, error = %v; want %q", path, got, err, want)
	}
}

func TestRealFS_RemoveOwnedTempsWithinRoot_RemovesOnlyOwnedTemps(t *testing.T) {
	fs := NewRealFS()
	root := t.TempDir()
	for _, dirRel := range []string{".", "nested"} {
		dir := filepath.Join(root, dirRel)
		owned := []string{
			filepath.Join(dir, ".monodev-copy-txnA-1-2"),
			filepath.Join(dir, ".monodev-aside-txnA-1-3", "child.txt"),
		}
		kept := []string{
			filepath.Join(dir, ".monodev-copy-user-notes"),
			filepath.Join(dir, ".monodev-aside-keep", "child.txt"),
			filepath.Join(dir, ".monodev-copy-txnB-1-2"),
			filepath.Join(dir, ".monodev-copy-txnAB-1-2"),
			filepath.Join(dir, ".monodev-copy-123-456"),
			filepath.Join(dir, "regular.txt"),
		}
		for _, path := range append(append([]string{}, owned...), kept...) {
			writeOwnedTempFixture(t, path, "fixture")
		}

		if err := fs.RemoveOwnedTempsWithinRoot(root, dirRel, "txnA"); err != nil {
			t.Fatalf("RemoveOwnedTempsWithinRoot(%q) failed: %v", dirRel, err)
		}
		for _, path := range []string{owned[0], filepath.Dir(owned[1])} {
			if _, err := os.Lstat(path); !os.IsNotExist(err) {
				t.Fatalf("owned temp %s survived, lstat error: %v", path, err)
			}
		}
		for _, path := range kept {
			requireFixtureContent(t, path, "fixture")
		}
	}

	if err := fs.RemoveOwnedTempsWithinRoot(root, "missing/dir", "txnA"); err != nil {
		t.Fatalf("missing directory error = %v, want nil", err)
	}
	unrelated := filepath.Join(root, ".monodev-copy-a")
	writeOwnedTempFixture(t, unrelated, "fixture")
	for _, owner := range []string{"", "a-b", "../a", "a/b"} {
		if err := fs.RemoveOwnedTempsWithinRoot(root, ".", owner); err == nil {
			t.Fatalf("owner %q accepted, want refusal", owner)
		}
		if err := fs.CopyWithinRootOwned(root, "file.txt", unrelated, owner); err == nil {
			t.Fatalf("CopyWithinRootOwned owner %q accepted, want refusal", owner)
		}
	}
	requireFixtureContent(t, unrelated, "fixture")
}

func TestRealFS_RemoveOwnedTempsWithinRoot_RefusesSymlinkedAncestor(t *testing.T) {
	fs := NewRealFS()
	root := t.TempDir()
	outside := t.TempDir()
	sentinels := []string{
		filepath.Join(outside, ".monodev-copy-txnA-1-2"),
		filepath.Join(outside, ".monodev-copy-user-notes"),
		filepath.Join(outside, "deeper", ".monodev-aside-txnA-1-3"),
		filepath.Join(outside, "deeper", ".monodev-copy-user-notes"),
	}
	for _, path := range sentinels {
		writeOwnedTempFixture(t, path, "outside sentinel")
	}
	requireSymlink(t, outside, filepath.Join(root, "nested"))

	for _, dirRel := range []string{"nested", "nested/deeper"} {
		err := fs.RemoveOwnedTempsWithinRoot(root, dirRel, "txnA")
		if err == nil || !strings.Contains(err.Error(), "symlinked destination ancestor") {
			t.Fatalf("RemoveOwnedTempsWithinRoot(%q) error = %v, want symlinked ancestor refusal", dirRel, err)
		}
	}
	for _, path := range sentinels {
		requireFixtureContent(t, path, "outside sentinel")
	}
}

func TestRealFS_CopyWithinRootOwned_TagsLeftoverTempsForSweep(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root bypasses the directory permissions that force a leftover temp")
	}
	fs := NewRealFS()
	root := t.TempDir()
	source := filepath.Join(t.TempDir(), "source")
	writeOwnedTempFixture(t, filepath.Join(source, "new.txt"), "replacement")
	locked := filepath.Join(root, "dest", "locked")
	writeOwnedTempFixture(t, filepath.Join(locked, "file.txt"), "original")
	unrelated := filepath.Join(root, ".monodev-aside-user-notes")
	writeOwnedTempFixture(t, unrelated, "keep")
	if err := os.Chmod(locked, 0500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = filepath.Walk(root, func(path string, _ os.FileInfo, _ error) error { return os.Chmod(path, 0700) })
	})

	// The replaced destination cannot be removed, so its moved-aside copy is
	// left behind the way an interrupted process would leave it.
	if err := fs.CopyWithinRootOwned(root, "dest", source, "txnA"); err == nil {
		t.Fatal("CopyWithinRootOwned error = nil, want aside removal failure")
	}
	requireFixtureContent(t, filepath.Join(root, "dest", "new.txt"), "replacement")
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	var leftover string
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".monodev-aside-txnA-") {
			leftover = filepath.Join(root, entry.Name())
		}
	}
	if leftover == "" {
		t.Fatalf("no owner-tagged aside left in %v", entries)
	}
	if err := os.Chmod(filepath.Join(leftover, "locked"), 0700); err != nil {
		t.Fatal(err)
	}

	if err := fs.RemoveOwnedTempsWithinRoot(root, ".", "txnA"); err != nil {
		t.Fatalf("RemoveOwnedTempsWithinRoot failed: %v", err)
	}
	if _, err := os.Lstat(leftover); !os.IsNotExist(err) {
		t.Fatalf("owned leftover survived, lstat error: %v", err)
	}
	requireFixtureContent(t, unrelated, "keep")
	requireFixtureContent(t, filepath.Join(root, "dest", "new.txt"), "replacement")
}

func requireSymlink(t *testing.T, oldname, newname string) {
	t.Helper()

	if err := os.Symlink(oldname, newname); err != nil {
		t.Skipf("symlink creation is not supported in this environment: %v", err)
	}
}

// gitAliasFixture builds a repository root with a real .git/hooks sentinel and
// reports whether the filesystem resolves ".GIT" to the same directory.
func gitAliasFixture(t *testing.T) (root, sentinel string, caseInsensitive bool) {
	t.Helper()
	root = t.TempDir()
	hooks := filepath.Join(root, ".git", "hooks")
	if err := os.MkdirAll(hooks, 0700); err != nil {
		t.Fatalf("failed to create hooks dir: %v", err)
	}
	sentinel = filepath.Join(hooks, "pre-commit")
	if err := os.WriteFile(sentinel, []byte("original"), 0700); err != nil {
		t.Fatalf("failed to write sentinel: %v", err)
	}
	_, err := os.Stat(filepath.Join(root, ".GIT", "hooks", "pre-commit"))
	return root, sentinel, err == nil
}

func requireSentinelUnchanged(t *testing.T, root, sentinel string) {
	t.Helper()
	content, err := os.ReadFile(sentinel)
	if err != nil || string(content) != "original" {
		t.Fatalf("sentinel content = %q, error = %v; want unchanged", content, err)
	}
	entries, err := os.ReadDir(filepath.Join(root, ".git", "hooks"))
	if err != nil || len(entries) != 1 {
		t.Fatalf("hooks entries = %v, error = %v; want only the sentinel", entries, err)
	}
}

func TestValidatePathOutsideGitDir_CaseAliases(t *testing.T) {
	root, _, caseInsensitive := gitAliasFixture(t)

	for _, alias := range []string{".GIT", ".Git", ".gIt"} {
		err := ValidatePathOutsideGitDir(root, filepath.Join(root, alias, "hooks", "sentinel"))
		if caseInsensitive && err == nil {
			t.Errorf("alias %q was not rejected on a case-insensitive filesystem", alias)
		}
		if !caseInsensitive && err != nil {
			t.Errorf("alias %q rejected on a case-sensitive filesystem: %v", alias, err)
		}
	}
	for _, ordinary := range []string{".github/workflows/ci.yml", ".git-hooks/pre-commit", "sub/.GIT/x"} {
		if err := ValidatePathOutsideGitDir(root, filepath.Join(root, ordinary)); err != nil {
			t.Errorf("ordinary path %q rejected: %v", ordinary, err)
		}
	}
}

func TestRealFS_RootConfinedPrimitivesRejectGitCaseAliases(t *testing.T) {
	root, sentinel, caseInsensitive := gitAliasFixture(t)
	if !caseInsensitive {
		t.Skip("filesystem is case-sensitive; .GIT is not an alias of .git")
	}
	fs := NewRealFS()
	source := filepath.Join(t.TempDir(), "source.txt")
	if err := os.WriteFile(source, []byte("malicious"), 0700); err != nil {
		t.Fatalf("failed to write source: %v", err)
	}

	for _, alias := range []string{".GIT", ".Git"} {
		for _, rel := range []string{alias + "/hooks/pre-commit", alias + "/hooks/post-checkout", alias + "/config", alias} {
			if err := fs.CopyWithinRoot(root, rel, source); err == nil {
				t.Errorf("CopyWithinRoot(%q) succeeded, want rejection", rel)
			}
			if err := fs.SymlinkWithinRoot(root, rel, "target"); err == nil {
				t.Errorf("SymlinkWithinRoot(%q) succeeded, want rejection", rel)
			}
			if err := fs.RemoveAllWithinRoot(root, rel); err == nil {
				t.Errorf("RemoveAllWithinRoot(%q) succeeded, want rejection", rel)
			}
		}
	}

	requireSentinelUnchanged(t, root, sentinel)
	if _, err := os.Stat(filepath.Join(root, ".git", "config")); !os.IsNotExist(err) {
		t.Fatalf("config was created through alias: %v", err)
	}
}

func TestRealFS_RootConfinedPrimitivesAllowSimilarlyNamedPaths(t *testing.T) {
	root, sentinel, caseInsensitive := gitAliasFixture(t)
	fs := NewRealFS()
	source := filepath.Join(t.TempDir(), "source.txt")
	if err := os.WriteFile(source, []byte("benign"), 0600); err != nil {
		t.Fatalf("failed to write source: %v", err)
	}

	rels := []string{".github/workflows/ci.yml", ".git-hooks/pre-commit", ".gitignore", "nested/.GIT-notes/x"}
	if !caseInsensitive {
		// A distinct directory on a case-sensitive filesystem is ordinary.
		rels = append(rels, ".GIT/hooks/pre-commit")
	}
	for _, rel := range rels {
		if err := fs.CopyWithinRoot(root, rel, source); err != nil {
			t.Errorf("CopyWithinRoot(%q) failed: %v", rel, err)
			continue
		}
		content, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil || string(content) != "benign" {
			t.Errorf("content of %q = %q, error = %v", rel, content, err)
		}
	}
	if err := fs.RemoveAllWithinRoot(root, ".github"); err != nil {
		t.Errorf("RemoveAllWithinRoot(.github) failed: %v", err)
	}

	content, err := os.ReadFile(sentinel)
	if err != nil || string(content) != "original" {
		t.Fatalf("sentinel content = %q, error = %v; want unchanged", content, err)
	}
}
