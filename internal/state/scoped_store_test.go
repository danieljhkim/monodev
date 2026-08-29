package state

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/danieljhkim/monodev/internal/fsops"
)

func TestNewScopedStore_NilSecondaryReturnsPrimary(t *testing.T) {
	primary := NewFileStateStore(fsops.NewRealFS(), t.TempDir())
	if got := NewScopedStore(primary, nil); got != primary {
		t.Fatal("expected primary store to be returned unchanged when secondary is nil")
	}
}

func TestScopedStore_LoadsFromPrimaryThenSecondary(t *testing.T) {
	fs := fsops.NewRealFS()
	primaryDir := t.TempDir()
	secondaryDir := t.TempDir()
	primary := NewFileStateStore(fs, primaryDir)
	secondary := NewFileStateStore(fs, secondaryDir)
	store := NewScopedStore(primary, secondary)

	globalWS := NewWorkspaceState("global-repo", ".", "copy")
	globalWS.ActiveStore = "global-store"
	if err := primary.SaveWorkspace("global-ws", globalWS); err != nil {
		t.Fatal(err)
	}
	componentWS := NewWorkspaceState("component-repo", ".", "copy")
	componentWS.ActiveStore = "qa-store"
	if err := secondary.SaveWorkspace("component-ws", componentWS); err != nil {
		t.Fatal(err)
	}

	got, err := store.LoadWorkspace("global-ws")
	if err != nil {
		t.Fatal(err)
	}
	if got.ActiveStore != "global-store" {
		t.Fatalf("global workspace ActiveStore = %q", got.ActiveStore)
	}
	got, err = store.LoadWorkspace("component-ws")
	if err != nil {
		t.Fatal(err)
	}
	if got.ActiveStore != "qa-store" {
		t.Fatalf("component workspace ActiveStore = %q", got.ActiveStore)
	}
}

func TestScopedStore_SavesNewIDsToPrimary(t *testing.T) {
	fs := fsops.NewRealFS()
	primaryDir := t.TempDir()
	secondaryDir := t.TempDir()
	primary := NewFileStateStore(fs, primaryDir)
	secondary := NewFileStateStore(fs, secondaryDir)
	store := NewScopedStore(primary, secondary)

	ws := NewWorkspaceState("repo", ".", "copy")
	ws.ActiveStore = "qa-store"
	if err := store.SaveWorkspace("new-ws", ws); err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(filepath.Join(primaryDir, "new-ws.json")); err != nil {
		t.Fatalf("new workspace should be saved on primary: %v", err)
	}
	if _, err := os.Stat(filepath.Join(secondaryDir, "new-ws.json")); !os.IsNotExist(err) {
		t.Fatal("new workspace should not be saved on secondary")
	}

	existing := NewWorkspaceState("component-repo", ".", "symlink")
	existing.ActiveStore = "existing-store"
	if err := secondary.SaveWorkspace("existing-ws", existing); err != nil {
		t.Fatal(err)
	}
	existing.ActiveStore = "updated-store"
	if err := store.SaveWorkspace("existing-ws", existing); err != nil {
		t.Fatal(err)
	}
	reloaded, err := secondary.LoadWorkspace("existing-ws")
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.ActiveStore != "updated-store" {
		t.Fatalf("existing secondary workspace ActiveStore = %q", reloaded.ActiveStore)
	}
	if _, err := os.Stat(filepath.Join(primaryDir, "existing-ws.json")); !os.IsNotExist(err) {
		t.Fatal("update should not copy secondary workspace onto primary")
	}
}
