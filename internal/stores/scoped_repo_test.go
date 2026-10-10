package stores

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/danieljhkim/monodev/internal/fsops"
	"github.com/danieljhkim/monodev/internal/lockfile"
)

func TestNewScopedRepo_NilComponentReturnsGlobal(t *testing.T) {
	global := NewFileStoreRepo(fsops.NewRealFS(), t.TempDir())
	if got := NewScopedRepo(global, nil); got != global {
		t.Fatal("expected global repo to be returned unchanged when component is nil")
	}
}

func TestScopedRepo_FindsComponentAndGlobalStores(t *testing.T) {
	fs := fsops.NewRealFS()
	globalDir := t.TempDir()
	componentDir := t.TempDir()
	global := NewFileStoreRepo(fs, globalDir)
	component := NewFileStoreRepo(fs, componentDir)
	repo := NewScopedRepo(global, component)

	now := time.Now()
	if err := global.Create("global-store", NewStoreMeta("global-store", now)); err != nil {
		t.Fatal(err)
	}
	if err := component.Create("qa-store", NewStoreMeta("qa-store", now)); err != nil {
		t.Fatal(err)
	}

	exists, err := repo.Exists("qa-store")
	if err != nil {
		t.Fatal(err)
	}
	if !exists {
		t.Fatal("expected component store qa-store to exist")
	}
	exists, err = repo.Exists("global-store")
	if err != nil {
		t.Fatal(err)
	}
	if !exists {
		t.Fatal("expected global store to exist")
	}

	ids, err := repo.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 2 {
		t.Fatalf("List() = %v, want 2 stores", ids)
	}

	if got := repo.OverlayRoot("qa-store"); got != filepath.Join(componentDir, "qa-store", "overlay") {
		t.Fatalf("component OverlayRoot = %q", got)
	}
	if got := repo.OverlayRoot("global-store"); got != filepath.Join(globalDir, "global-store", "overlay") {
		t.Fatalf("global OverlayRoot = %q", got)
	}
}

func TestScopedRepo_PrefersComponentAndCreatesInComponent(t *testing.T) {
	fs := fsops.NewRealFS()
	globalDir := t.TempDir()
	componentDir := t.TempDir()
	global := NewFileStoreRepo(fs, globalDir)
	component := NewFileStoreRepo(fs, componentDir)
	repo := NewScopedRepo(global, component)

	now := time.Now()
	if err := global.Create("shared", NewStoreMeta("shared-global", now)); err != nil {
		t.Fatal(err)
	}
	if err := component.Create("shared", NewStoreMeta("shared-component", now)); err != nil {
		t.Fatal(err)
	}

	meta, err := repo.LoadMeta("shared")
	if err != nil {
		t.Fatal(err)
	}
	if meta.Name != "shared-component" {
		t.Fatalf("LoadMeta name = %q, want shared-component", meta.Name)
	}

	if err := repo.Create("new-store", NewStoreMeta("new-store", now)); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(componentDir, "new-store")); err != nil {
		t.Fatalf("new store should be created in component scope: %v", err)
	}
	if _, err := os.Stat(filepath.Join(globalDir, "new-store")); !os.IsNotExist(err) {
		t.Fatal("new store should not be created in global scope")
	}
}

func TestScopedRepo_ListRepoLocalExcludesSharedStores(t *testing.T) {
	fs := fsops.NewRealFS()
	global := NewFileStoreRepo(fs, t.TempDir())
	component := NewFileStoreRepo(fs, t.TempDir())
	now := time.Now()
	if err := global.Create("shared-store", NewStoreMeta("shared-store", now)); err != nil {
		t.Fatal(err)
	}
	if err := component.Create("local-store", NewStoreMeta("local-store", now)); err != nil {
		t.Fatal(err)
	}

	scoped, ok := NewScopedRepo(global, component).(RepoLocalLister)
	if !ok {
		t.Fatal("scoped repo does not implement RepoLocalLister")
	}
	local, err := scoped.ListRepoLocal()
	if err != nil {
		t.Fatal(err)
	}
	if len(local) != 1 || local[0] != "local-store" {
		t.Fatalf("ListRepoLocal = %v, want [local-store]", local)
	}

	shared := NewSharedRepo(global)
	local, err = shared.(RepoLocalLister).ListRepoLocal()
	if err != nil {
		t.Fatal(err)
	}
	if len(local) != 0 {
		t.Fatalf("shared ListRepoLocal = %v, want none", local)
	}
	if exists, err := shared.Exists("shared-store"); err != nil || !exists {
		t.Fatalf("shared Exists(shared-store) = %v, %v; want true", exists, err)
	}
}

