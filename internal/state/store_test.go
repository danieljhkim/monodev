package state

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/danieljhkim/monodev/internal/fsops"
)

type stateStoreValidationFS struct {
	real *fsops.RealFS

	validateCalls int
	touched       int
}

func newStateStoreValidationFS() *stateStoreValidationFS {
	return &stateStoreValidationFS{real: fsops.NewRealFS()}
}

func (fs *stateStoreValidationFS) touch() error {
	fs.touched++
	return errors.New("unexpected filesystem access")
}

func (fs *stateStoreValidationFS) Lstat(path string) (os.FileInfo, error) {
	return nil, fs.touch()
}

func (fs *stateStoreValidationFS) Readlink(path string) (string, error) {
	return "", fs.touch()
}

func (fs *stateStoreValidationFS) MkdirAll(path string, perm os.FileMode) error {
	return fs.touch()
}

func (fs *stateStoreValidationFS) Remove(path string) error {
	return fs.touch()
}

func (fs *stateStoreValidationFS) RemoveAll(path string) error {
	return fs.touch()
}

func (fs *stateStoreValidationFS) Symlink(oldname, newname string) error {
	return fs.touch()
}

func (fs *stateStoreValidationFS) Copy(src, dst string) error {
	return fs.touch()
}

func (fs *stateStoreValidationFS) AtomicWrite(path string, data []byte, perm os.FileMode) error {
	return fs.touch()
}

func (fs *stateStoreValidationFS) ReadFile(path string) ([]byte, error) {
	return nil, fs.touch()
}

func (fs *stateStoreValidationFS) Exists(path string) (bool, error) {
	return false, fs.touch()
}

func (fs *stateStoreValidationFS) ValidateRelPath(relPath string) error {
	return fs.real.ValidateRelPath(relPath)
}

func (fs *stateStoreValidationFS) ValidateIdentifier(id string) error {
	fs.validateCalls++
	return fs.real.ValidateIdentifier(id)
}

func TestFileStateStoreRejectsInvalidWorkspaceIDsBeforeFilesystemAccess(t *testing.T) {
	invalidIDs := []struct {
		name string
		id   string
	}{
		{name: "empty", id: ""},
		{name: "slash", id: "workspace/state"},
		{name: "backslash", id: `workspace\state`},
		{name: "current directory", id: "."},
		{name: "parent directory", id: ".."},
		{name: "traversal prefix", id: "..workspace"},
	}

	operations := []struct {
		name string
		run  func(*FileStateStore, string) error
	}{
		{
			name: "load",
			run: func(store *FileStateStore, id string) error {
				_, err := store.LoadWorkspace(id)
				return err
			},
		},
		{
			name: "save",
			run: func(store *FileStateStore, id string) error {
				return store.SaveWorkspace(id, NewWorkspaceState("repo", "workspace", "symlink"))
			},
		},
		{
			name: "delete",
			run: func(store *FileStateStore, id string) error {
				return store.DeleteWorkspace(id)
			},
		},
	}

	for _, op := range operations {
		for _, tt := range invalidIDs {
			t.Run(op.name+"/"+tt.name, func(t *testing.T) {
				fs := newStateStoreValidationFS()
				store := NewFileStateStore(fs, "/workspaces")

				err := op.run(store, tt.id)
				if err == nil {
					t.Fatal("expected invalid workspace ID error")
				}
				if !strings.Contains(err.Error(), "invalid workspace ID") {
					t.Fatalf("error = %q, want invalid workspace ID", err)
				}
				if fs.validateCalls != 1 {
					t.Fatalf("ValidateIdentifier calls = %d, want 1", fs.validateCalls)
				}
				if fs.touched != 0 {
					t.Fatalf("filesystem was touched %d time(s), want 0", fs.touched)
				}
			})
		}
	}
}

func TestFileStateStoreValidHashWorkspaceIDLifecycle(t *testing.T) {
	fs := fsops.NewRealFS()
	store := NewFileStateStore(fs, filepath.Join(t.TempDir(), "workspaces"))
	workspaceID := ComputeWorkspaceID("repo-fingerprint", "services/api")

	want := NewWorkspaceState("repo", "services/api", "symlink")
	want.Applied = true
	want.ActiveStore = "store1"
	want.Paths["Makefile"] = PathOwnership{Store: "store1", Type: "symlink"}

	if err := store.SaveWorkspace(workspaceID, want); err != nil {
		t.Fatalf("SaveWorkspace() error = %v, want nil", err)
	}

	got, err := store.LoadWorkspace(workspaceID)
	if err != nil {
		t.Fatalf("LoadWorkspace() error = %v, want nil", err)
	}
	if got.Repo != want.Repo {
		t.Errorf("Repo = %q, want %q", got.Repo, want.Repo)
	}
	if got.WorkspacePath != want.WorkspacePath {
		t.Errorf("WorkspacePath = %q, want %q", got.WorkspacePath, want.WorkspacePath)
	}
	if got.ActiveStore != want.ActiveStore {
		t.Errorf("ActiveStore = %q, want %q", got.ActiveStore, want.ActiveStore)
	}
	if len(got.Paths) != 1 {
		t.Fatalf("Paths length = %d, want 1", len(got.Paths))
	}

	if err := store.DeleteWorkspace(workspaceID); err != nil {
		t.Fatalf("DeleteWorkspace() error = %v, want nil", err)
	}
	if _, err := store.LoadWorkspace(workspaceID); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("LoadWorkspace() after delete error = %v, want os.ErrNotExist", err)
	}
}
