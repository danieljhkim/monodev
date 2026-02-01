package planner

import (
	"os"
	"testing"
	"time"

	"github.com/danieljhkim/monodev/internal/state"
)

// mockFS is a mock implementation of fsops.FS for testing
type mockFS struct {
	exists      map[string]bool
	lstat       map[string]os.FileInfo
	readlink    map[string]string
	readlinkErr map[string]error
}

func newMockFS() *mockFS {
	return &mockFS{
		exists:      make(map[string]bool),
		lstat:       make(map[string]os.FileInfo),
		readlink:    make(map[string]string),
		readlinkErr: make(map[string]error),
	}
}

func (m *mockFS) setExists(path string, exists bool) {
	m.exists[path] = exists
}

func (m *mockFS) setLstat(path string, info os.FileInfo) {
	m.lstat[path] = info
}

func (m *mockFS) setReadlink(path string, target string, err error) {
	if err != nil {
		m.readlinkErr[path] = err
	} else {
		m.readlink[path] = target
	}
}

func (m *mockFS) Exists(path string) (bool, error) {
	if exists, ok := m.exists[path]; ok {
		return exists, nil
	}
	return false, nil
}

func (m *mockFS) Lstat(path string) (os.FileInfo, error) {
	if info, ok := m.lstat[path]; ok {
		return info, nil
	}
	return nil, os.ErrNotExist
}

func (m *mockFS) Readlink(path string) (string, error) {
	if err, ok := m.readlinkErr[path]; ok {
		return "", err
	}
	if target, ok := m.readlink[path]; ok {
		return target, nil
	}
	return "", os.ErrInvalid
}

// Unused methods for mockFS
func (m *mockFS) MkdirAll(path string, perm os.FileMode) error                 { return nil }
func (m *mockFS) Remove(path string) error                                     { return nil }
func (m *mockFS) RemoveAll(path string) error                                  { return nil }
func (m *mockFS) Symlink(oldname, newname string) error                        { return nil }
func (m *mockFS) Copy(src, dst string) error                                   { return nil }
func (m *mockFS) AtomicWrite(path string, data []byte, perm os.FileMode) error { return nil }
func (m *mockFS) ReadFile(path string) ([]byte, error)                         { return nil, nil }
func (m *mockFS) ValidateRelPath(relPath string) error                         { return nil }
func (m *mockFS) ValidateIdentifier(id string) error                           { return nil }

// mockFileInfo is a simple implementation of os.FileInfo
type mockFileInfo struct {
	name  string
	size  int64
	mode  os.FileMode
	isDir bool
}

func (m *mockFileInfo) Name() string       { return m.name }
func (m *mockFileInfo) Size() int64        { return m.size }
func (m *mockFileInfo) Mode() os.FileMode  { return m.mode }
func (m *mockFileInfo) ModTime() time.Time { return time.Time{} }
func (m *mockFileInfo) IsDir() bool        { return m.isDir }
func (m *mockFileInfo) Sys() interface{}   { return nil }