var errScriptedLookup = errors.New("scripted lookup failed")

// scriptedRepo records which operations routing actually performs.
type scriptedRepo struct {
	name      string
	exists    bool
	existsErr error
	meta      *StoreMeta
	calls     []string
}

func (r *scriptedRepo) call(method string) {
	r.calls = append(r.calls, method)
}

func (r *scriptedRepo) List() ([]string, error) {
	r.call("List")
	if r.exists {
		return []string{"shared"}, nil
	}
	return nil, nil
}

func (r *scriptedRepo) Exists(string) (bool, error) {
	r.call("Exists")
	if r.existsErr != nil {
		return false, r.existsErr
	}
	return r.exists, nil
}

func (r *scriptedRepo) Create(string, *StoreMeta) error {
	r.call("Create")
	r.exists = true
	return nil
}

func (r *scriptedRepo) LoadMeta(string) (*StoreMeta, error) {
	r.call("LoadMeta")
	if r.meta == nil {
		return nil, errors.New("store not found")
	}
	return r.meta, nil
}

func (r *scriptedRepo) SaveMeta(_ string, meta *StoreMeta) error {
	r.call("SaveMeta")
	r.meta = meta
	return nil
}

func (r *scriptedRepo) LoadTrack(string) (*TrackFile, error) {
	r.call("LoadTrack")
	return NewTrackFile(), nil
}

func (r *scriptedRepo) SaveTrack(string, *TrackFile) error {
	r.call("SaveTrack")
	return nil
}

func (r *scriptedRepo) OverlayRoot(id string) string {
	r.call("OverlayRoot")
	return "/overlay/" + r.name + "/" + id
}

func (r *scriptedRepo) Delete(string) error {
	r.call("Delete")
	return nil
}

// lockingRepo adds lock operations without changing scriptedRepo itself, so a
// plain scriptedRepo still reports ErrLockUnsupported.
type lockingRepo struct {
	*scriptedRepo
}

func (r lockingRepo) StoreLockKey(id string) (string, error) {
	r.call("StoreLockKey")
	return "lock:" + r.name + ":" + id, nil
}

func (r lockingRepo) LockStore(context.Context, string, lockfile.Mode) (*lockfile.Lock, error) {
	r.call("LockStore")
	return nil, errors.New("scripted lock")
}

func asLocker(t *testing.T, repo StoreRepo) StoreLocker {
	t.Helper()
	locker, ok := repo.(StoreLocker)
	if !ok {
		t.Fatalf("%T does not implement StoreLocker", repo)
	}
	return locker
}

func requireCalls(t *testing.T, repo *scriptedRepo, want ...string) {
	t.Helper()
	if len(repo.calls) != len(want) {
		t.Fatalf("%s calls = %v, want %v", repo.name, repo.calls, want)
	}
	for i := range want {
		if repo.calls[i] != want[i] {
			t.Fatalf("%s calls = %v, want %v", repo.name, repo.calls, want)
		}
	}
}

func TestScopedRepo_LookupErrorDoesNotTouchOtherScope(t *testing.T) {
	component := &scriptedRepo{name: "component", existsErr: errScriptedLookup}
	global := &scriptedRepo{name: "global", exists: true, meta: &StoreMeta{Name: "GLOBAL"}}
	repo := NewScopedRepo(lockingRepo{global}, lockingRepo{component})
	now := time.Now()

	if _, err := repo.LoadMeta("shared"); !errors.Is(err, errScriptedLookup) {
		t.Fatalf("LoadMeta err = %v, want scripted lookup", err)
	}
	if err := repo.SaveMeta("shared", NewStoreMeta("WRONG_SCOPE_WRITE", now)); !errors.Is(err, errScriptedLookup) {
		t.Fatalf("SaveMeta err = %v, want scripted lookup", err)
	}
	if _, err := repo.LoadTrack("shared"); !errors.Is(err, errScriptedLookup) {
		t.Fatalf("LoadTrack err = %v, want scripted lookup", err)
	}
	if err := repo.SaveTrack("shared", NewTrackFile()); !errors.Is(err, errScriptedLookup) {
		t.Fatalf("SaveTrack err = %v, want scripted lookup", err)
	}
	if err := repo.Delete("shared"); !errors.Is(err, errScriptedLookup) {
		t.Fatalf("Delete err = %v, want scripted lookup", err)
	}
	if err := repo.Create("fresh", NewStoreMeta("fresh", now)); !errors.Is(err, errScriptedLookup) {
		t.Fatalf("Create err = %v, want scripted lookup", err)
	}
	if key, err := asLocker(t, repo).StoreLockKey("shared"); key != "" || !errors.Is(err, errScriptedLookup) {
		t.Fatalf("StoreLockKey = %q, %v; want lookup error", key, err)
	}
	if lock, err := asLocker(t, repo).LockStore(context.Background(), "shared", lockfile.Shared); lock != nil || !errors.Is(err, errScriptedLookup) {
		t.Fatalf("LockStore = %v, %v; want lookup error", lock, err)
	}
	if got := repo.OverlayRoot("shared"); got != "" {
		t.Fatalf("OverlayRoot = %q, want empty", got)
	}
	if global.meta.Name != "GLOBAL" {
		t.Fatalf("global meta name = %q, want GLOBAL", global.meta.Name)
	}

	requireCalls(t, component, "Exists", "Exists", "Exists", "Exists", "Exists", "Exists", "Exists", "Exists", "Exists")
	requireCalls(t, global)
}

