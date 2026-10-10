package state

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/danieljhkim/monodev/internal/fsops"
	"github.com/danieljhkim/monodev/internal/lockfile"
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

func shortLockContext(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	t.Cleanup(cancel)
	return ctx
}

func TestScopedStore_LockWorkspaceExcludesBothScopes(t *testing.T) {
	fs := fsops.NewRealFS()
	primary := NewFileStateStore(fs, t.TempDir())
	secondary := NewFileStateStore(fs, t.TempDir())
	locker, ok := NewScopedStore(primary, secondary).(WorkspaceLocker)
	if !ok {
		t.Fatal("dual-scope state store must preserve WorkspaceLocker")
	}

	lock, err := locker.LockWorkspace(context.Background(), "ws", lockfile.Exclusive)
	if err != nil {
		t.Fatal(err)
	}
	for name, scope := range map[string]*FileStateStore{"primary": primary, "secondary": secondary} {
		if _, err := scope.LockWorkspace(shortLockContext(t), "ws", lockfile.Shared); !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("%s lock while scoped lock held = %v, want wait until deadline", name, err)
		}
	}
	if other, err := secondary.LockWorkspace(shortLockContext(t), "other-ws", lockfile.Exclusive); err != nil {
		t.Fatalf("independent workspace lock: %v", err)
	} else {
		_ = other.Close()
	}

	if err := lock.Close(); err != nil {
		t.Fatal(err)
	}
	for name, scope := range map[string]*FileStateStore{"primary": primary, "secondary": secondary} {
		relock, err := scope.LockWorkspace(shortLockContext(t), "ws", lockfile.Exclusive)
		if err != nil {
			t.Fatalf("%s lock after scoped release: %v", name, err)
		}
		_ = relock.Close()
	}
}

func TestScopedStore_LockWorkspaceWaitsForEitherScope(t *testing.T) {
	fs := fsops.NewRealFS()
	primary := NewFileStateStore(fs, t.TempDir())
	secondary := NewFileStateStore(fs, t.TempDir())
	scoped := NewScopedStore(primary, secondary).(WorkspaceLocker)

	for name, scope := range map[string]*FileStateStore{"primary": primary, "secondary": secondary} {
		held, err := scope.LockWorkspace(context.Background(), "ws", lockfile.Exclusive)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := scoped.LockWorkspace(shortLockContext(t), "ws", lockfile.Shared); !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("scoped lock while %s held = %v, want wait until deadline", name, err)
		}
		_ = held.Close()
	}

	// A failed secondary acquisition must release the primary lock it took.
	if relock, err := primary.LockWorkspace(shortLockContext(t), "ws", lockfile.Exclusive); err != nil {
		t.Fatalf("primary lock leaked by failed scoped acquisition: %v", err)
	} else {
		_ = relock.Close()
	}
}

type unlockedStateStore struct{ StateStore }

func TestScopedStore_LockWorkspaceUsesAvailableLockers(t *testing.T) {
	fs := fsops.NewRealFS()
	secondary := NewFileStateStore(fs, t.TempDir())
	scoped := NewScopedStore(unlockedStateStore{NewFileStateStore(fs, t.TempDir())}, secondary).(WorkspaceLocker)

	lock, err := scoped.LockWorkspace(context.Background(), "ws", lockfile.Exclusive)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := secondary.LockWorkspace(shortLockContext(t), "ws", lockfile.Shared); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("secondary lock while scoped lock held = %v, want wait until deadline", err)
	}
	_ = lock.Close()

	unsupported := NewScopedStore(unlockedStateStore{secondary}, unlockedStateStore{secondary})
	if _, err := unsupported.(WorkspaceLocker).LockWorkspace(context.Background(), "ws", lockfile.Exclusive); !errors.Is(err, ErrLockUnsupported) {
		t.Fatalf("LockWorkspace without lockers = %v, want ErrLockUnsupported", err)
	}
	unlock, err := LockWorkspace(context.Background(), unsupported, "ws", lockfile.Exclusive)
	if err != nil {
		t.Fatalf("LockWorkspace helper should treat unsupported locking as optional: %v", err)
	}
	unlock()
}
