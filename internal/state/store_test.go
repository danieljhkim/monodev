package state

import (
	"encoding/json"
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

func TestFileStateStore_MigratesLegacyStackOnLoad(t *testing.T) {
	workspacesDir := t.TempDir()
	workspaceID := "legacy-stack"
	fixture, err := os.ReadFile(filepath.Join("testdata", "legacy_stack_workspace.json"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	if err := os.WriteFile(filepath.Join(workspacesDir, workspaceID+".json"), fixture, 0600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	store := NewFileStateStore(fsops.NewRealFS(), workspacesDir)
	got, err := store.LoadWorkspace(workspaceID)
	if err != nil {
		t.Fatalf("LoadWorkspace() error = %v", err)
	}
	if len(got.Stack) != 0 {
		t.Fatalf("Stack = %#v, want empty after migration", got.Stack)
	}
	wantApplied := []string{"store-a", "store-b", "active-store"}
	if gotIDs := got.AppliedStoreIDs(); len(gotIDs) != len(wantApplied) {
		t.Fatalf("AppliedStores = %#v, want %v", got.AppliedStores, wantApplied)
	} else {
		for i, id := range wantApplied {
			if gotIDs[i] != id {
				t.Fatalf("AppliedStores = %#v, want %v", gotIDs, wantApplied)
			}
		}
	}
	if got.Paths["a.txt"].Store != "store-a" || got.Paths["shared.txt"].Store != "store-b" || got.Paths["active.txt"].Store != "active-store" {
		t.Fatalf("path ownership = %#v", got.Paths)
	}
	if !got.Applied {
		t.Fatal("Applied = false, want true so overlays remain applied")
	}
	if got.SchemaVersion != WorkspaceSchemaVersion {
		t.Fatalf("SchemaVersion = %d, want %d", got.SchemaVersion, WorkspaceSchemaVersion)
	}

	persisted, err := os.ReadFile(filepath.Join(workspacesDir, workspaceID+".json"))
	if err != nil {
		t.Fatalf("read persisted workspace: %v", err)
	}
	var onDisk WorkspaceState
	if err := json.Unmarshal(persisted, &onDisk); err != nil {
		t.Fatalf("unmarshal persisted workspace: %v", err)
	}
	if len(onDisk.Stack) != 0 {
		t.Fatalf("persisted Stack = %#v, want empty", onDisk.Stack)
	}
	if onDisk.AppliedStoreIDs()[0] != "store-a" || onDisk.AppliedStoreIDs()[1] != "store-b" || onDisk.AppliedStoreIDs()[2] != "active-store" {
		t.Fatalf("persisted AppliedStores = %#v", onDisk.AppliedStores)
	}
	if onDisk.Paths["shared.txt"].Store != "store-b" {
		t.Fatalf("persisted shared.txt owner = %#v, want store-b", onDisk.Paths["shared.txt"])
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(persisted, &raw); err != nil {
		t.Fatalf("unmarshal migrated workspace: %v", err)
	}
	if _, exists := raw["stack"]; exists {
		t.Fatalf("migrated workspace retains retired stack field: %s", persisted)
	}
	var extension struct {
		Keep string `json:"keep"`
	}
	if err := json.Unmarshal(raw["legacyExtension"], &extension); err != nil || extension.Keep != "this field" {
		t.Fatalf("migration dropped unrecognized field: %s", persisted)
	}

	firstMigration := string(persisted)
	if _, err := store.LoadWorkspace(workspaceID); err != nil {
		t.Fatalf("second LoadWorkspace() error = %v", err)
	}
	secondMigration, err := os.ReadFile(filepath.Join(workspacesDir, workspaceID+".json"))
	if err != nil {
		t.Fatalf("read workspace after second migration: %v", err)
	}
	if string(secondMigration) != firstMigration {
		t.Fatalf("workspace migration is not idempotent:\nfirst:  %s\nsecond: %s", firstMigration, secondMigration)
	}
	got.ActiveStore = "rewritten-store"
	if err := store.SaveWorkspace(workspaceID, got); err != nil {
		t.Fatalf("save migrated workspace: %v", err)
	}
	persistedAfterSave, err := os.ReadFile(filepath.Join(workspacesDir, workspaceID+".json"))
	if err != nil {
		t.Fatalf("read saved workspace: %v", err)
	}
	var savedRaw map[string]json.RawMessage
	if err := json.Unmarshal(persistedAfterSave, &savedRaw); err != nil {
		t.Fatalf("unmarshal saved workspace: %v", err)
	}
	if err := json.Unmarshal(savedRaw["legacyExtension"], &extension); err != nil || extension.Keep != "this field" {
		t.Fatalf("save dropped unrecognized field after migration: %s", persistedAfterSave)
	}
}

func TestFileStateStoreRejectsFutureSchemaVersion(t *testing.T) {
	workspacesDir := t.TempDir()
	workspaceID := "future-workspace"
	path := filepath.Join(workspacesDir, workspaceID+".json")
	if err := os.WriteFile(path, []byte(`{"schemaVersion":3}`), 0600); err != nil {
		t.Fatal(err)
	}

	_, err := NewFileStateStore(fsops.NewRealFS(), workspacesDir).LoadWorkspace(workspaceID)
	if err == nil {
		t.Fatal("LoadWorkspace() error = nil, want future schema refusal")
	}
	for _, want := range []string{path, "schemaVersion 3", "supported schemaVersion 2", "upgrade monodev"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("LoadWorkspace() error = %q, want %q", err, want)
		}
	}
}