func TestScopedRepo_GlobalLookupErrorAfterConfirmedAbsence(t *testing.T) {
	component := &scriptedRepo{name: "component", exists: false}
	global := &scriptedRepo{name: "global", existsErr: errScriptedLookup, meta: &StoreMeta{Name: "GLOBAL"}}
	repo := NewScopedRepo(lockingRepo{global}, lockingRepo{component})

	if _, err := repo.LoadMeta("shared"); !errors.Is(err, errScriptedLookup) {
		t.Fatalf("LoadMeta err = %v, want scripted lookup", err)
	}
	if err := repo.SaveMeta("shared", NewStoreMeta("WRONG_SCOPE_WRITE", time.Now())); !errors.Is(err, errScriptedLookup) {
		t.Fatalf("SaveMeta err = %v, want scripted lookup", err)
	}
	if got := repo.OverlayRoot("shared"); got != "" {
		t.Fatalf("OverlayRoot = %q, want empty", got)
	}
	if _, err := asLocker(t, repo).StoreLockKey("shared"); !errors.Is(err, errScriptedLookup) {
		t.Fatalf("StoreLockKey err = %v, want scripted lookup", err)
	}
	if global.meta.Name != "GLOBAL" {
		t.Fatalf("global meta name = %q, want GLOBAL", global.meta.Name)
	}
	requireCalls(t, component, "Exists", "Exists", "Exists", "Exists")
	requireCalls(t, global, "Exists", "Exists", "Exists", "Exists")
}