func TestConflictChecker_CheckPath_NoConflict(t *testing.T) {
	tests := []struct {
		name          string
		relPath       string
		destPath      string
		incomingType  string
		incomingMode  string
		incomingStore string
		setupFS       func(*mockFS)
		setupState    func() *state.WorkspaceState
		force         bool
		wantConflict  bool
	}{
		{
			name:          "path does not exist",
			relPath:       "Makefile",
			destPath:      "/workspace/Makefile",
			incomingType:  "file",
			incomingMode:  "symlink",
			incomingStore: "store1",
			setupFS: func(fs *mockFS) {
				fs.setExists("/workspace/Makefile", false)
			},
			setupState: func() *state.WorkspaceState {
				return state.NewWorkspaceState("repo1", "workspace", "symlink")
			},
			force:        false,
			wantConflict: false,
		},
		{
			name:          "path exists but unmanaged with force",
			relPath:       "Makefile",
			destPath:      "/workspace/Makefile",
			incomingType:  "file",
			incomingMode:  "symlink",
			incomingStore: "store1",
			setupFS: func(fs *mockFS) {
				fs.setExists("/workspace/Makefile", true)
				fs.setLstat("/workspace/Makefile", &mockFileInfo{name: "Makefile", isDir: false})
			},
			setupState: func() *state.WorkspaceState {
				return state.NewWorkspaceState("repo1", "workspace", "symlink")
			},
			force:        true,
			wantConflict: false,
		},
		{
			name:          "store-to-store override same mode",
			relPath:       "Makefile",
			destPath:      "/workspace/Makefile",
			incomingType:  "file",
			incomingMode:  "symlink",
			incomingStore: "store2",
			setupFS: func(fs *mockFS) {
				fs.setExists("/workspace/Makefile", true)
				fs.setLstat("/workspace/Makefile", &mockFileInfo{name: "Makefile", isDir: false})
				fs.setReadlink("/workspace/Makefile", "/store1/overlay/Makefile", nil)
			},
			setupState: func() *state.WorkspaceState {
				ws := state.NewWorkspaceState("repo1", "workspace", "symlink")
				ws.Paths["Makefile"] = state.PathOwnership{
					Store: "store1",
					Type:  "symlink",
				}
				return ws
			},
			force:        false,
			wantConflict: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fs := newMockFS()
			if tt.setupFS != nil {
				tt.setupFS(fs)
			}
			workspace := tt.setupState()
			checker := NewConflictChecker(fs, workspace, tt.force)

			conflict := checker.CheckPath(tt.relPath, tt.destPath, tt.incomingType, tt.incomingMode, tt.incomingStore)

			if tt.wantConflict && conflict == nil {
				t.Errorf("expected conflict but got none")
			}
			if !tt.wantConflict && conflict != nil {
				t.Errorf("unexpected conflict: %v", conflict)
			}
		})
	}
}

func TestConflictChecker_CheckPath_UnmanagedConflict(t *testing.T) {
	fs := newMockFS()
	fs.setExists("/workspace/Makefile", true)
	fs.setLstat("/workspace/Makefile", &mockFileInfo{name: "Makefile", isDir: false})

	workspace := state.NewWorkspaceState("repo1", "workspace", "symlink")
	checker := NewConflictChecker(fs, workspace, false)

	conflict := checker.CheckPath("Makefile", "/workspace/Makefile", "file", "symlink", "store1")

	if conflict == nil {
		t.Fatal("expected conflict for unmanaged path")
	}

	if conflict.Path != "Makefile" {
		t.Errorf("expected Path='Makefile', got %q", conflict.Path)
	}
	if conflict.Existing != "unmanaged" {
		t.Errorf("expected Existing='unmanaged', got %q", conflict.Existing)
	}
	if conflict.Incoming != "file" {
		t.Errorf("expected Incoming='file', got %q", conflict.Incoming)
	}
}

func TestConflictChecker_CheckPath_ModeMismatch(t *testing.T) {
	fs := newMockFS()
	fs.setExists("/workspace/Makefile", true)
	fs.setLstat("/workspace/Makefile", &mockFileInfo{name: "Makefile", isDir: false})
	fs.setReadlink("/workspace/Makefile", "/store1/overlay/Makefile", nil)

	workspace := state.NewWorkspaceState("repo1", "workspace", "symlink")
	workspace.Paths["Makefile"] = state.PathOwnership{
		Store: "store1",
		Type:  "symlink",
	}
	checker := NewConflictChecker(fs, workspace, false)

	conflict := checker.CheckPath("Makefile", "/workspace/Makefile", "file", "copy", "store2")

	if conflict == nil {
		t.Fatal("expected conflict for mode mismatch")
	}

	if conflict.Path != "Makefile" {
		t.Errorf("expected Path='Makefile', got %q", conflict.Path)
	}
	if conflict.Existing != "symlink" {
		t.Errorf("expected Existing='symlink', got %q", conflict.Existing)
	}
	if conflict.Incoming != "copy" {
		t.Errorf("expected Incoming='copy', got %q", conflict.Incoming)
	}
	if conflict.Reason == "" {
		t.Error("expected non-empty Reason")
	}
}

