package stores

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/danieljhkim/monodev/internal/fsops"
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