func TestScopedRepo_ConfirmedAbsenceFallsBackAndSameIDPrefersComponent(t *testing.T) {
	t.Run("confirmed component absence uses global", func(t *testing.T) {
		component := &scriptedRepo{name: "component", exists: false}
		global := &scriptedRepo{name: "global", exists: true, meta: &StoreMeta{Name: "GLOBAL"}}
		repo := NewScopedRepo(lockingRepo{global}, lockingRepo{component})

		meta, err := repo.LoadMeta("shared")
		if err != nil {
			t.Fatal(err)
		}
		if meta.Name != "GLOBAL" {
			t.Fatalf("LoadMeta name = %q, want GLOBAL", meta.Name)
		}
		if got := repo.OverlayRoot("shared"); got != "/overlay/global/shared" {
			t.Fatalf("OverlayRoot = %q, want global", got)
		}
		key, err := asLocker(t, repo).StoreLockKey("shared")
		if err != nil {
			t.Fatal(err)
		}
		if key != "lock:global:shared" {
			t.Fatalf("StoreLockKey = %q, want global", key)
		}
		requireCalls(t, component, "Exists", "Exists", "Exists")
		requireCalls(t, global, "Exists", "LoadMeta", "Exists", "OverlayRoot", "Exists", "StoreLockKey")
	})

	t.Run("same id prefers component", func(t *testing.T) {
		component := &scriptedRepo{name: "component", exists: true, meta: &StoreMeta{Name: "COMPONENT"}}
		global := &scriptedRepo{name: "global", exists: true, meta: &StoreMeta{Name: "GLOBAL"}}
		repo := NewScopedRepo(lockingRepo{global}, lockingRepo{component})

		meta, err := repo.LoadMeta("shared")
		if err != nil {
			t.Fatal(err)
		}
		if meta.Name != "COMPONENT" {
			t.Fatalf("LoadMeta name = %q, want COMPONENT", meta.Name)
		}
		if err := repo.SaveMeta("shared", NewStoreMeta("COMPONENT_WRITE", time.Now())); err != nil {
			t.Fatal(err)
		}
		if component.meta.Name != "COMPONENT_WRITE" {
			t.Fatalf("component meta = %q, want COMPONENT_WRITE", component.meta.Name)
		}
		if global.meta.Name != "GLOBAL" {
			t.Fatalf("global meta = %q, want GLOBAL", global.meta.Name)
		}
		if got := repo.OverlayRoot("shared"); got != "/overlay/component/shared" {
			t.Fatalf("OverlayRoot = %q, want component", got)
		}
		requireCalls(t, global)
	})

	t.Run("confirmed absence of both creates in component", func(t *testing.T) {
		component := &scriptedRepo{name: "component", exists: false}
		global := &scriptedRepo{name: "global", exists: false}
		repo := NewScopedRepo(global, component)
		if err := repo.Create("fresh", NewStoreMeta("fresh", time.Now())); err != nil {
			t.Fatal(err)
		}
		requireCalls(t, component, "Exists", "Create")
		requireCalls(t, global, "Exists")
		if !component.exists {
			t.Fatal("create did not land in the component scope")
		}
	})

	t.Run("resolved repo without locking reports unsupported", func(t *testing.T) {
		component := &scriptedRepo{name: "component", exists: true}
		global := &scriptedRepo{name: "global", exists: true}
		repo := NewScopedRepo(global, component)
		if _, err := asLocker(t, repo).StoreLockKey("shared"); !errors.Is(err, ErrLockUnsupported) {
			t.Fatalf("StoreLockKey err = %v, want ErrLockUnsupported", err)
		}
		if _, err := asLocker(t, repo).LockStore(context.Background(), "shared", lockfile.Exclusive); !errors.Is(err, ErrLockUnsupported) {
			t.Fatalf("LockStore err = %v, want ErrLockUnsupported", err)
		}
		requireCalls(t, global)
	})
}

