package fsops

import (
	"os"
	"path/filepath"
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
	defer os.RemoveAll(tmpDir)

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
	defer os.RemoveAll(tmpDir)

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
	defer os.RemoveAll(tmpDir)

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
	defer os.RemoveAll(tmpDir)

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
	defer os.RemoveAll(tmpDir)

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