func TestConflictChecker_CheckPath_TypeMismatch(t *testing.T) {
	tests := []struct {
		name          string
		existingIsDir bool
		incomingType  string
		force         bool
		wantConflict  bool
	}{
		{
			name:          "file vs directory without force",
			existingIsDir: false,
			incomingType:  "directory",
			force:         false,
			wantConflict:  true,
		},
		{
			name:          "directory vs file without force",
			existingIsDir: true,
			incomingType:  "file",
			force:         false,
			wantConflict:  true,
		},
		{
			name:          "file vs directory with force",
			existingIsDir: false,
			incomingType:  "directory",
			force:         true,
			wantConflict:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fs := newMockFS()
			fs.setExists("/workspace/path", true)
			fs.setLstat("/workspace/path", &mockFileInfo{name: "path", isDir: tt.existingIsDir})

			workspace := state.NewWorkspaceState("repo1", "workspace", "symlink")
			workspace.Paths["path"] = state.PathOwnership{
				Store: "store1",
				Type:  "symlink",
			}
			checker := NewConflictChecker(fs, workspace, tt.force)

			conflict := checker.CheckPath("path", "/workspace/path", tt.incomingType, "symlink", "store2")

			if tt.wantConflict && conflict == nil {
				t.Errorf("expected conflict but got none")
			}
			if !tt.wantConflict && conflict != nil {
				t.Errorf("unexpected conflict: %v", conflict)
			}
		})
	}
}

func TestConflictChecker_CheckPath_SymlinkValidation(t *testing.T) {
	tests := []struct {
		name         string
		setupFS      func(*mockFS)
		force        bool
		wantConflict bool
	}{
		{
			name: "valid symlink",
			setupFS: func(fs *mockFS) {
				fs.setExists("/workspace/Makefile", true)
				fs.setLstat("/workspace/Makefile", &mockFileInfo{name: "Makefile", isDir: false})
				fs.setReadlink("/workspace/Makefile", "/store1/overlay/Makefile", nil)
			},
			force:        false,
			wantConflict: false,
		},
		{
			name: "expected symlink but found non-symlink without force",
			setupFS: func(fs *mockFS) {
				fs.setExists("/workspace/Makefile", true)
				fs.setLstat("/workspace/Makefile", &mockFileInfo{name: "Makefile", isDir: false})
				fs.setReadlink("/workspace/Makefile", "", os.ErrInvalid)
			},
			force:        false,
			wantConflict: true,
		},
		{
			name: "expected symlink but found non-symlink with force",
			setupFS: func(fs *mockFS) {
				fs.setExists("/workspace/Makefile", true)
				fs.setLstat("/workspace/Makefile", &mockFileInfo{name: "Makefile", isDir: false})
				fs.setReadlink("/workspace/Makefile", "", os.ErrInvalid)
			},
			force:        true,
			wantConflict: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fs := newMockFS()
			tt.setupFS(fs)

			workspace := state.NewWorkspaceState("repo1", "workspace", "symlink")
			workspace.Paths["Makefile"] = state.PathOwnership{
				Store: "store1",
				Type:  "symlink",
			}
			checker := NewConflictChecker(fs, workspace, tt.force)

			conflict := checker.CheckPath("Makefile", "/workspace/Makefile", "file", "symlink", "store2")

			if tt.wantConflict && conflict == nil {
				t.Errorf("expected conflict but got none")
			}
			if !tt.wantConflict && conflict != nil {
				t.Errorf("unexpected conflict: %v", conflict)
			}
		})
	}
}

