package engine

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/danieljhkim/monodev/internal/fsops"
	"github.com/danieljhkim/monodev/internal/state"
)

func TestEject_KeepFilesPreservesModifiedBytesAndDetachesWorkspace(t *testing.T) {
	fx := newOverlayTxnFixture(t, "a.txt", "nested/b.txt")
	store := state.NewFileStateStore(fsops.NewRealFS(), fx.workspacesDir)
	eng := fx.engine(t, nil, store)
	if _, err := eng.Apply(context.Background(), &ApplyRequest{CWD: fx.repoRoot, StoreIDs: []string{fx.storeID}, Mode: "copy"}); err != nil {
		t.Fatalf("Apply: %v", err)
	}

	want := []byte("modified by the user\x00without normalization\n")
	modifiedPath := filepath.Join(fx.repoRoot, "a.txt")
	if err := os.WriteFile(modifiedPath, want, 0600); err != nil {
		t.Fatalf("modify overlaid file: %v", err)
	}

	result, err := eng.Eject(context.Background(), &EjectRequest{CWD: fx.repoRoot})
	if err != nil {
		t.Fatalf("Eject keep-files: %v", err)
	}
	if result.RemoveFiles {
		t.Fatal("keep-files result reported remove-files mode")
	}
	if len(result.Retained) != 2 || len(result.Removed) != 0 {
		t.Fatalf("Eject result = %#v, want two retained paths and no removals", result)
	}
	got, err := os.ReadFile(modifiedPath)
	if err != nil {
		t.Fatalf("read retained modified file: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("retained bytes = %q, want %q", got, want)
	}
	if _, err := os.Stat(filepath.Join(fx.repoRoot, "nested", "b.txt")); err != nil {
		t.Fatalf("second retained path missing: %v", err)
	}
	if _, err := store.LoadWorkspace(fx.workspaceID); !os.IsNotExist(err) {
		t.Fatalf("workspace ledger after eject = %v, want missing", err)
	}

	excludePath := filepath.Join(fx.repoRoot, ".git", "info", "exclude")
	exclude, err := os.ReadFile(excludePath)
	if err != nil {
		t.Fatalf("read exclude after eject: %v", err)
	}
	if strings.Contains(string(exclude), "monodev managed block") {
		t.Fatalf("managed exclude block remained after eject:\n%s", exclude)
	}
}

func TestEject_RemoveFilesPlansWithoutMutationThenRemovesAllPaths(t *testing.T) {
	fx := newOverlayTxnFixture(t, "a.txt", "nested/b.txt")
	store := state.NewFileStateStore(fsops.NewRealFS(), fx.workspacesDir)
	eng := fx.engine(t, nil, store)
	if _, err := eng.Apply(context.Background(), &ApplyRequest{CWD: fx.repoRoot, StoreIDs: []string{fx.storeID}, Mode: "copy"}); err != nil {
		t.Fatalf("Apply: %v", err)
	}

	plan, err := eng.Eject(context.Background(), &EjectRequest{CWD: fx.repoRoot, RemoveFiles: true, DryRun: true})
	if err != nil {
		t.Fatalf("Eject --remove-files --dry-run: %v", err)
	}
	if !plan.DryRun || !plan.RemoveFiles || len(plan.Removed) != 2 {
		t.Fatalf("dry-run plan = %#v, want two planned removals", plan)
	}
	if _, err := os.Stat(filepath.Join(fx.repoRoot, "a.txt")); err != nil {
		t.Fatalf("dry-run removed a.txt: %v", err)
	}
	if _, err := store.LoadWorkspace(fx.workspaceID); err != nil {
		t.Fatalf("dry-run removed workspace ledger: %v", err)
	}

	result, err := eng.Eject(context.Background(), &EjectRequest{CWD: fx.repoRoot, RemoveFiles: true})
	if err != nil {
		t.Fatalf("Eject --remove-files: %v", err)
	}
	if !result.RemoveFiles || len(result.Removed) != 2 || len(result.Retained) != 0 {
		t.Fatalf("remove-files result = %#v", result)
	}
	for _, relPath := range fx.files {
		if _, err := os.Lstat(filepath.Join(fx.repoRoot, relPath)); !os.IsNotExist(err) {
			t.Fatalf("removed path %s still exists, err=%v", relPath, err)
		}
	}
	if _, err := store.LoadWorkspace(fx.workspaceID); !os.IsNotExist(err) {
		t.Fatalf("workspace ledger after remove-files eject = %v, want missing", err)
	}
}

func TestEject_RecoversCommittedKeepFilesTransaction(t *testing.T) {
	fx := newOverlayTxnFixture(t, "a.txt")
	baseStore := state.NewFileStateStore(fsops.NewRealFS(), fx.workspacesDir)
	fx.seedApplied(t)

	failedStore := &failSaveStore{FileStateStore: baseStore, fail: true}
	failedEngine := fx.engine(t, nil, failedStore)
	_, err := failedEngine.Eject(context.Background(), &EjectRequest{CWD: fx.repoRoot})
	if err == nil || !strings.Contains(err.Error(), "injected state save failure") {
		t.Fatalf("interrupted eject error = %v, want injected state save failure", err)
	}
	if _, err := os.Stat(filepath.Join(fx.workspacesDir, fx.workspaceID+".txn.json")); err != nil {
		t.Fatalf("interrupted eject journal missing: %v", err)
	}

	cleanEngine := fx.engine(t, nil, baseStore)
	result, err := cleanEngine.Eject(context.Background(), &EjectRequest{CWD: fx.repoRoot})
	if err != nil {
		t.Fatalf("recovered eject: %v", err)
	}
	if len(result.Retained) != 1 || result.Retained[0] != "a.txt" {
		t.Fatalf("recovered result = %#v, want a retained path", result)
	}
	if got := fx.readWorkspace(t, "a.txt"); got != "overlay content" {
		t.Fatalf("recovered eject changed retained content: %q", got)
	}
	if _, err := baseStore.LoadWorkspace(fx.workspaceID); !os.IsNotExist(err) {
		t.Fatalf("recovered eject workspace ledger = %v, want missing", err)
	}
	if _, err := os.Stat(filepath.Join(fx.workspacesDir, fx.workspaceID+".txn.json")); !os.IsNotExist(err) {
		t.Fatalf("recovered eject left transaction journal: %v", err)
	}
}

func TestEject_RequiresExistingWorkspaceState(t *testing.T) {
	fx := newOverlayTxnFixture(t, "a.txt")
	eng := fx.engine(t, nil, state.NewFileStateStore(fsops.NewRealFS(), fx.workspacesDir))
	_, err := eng.Eject(context.Background(), &EjectRequest{CWD: fx.repoRoot})
	if !errors.Is(err, ErrStateMissing) {
		t.Fatalf("Eject without workspace state = %v, want ErrStateMissing", err)
	}
}