func TestScopedRepo_PermissionDeniedDoesNotUseOtherScope(t *testing.T) {
	if os.Geteuid() <= 0 {
		t.Skip("directory search permission is not enforced for this user")
	}

	fs := fsops.NewRealFS()
	globalDir := t.TempDir()
	componentDir := t.TempDir()
	global := NewFileStoreRepo(fs, globalDir)
	component := NewFileStoreRepo(fs, componentDir)
	repo := NewScopedRepo(global, component)

	now := time.Now()
	const id = "shared"
	if err := global.Create(id, NewStoreMeta("GLOBAL", now)); err != nil {
		t.Fatal(err)
	}
	if err := component.Create(id, NewStoreMeta("COMPONENT", now)); err != nil {
		t.Fatal(err)
	}
	globalMarker := filepath.Join(global.OverlayRoot(id), "marker.txt")
	componentMarker := filepath.Join(component.OverlayRoot(id), "marker.txt")
	if err := os.WriteFile(globalMarker, []byte("GLOBAL"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(componentMarker, []byte("COMPONENT"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := os.Chmod(componentDir, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(componentDir, 0o700) })

	if _, err := component.Exists(id); !errors.Is(err, os.ErrPermission) {
		t.Fatalf("component.Exists err = %v, want permission denied", err)
	}

	assertPermission := func(err error) {
		t.Helper()
		if !errors.Is(err, os.ErrPermission) {
			t.Fatalf("err = %v, want permission denied", err)
		}
	}
	_, err := repo.LoadMeta(id)
	assertPermission(err)
	assertPermission(repo.SaveMeta(id, NewStoreMeta("WRONG_SCOPE_WRITE", now)))
	_, err = repo.LoadTrack(id)
	assertPermission(err)
	assertPermission(repo.SaveTrack(id, NewTrackFile()))
	assertPermission(repo.Delete(id))
	assertPermission(repo.Create("fresh", NewStoreMeta("fresh", now)))
	_, err = asLocker(t, repo).StoreLockKey(id)
	assertPermission(err)
	_, err = asLocker(t, repo).LockStore(context.Background(), id, lockfile.Shared)
	assertPermission(err)
	if got := repo.OverlayRoot(id); got != "" {
		t.Fatalf("OverlayRoot = %q, want empty", got)
	}

	meta, err := global.LoadMeta(id)
	if err != nil {
		t.Fatal(err)
	}
	if meta.Name != "GLOBAL" {
		t.Fatalf("global meta name = %q, want GLOBAL", meta.Name)
	}
	marker, err := os.ReadFile(globalMarker)
	if err != nil {
		t.Fatal(err)
	}
	if string(marker) != "GLOBAL" {
		t.Fatalf("global marker = %q, want GLOBAL", marker)
	}
	if _, err := os.Stat(filepath.Join(globalDir, "fresh")); !os.IsNotExist(err) {
		t.Fatalf("fresh store appeared in global scope: %v", err)
	}
	lockEntries, err := os.ReadDir(filepath.Join(globalDir, ".locks"))
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	if len(lockEntries) != 0 {
		t.Fatalf("global locks = %v, want none", lockEntries)
	}

	if err := os.Chmod(componentDir, 0o700); err != nil {
		t.Fatal(err)
	}
	meta, err = component.LoadMeta(id)
	if err != nil {
		t.Fatal(err)
	}
	if meta.Name != "COMPONENT" {
		t.Fatalf("component meta name = %q, want COMPONENT", meta.Name)
	}
	marker, err = os.ReadFile(componentMarker)
	if err != nil {
		t.Fatal(err)
	}
	if string(marker) != "COMPONENT" {
		t.Fatalf("component marker = %q, want COMPONENT", marker)
	}
	if _, err := os.Stat(filepath.Join(componentDir, "fresh")); !os.IsNotExist(err) {
		t.Fatalf("fresh store appeared in component scope: %v", err)
	}
}

func TestScopedRepo_RealDirsFallbackAndPreference(t *testing.T) {
	fs := fsops.NewRealFS()
	globalDir := t.TempDir()
	componentDir := t.TempDir()
	global := NewFileStoreRepo(fs, globalDir)
	component := NewFileStoreRepo(fs, componentDir)
	repo := NewScopedRepo(global, component)
	now := time.Now()

	t.Run("missing component store falls back to global", func(t *testing.T) {
		if err := global.Create("only-global", NewStoreMeta("GLOBAL", now)); err != nil {
			t.Fatal(err)
		}
		meta, err := repo.LoadMeta("only-global")
		if err != nil {
			t.Fatal(err)
		}
		if meta.Name != "GLOBAL" {
			t.Fatalf("LoadMeta name = %q, want GLOBAL", meta.Name)
		}
		if got := repo.OverlayRoot("only-global"); got != filepath.Join(globalDir, "only-global", "overlay") {
			t.Fatalf("OverlayRoot = %q, want global", got)
		}
		if err := repo.SaveMeta("only-global", NewStoreMeta("GLOBAL_WRITE", now)); err != nil {
			t.Fatal(err)
		}
		meta, err = global.LoadMeta("only-global")
		if err != nil {
			t.Fatal(err)
		}
		if meta.Name != "GLOBAL_WRITE" {
			t.Fatalf("global meta name = %q, want GLOBAL_WRITE", meta.Name)
		}
		if _, err := os.Stat(filepath.Join(componentDir, "only-global")); !os.IsNotExist(err) {
			t.Fatalf("fallback wrote the component scope: %v", err)
		}
	})

	t.Run("same id prefers component", func(t *testing.T) {
		if err := global.Create("shared", NewStoreMeta("GLOBAL", now)); err != nil {
			t.Fatal(err)
		}
		if err := component.Create("shared", NewStoreMeta("COMPONENT", now)); err != nil {
			t.Fatal(err)
		}
		if err := repo.SaveMeta("shared", NewStoreMeta("COMPONENT_WRITE", now)); err != nil {
			t.Fatal(err)
		}
		componentMeta, err := component.LoadMeta("shared")
		if err != nil {
			t.Fatal(err)
		}
		if componentMeta.Name != "COMPONENT_WRITE" {
			t.Fatalf("component meta name = %q, want COMPONENT_WRITE", componentMeta.Name)
		}
		globalMeta, err := global.LoadMeta("shared")
		if err != nil {
			t.Fatal(err)
		}
		if globalMeta.Name != "GLOBAL" {
			t.Fatalf("global meta name = %q, want GLOBAL", globalMeta.Name)
		}
		key, err := asLocker(t, repo).StoreLockKey("shared")
		if err != nil {
			t.Fatal(err)
		}
		if !strings.HasPrefix(key, componentDir) {
			t.Fatalf("StoreLockKey = %q, want a path under %s", key, componentDir)
		}
	})
}