func TestConflictChecker_IsPathManaged(t *testing.T) {
	workspace := state.NewWorkspaceState("repo1", "workspace", "symlink")
	workspace.Paths["Makefile"] = state.PathOwnership{
		Store: "store1",
		Type:  "symlink",
	}
	workspace.Paths["scripts"] = state.PathOwnership{
		Store: "store1",
		Type:  "symlink",
	}

	fs := newMockFS()
	checker := NewConflictChecker(fs, workspace, false)

	tests := []struct {
		path     string
		expected bool
	}{
		{"Makefile", true},
		{"scripts", true},
		{"nonexistent", false},
		{"other/file", false},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			result := checker.IsPathManaged(tt.path)
			if result != tt.expected {
				t.Errorf("IsPathManaged(%q) = %v, want %v", tt.path, result, tt.expected)
			}
		})
	}
}

func TestConflictChecker_GetOwnership(t *testing.T) {
	workspace := state.NewWorkspaceState("repo1", "workspace", "symlink")
	workspace.Paths["Makefile"] = state.PathOwnership{
		Store: "store1",
		Type:  "symlink",
	}
	workspace.Paths["scripts"] = state.PathOwnership{
		Store: "store2",
		Type:  "copy",
	}

	fs := newMockFS()
	checker := NewConflictChecker(fs, workspace, false)

	tests := []struct {
		path     string
		expected *state.PathOwnership
	}{
		{
			"Makefile",
			&state.PathOwnership{Store: "store1", Type: "symlink"},
		},
		{
			"scripts",
			&state.PathOwnership{Store: "store2", Type: "copy"},
		},
		{
			"nonexistent",
			nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			result := checker.GetOwnership(tt.path)
			if tt.expected == nil {
				if result != nil {
					t.Errorf("GetOwnership(%q) = %v, want nil", tt.path, result)
				}
			} else {
				if result == nil {
					t.Errorf("GetOwnership(%q) = nil, want %v", tt.path, tt.expected)
				} else if result.Store != tt.expected.Store || result.Type != tt.expected.Type {
					t.Errorf("GetOwnership(%q) = %v, want %v", tt.path, result, tt.expected)
				}
			}
		})
	}
}

func TestConflictChecker_CheckPath_FilesystemError(t *testing.T) {
	fs := newMockFS()
	workspace := state.NewWorkspaceState("repo1", "workspace", "symlink")
	checker := NewConflictChecker(fs, workspace, false)

	// Test with a path that doesn't exist - should not conflict
	fs.setExists("/workspace/Makefile", false)

	conflict := checker.CheckPath("Makefile", "/workspace/Makefile", "file", "symlink", "store1")

	// Should not conflict if path doesn't exist
	if conflict != nil {
		t.Errorf("unexpected conflict when path doesn't exist: %v", conflict)
	}
}

func TestConflictChecker_CheckPath_RelativePathHandling(t *testing.T) {
	// Test that relative paths are used correctly for state lookups
	// while absolute paths are used for filesystem operations
	fs := newMockFS()
	fs.setExists("/different/absolute/path/Makefile", true)
	fs.setLstat("/different/absolute/path/Makefile", &mockFileInfo{name: "Makefile", isDir: false})
	fs.setReadlink("/different/absolute/path/Makefile", "/store1/overlay/Makefile", nil)

	workspace := state.NewWorkspaceState("repo1", "workspace", "symlink")
	workspace.Paths["Makefile"] = state.PathOwnership{
		Store: "store1",
		Type:  "symlink",
	}

	checker := NewConflictChecker(fs, workspace, false)

	// Use different absolute path but same relative path
	conflict := checker.CheckPath("Makefile", "/different/absolute/path/Makefile", "file", "copy", "store2")

	if conflict == nil {
		t.Fatal("expected conflict for mode mismatch")
	}

	// Conflict should reference the relative path
	if conflict.Path != "Makefile" {
		t.Errorf("expected Path='Makefile', got %q", conflict.Path)
	}
}
