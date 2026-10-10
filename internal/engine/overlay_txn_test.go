package engine

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/danieljhkim/monodev/internal/config"
	"github.com/danieljhkim/monodev/internal/fsops"
	"github.com/danieljhkim/monodev/internal/hash"
	"github.com/danieljhkim/monodev/internal/planner"
	"github.com/danieljhkim/monodev/internal/state"
	"github.com/danieljhkim/monodev/internal/stores"
)

type faultingFS struct {
	*fsops.RealFS
	mu     sync.Mutex
	n      int
	failAt int
}

func (f *faultingFS) hit() error {
	if f == nil {
		return nil
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.n++
	if f.failAt > 0 && f.n == f.failAt {
		return errors.New("injected filesystem failure")
	}
	return nil
}

func (f *faultingFS) MkdirAll(path string, perm os.FileMode) error {
	if err := f.hit(); err != nil {
		return err
	}
	return f.RealFS.MkdirAll(path, perm)
}

func (f *faultingFS) Remove(path string) error {
	if err := f.hit(); err != nil {
		return err
	}
	return f.RealFS.Remove(path)
}

func (f *faultingFS) RemoveAll(path string) error {
	if err := f.hit(); err != nil {
		return err
	}
	return f.RealFS.RemoveAll(path)
}

func (f *faultingFS) Copy(src, dst string) error {
	if err := f.hit(); err != nil {
		return err
	}
	return f.RealFS.Copy(src, dst)
}

func (f *faultingFS) AtomicWrite(path string, data []byte, perm os.FileMode) error {
	if err := f.hit(); err != nil {
		return err
	}
	return f.RealFS.AtomicWrite(path, data, perm)
}

func (f *faultingFS) Symlink(oldname, newname string) error {
	if err := f.hit(); err != nil {
		return err
	}
	return f.RealFS.Symlink(oldname, newname)
}

func (f *faultingFS) CopyWithinRoot(root, relPath, src string) error {
	if err := f.hit(); err != nil {
		return err
	}
	return f.RealFS.CopyWithinRoot(root, relPath, src)
}

func (f *faultingFS) CopyWithinRootOwned(root, relPath, src, owner string) error {
	if err := f.hit(); err != nil {
		return err
	}
	return f.RealFS.CopyWithinRootOwned(root, relPath, src, owner)
}

func (f *faultingFS) RemoveAllWithinRoot(root, relPath string) error {
	if err := f.hit(); err != nil {
		return err
	}
	return f.RealFS.RemoveAllWithinRoot(root, relPath)
}

func (f *faultingFS) SymlinkWithinRoot(root, relPath, target string) error {
	if err := f.hit(); err != nil {
		return err
	}
	return f.RealFS.SymlinkWithinRoot(root, relPath, target)
}

func (f *faultingFS) RestoreTreeWithinRoot(root, relPath, src, owner string) error {
	if err := f.hit(); err != nil {
		return err
	}
	return f.RealFS.RestoreTreeWithinRoot(root, relPath, src, owner)
}

type failSaveStore struct {
	*state.FileStateStore
	fail bool
}

func (s *failSaveStore) SaveWorkspace(id string, ws *state.WorkspaceState) error {
	if s.fail {
		return errors.New("injected state save failure")
	}
	return s.FileStateStore.SaveWorkspace(id, ws)
}

func (s *failSaveStore) DeleteWorkspace(id string) error {
	if s.fail {
		return errors.New("injected state save failure")
	}
	return s.FileStateStore.DeleteWorkspace(id)
}

type overlayTxnFixture struct {
	repoRoot      string
	overlayRoot   string
	workspacesDir string
	workspaceID   string
	storeID       string
	files         []string
}

func newOverlayTxnFixture(t *testing.T, files ...string) overlayTxnFixture {
	t.Helper()
	if len(files) == 0 {
		files = []string{"a.txt", "nested/b.txt"}
	}
	repoRoot := t.TempDir()
	overlayRoot := filepath.Join(t.TempDir(), "overlay")
	for _, rel := range files {
		writeOverlayFile(t, overlayRoot, rel)
	}
	return overlayTxnFixture{
		repoRoot:      repoRoot,
		overlayRoot:   overlayRoot,
		workspacesDir: filepath.Join(repoRoot, ".state"),
		workspaceID:   state.ComputeWorkspaceID("fp1", "."),
		storeID:       "untrusted-store",
		files:         files,
	}
}

func (fx overlayTxnFixture) track() *stores.TrackFile {
	track := stores.NewTrackFile()
	for _, rel := range fx.files {
		track.Tracked = append(track.Tracked, stores.TrackedPath{Path: rel, Kind: "file"})
	}
	return track
}

func (fx overlayTxnFixture) engine(t *testing.T, fs fsops.FS, store state.StateStore) *Engine {
	t.Helper()
	if fs == nil {
		fs = fsops.NewRealFS()
	}
	if store == nil {
		store = state.NewFileStateStore(fsops.NewRealFS(), fx.workspacesDir)
	}
	storeRepo := &realOverlayStoreRepo{trackStoreRepo: newTrackStoreRepo(), overlayRoot: fx.overlayRoot}
	storeRepo.tracks[fx.storeID] = fx.track()
	return New(
		&trackGitRepo{root: fx.repoRoot, fingerprint: "fp1", workspacePath: "."},
		storeRepo,
		store,
		fs,
		hash.NewSHA256Hasher(),
		&mockClock{},
		config.Paths{Root: filepath.Join(fx.repoRoot, ".monodev"), Stores: filepath.Dir(fx.overlayRoot), Workspaces: fx.workspacesDir},
	)
}

func (fx overlayTxnFixture) seedApplied(t *testing.T) {
	t.Helper()
	store := state.NewFileStateStore(fsops.NewRealFS(), fx.workspacesDir)
	eng := fx.engine(t, nil, store)
	if _, err := eng.Apply(context.Background(), &ApplyRequest{CWD: fx.repoRoot, StoreIDs: []string{fx.storeID}, Mode: "copy", Force: true}); err != nil {
		t.Fatalf("seed apply: %v", err)
	}
}

func (fx overlayTxnFixture) requireUserFile(t *testing.T, rel, content string) {
	t.Helper()
	path := filepath.Join(fx.repoRoot, rel)
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatalf("mkdir user file: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatalf("write user file: %v", err)
	}
}

func (fx overlayTxnFixture) readWorkspace(t *testing.T, rel string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(fx.repoRoot, rel))
	if err != nil {
		t.Fatalf("read %s: %v", rel, err)
	}
	return string(data)
}

func (fx overlayTxnFixture) requireCoherentApplied(t *testing.T, store state.StateStore) {
	t.Helper()
	ws, err := store.LoadWorkspace(fx.workspaceID)
	if err != nil {
		t.Fatalf("load workspace: %v", err)
	}
	for _, rel := range fx.files {
		if _, ok := ws.Paths[rel]; !ok {
			t.Fatalf("state missing path %s", rel)
		}
		got := fx.readWorkspace(t, rel)
		if got != "overlay content" {
			t.Fatalf("%s content = %q, want overlay content", rel, got)
		}
	}
}

func TestOverlayTxn_NthFilesystemFailureIsRecoverable(t *testing.T) {
	kinds := []string{overlayTxnApply, overlayTxnUnapply}
	for _, kind := range kinds {
		t.Run(kind, func(t *testing.T) {
			recovered := false
			for failAt := 1; failAt <= 40; failAt++ {
				fx := newOverlayTxnFixture(t)
				fx.requireUserFile(t, "a.txt", "user-original")
				if kind == overlayTxnUnapply {
					fx.seedApplied(t)
				}
				fault := &faultingFS{RealFS: fsops.NewRealFS(), failAt: failAt}
				store := state.NewFileStateStore(fault, fx.workspacesDir)
				eng := fx.engine(t, fault, store)
				err := runOverlayKind(t, eng, fx, kind)
				if err == nil {
					if failAt == 1 {
						t.Fatal("expected at least one injected filesystem failure")
					}
					break
				}
				if !strings.Contains(err.Error(), "injected filesystem failure") && !strings.Contains(err.Error(), "overlay transaction") {
					t.Fatalf("kind %s failAt %d: unexpected error %v", kind, failAt, err)
				}
				got, readErr := os.ReadFile(filepath.Join(fx.repoRoot, "a.txt"))
				if readErr == nil && strings.Contains(string(got), "partial") {
					t.Fatalf("kind %s failAt %d truncated destination: %q", kind, failAt, got)
				}

				cleanStore := state.NewFileStateStore(fsops.NewRealFS(), fx.workspacesDir)
				clean := fx.engine(t, nil, cleanStore)
				if recoverErr := runOverlayKind(t, clean, fx, kind); recoverErr != nil {
					if kind != overlayTxnUnapply || !errors.Is(recoverErr, ErrStateMissing) {
						t.Fatalf("kind %s failAt %d recovery: %v", kind, failAt, recoverErr)
					}
				}
				recovered = true
			}
			if !recovered {
				t.Fatal("did not exercise a recoverable filesystem failure")
			}
		})
	}
}

func TestOverlayTxn_StateSaveFailureIsRecoverable(t *testing.T) {
	kinds := []string{overlayTxnApply, overlayTxnUnapply}
	for _, kind := range kinds {
		t.Run(kind, func(t *testing.T) {
			fx := newOverlayTxnFixture(t)
			base := state.NewFileStateStore(fsops.NewRealFS(), fx.workspacesDir)
			if kind == overlayTxnUnapply {
				fx.seedApplied(t)
			}
			store := &failSaveStore{FileStateStore: base, fail: true}
			eng := fx.engine(t, nil, store)
			err := runOverlayKind(t, eng, fx, kind)
			if err == nil {
				t.Fatal("expected injected state save failure")
			}
			if !strings.Contains(err.Error(), "injected state save failure") {
				t.Fatalf("error = %v, want injected state save failure", err)
			}

			cleanStore := state.NewFileStateStore(fsops.NewRealFS(), fx.workspacesDir)
			clean := fx.engine(t, nil, cleanStore)
			if recoverErr := runOverlayKind(t, clean, fx, kind); recoverErr != nil {
				if kind != overlayTxnUnapply || !errors.Is(recoverErr, ErrStateMissing) {
					t.Fatalf("recovery: %v", recoverErr)
				}
			}
			if kind == overlayTxnApply {
				fx.requireCoherentApplied(t, cleanStore)
			}
		})
	}
}

func TestOverlayTxn_CancellationLeavesJournalOrRollback(t *testing.T) {
	fx := newOverlayTxnFixture(t)
	fx.requireUserFile(t, "a.txt", "user-original")
	ctx, cancel := context.WithCancel(context.Background())
	fs := &cancelAfterCopyFS{RealFS: fsops.NewRealFS(), cancel: cancel}
	store := state.NewFileStateStore(fs, fx.workspacesDir)
	eng := fx.engine(t, fs, store)
	_, err := eng.Apply(ctx, &ApplyRequest{CWD: fx.repoRoot, StoreIDs: []string{fx.storeID}, Mode: "copy", Force: true})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Apply error = %v, want context.Canceled", err)
	}

	got := fx.readWorkspace(t, "a.txt")
	if got != "user-original" && got != "overlay content" {
		t.Fatalf("destination after cancel = %q, want original or fully applied content", got)
	}

	clean := fx.engine(t, nil, state.NewFileStateStore(fsops.NewRealFS(), fx.workspacesDir))
	if _, err := clean.Apply(context.Background(), &ApplyRequest{CWD: fx.repoRoot, StoreIDs: []string{fx.storeID}, Mode: "copy", Force: true}); err != nil {
		t.Fatalf("recovery apply: %v", err)
	}
	fx.requireCoherentApplied(t, state.NewFileStateStore(fsops.NewRealFS(), fx.workspacesDir))
}

func TestOverlayTxn_DryRunDoesNotMutate(t *testing.T) {
	fx := newOverlayTxnFixture(t)
	fx.requireUserFile(t, "a.txt", "user-original")
	store := state.NewFileStateStore(fsops.NewRealFS(), fx.workspacesDir)
	eng := fx.engine(t, nil, store)
	if _, err := eng.Apply(context.Background(), &ApplyRequest{CWD: fx.repoRoot, StoreIDs: []string{fx.storeID}, Mode: "copy", DryRun: true, Force: true}); err != nil {
		t.Fatalf("dry-run apply: %v", err)
	}
	if fx.readWorkspace(t, "a.txt") != "user-original" {
		t.Fatal("dry-run mutated workspace file")
	}
	if _, err := store.LoadWorkspace(fx.workspaceID); !os.IsNotExist(err) {
		t.Fatalf("dry-run persisted workspace state, err=%v", err)
	}
	entries, err := os.ReadDir(fx.workspacesDir)
	if err != nil && !os.IsNotExist(err) {
		t.Fatalf("read workspaces: %v", err)
	}
	for _, entry := range entries {
		if strings.Contains(entry.Name(), ".txn") {
			t.Fatalf("dry-run wrote transaction artifact %s", entry.Name())
		}
	}
}

func TestOverlayTxn_SuccessfulApplyIsCoherent(t *testing.T) {
	fx := newOverlayTxnFixture(t)
	store := state.NewFileStateStore(fsops.NewRealFS(), fx.workspacesDir)
	eng := fx.engine(t, nil, store)
	if _, err := eng.Apply(context.Background(), &ApplyRequest{CWD: fx.repoRoot, StoreIDs: []string{fx.storeID}, Mode: "copy"}); err != nil {
		t.Fatalf("apply: %v", err)
	}
	fx.requireCoherentApplied(t, store)
	if _, err := os.Stat(filepath.Join(fx.workspacesDir, fx.workspaceID+".txn.json")); !os.IsNotExist(err) {
		t.Fatal("successful apply left a transaction journal")
	}
}

func TestOverlayTxn_LoadsLegacyFixtureAndRejectsFutureSchema(t *testing.T) {
	fx := newOverlayTxnFixture(t)
	eng := fx.engine(t, nil, state.NewFileStateStore(fsops.NewRealFS(), fx.workspacesDir))
	journalPath, _, err := eng.overlayTxnPaths(fx.workspaceID)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(journalPath), 0700); err != nil {
		t.Fatal(err)
	}
	fixture, err := os.ReadFile(filepath.Join("testdata", "legacy_overlay_transaction.json"))
	if err != nil {
		t.Fatalf("read legacy journal fixture: %v", err)
	}
	if err := os.WriteFile(journalPath, fixture, 0600); err != nil {
		t.Fatalf("write legacy journal fixture: %v", err)
	}
	txn, err := eng.loadOverlayTxn(journalPath)
	if err != nil {
		t.Fatalf("load legacy journal fixture: %v", err)
	}
	if txn.SchemaVersion != overlayTxnSchemaVersion || txn.Kind != overlayTxnApply || txn.Phase != overlayTxnPreparing {
		t.Fatalf("load legacy journal = %#v", txn)
	}
	first, err := os.ReadFile(journalPath)
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(first, &raw); err != nil {
		t.Fatal(err)
	}
	if _, exists := raw["version"]; exists {
		t.Fatalf("migrated journal retains legacy version header: %s", first)
	}
	var extension struct {
		Keep bool `json:"keep"`
	}
	if err := json.Unmarshal(raw["legacyExtension"], &extension); err != nil || !extension.Keep {
		t.Fatalf("migrated journal lost unrecognized data: %s", first)
	}
	if _, err := eng.loadOverlayTxn(journalPath); err != nil {
		t.Fatalf("second legacy journal load: %v", err)
	}
	second, err := os.ReadFile(journalPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(second) != string(first) {
		t.Fatalf("journal migration is not idempotent:\nfirst:  %s\nsecond: %s", first, second)
	}

	if err := os.WriteFile(journalPath, []byte(`{"schemaVersion":3}`), 0600); err != nil {
		t.Fatal(err)
	}
	_, err = eng.loadOverlayTxn(journalPath)
	if err == nil {
		t.Fatal("load future journal error = nil, want refusal")
	}
	for _, want := range []string{journalPath, "schemaVersion 3", "supported schemaVersion 2", "upgrade monodev"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("load future journal error = %q, want %q", err, want)
		}
	}
}

func TestOverlayTxn_PreparedRecoverySynchronizesExcludeLedger(t *testing.T) {
	fx := newOverlayTxnFixture(t, "a.txt")
	store := state.NewFileStateStore(fsops.NewRealFS(), fx.workspacesDir)
	ws := state.NewWorkspaceState("fp1", ".", "copy")
	ws.Applied = true
	ws.AddAppliedStore(fx.storeID, "copy")
	ws.Paths["a.txt"] = state.PathOwnership{Store: fx.storeID, Type: "copy"}
	if err := store.SaveWorkspace(fx.workspaceID, ws); err != nil {
		t.Fatalf("seed workspace state: %v", err)
	}

	eng := fx.engine(t, nil, store)
	journalPath, _, err := eng.overlayTxnPaths(fx.workspaceID)
	if err != nil {
		t.Fatalf("journal paths: %v", err)
	}
	if err := eng.writeOverlayTxn(journalPath, &overlayTxn{
		SchemaVersion: overlayTxnSchemaVersion,
		Kind:          overlayTxnApply,
		WorkspaceID:   fx.workspaceID,
		WorkspaceRoot: fx.repoRoot,
		Phase:         overlayTxnPrepared,
	}); err != nil {
		t.Fatalf("write prepared journal: %v", err)
	}

	excludePath := filepath.Join(fx.repoRoot, ".git", "info", "exclude")
	if err := os.MkdirAll(filepath.Dir(excludePath), 0700); err != nil {
		t.Fatalf("create git info directory: %v", err)
	}
	if err := os.WriteFile(excludePath, []byte("# >>> monodev managed block — do not edit <<<\n/stale.txt\n# <<< monodev managed block <<<\n"), 0600); err != nil {
		t.Fatalf("seed stale exclude block: %v", err)
	}

	warnings, err := eng.recoverWorkspaceOverlay(context.Background(), fx.workspaceID, fx.repoRoot, fx.repoRoot, ".")
	if err != nil {
		t.Fatalf("recover prepared journal: %v", err)
	}
	if len(warnings) != 0 {
		t.Fatalf("recovery warnings = %v, want none", warnings)
	}
	contents, err := os.ReadFile(excludePath)
	if err != nil {
		t.Fatalf("read recovered exclude file: %v", err)
	}
	want := "# >>> monodev managed block — do not edit <<<\n/a.txt\n# <<< monodev managed block <<<\n"
	if string(contents) != want {
		t.Fatalf("recovered exclude file = %q, want %q", contents, want)
	}
}

// seedInstallTempLeftovers places an owned temp, as an interrupted install
// leaves it, beside unrelated files that share monodev's temp prefixes.
func seedInstallTempLeftovers(t *testing.T, fx overlayTxnFixture, dirRel string) (owned []string, unrelated []string) {
	t.Helper()
	dir := filepath.Join(fx.repoRoot, dirRel)
	owner := overlayTempOwner(fx.workspaceID)
	owned = []string{
		filepath.Join(dir, ".monodev-copy-"+owner+"-1-2"),
		filepath.Join(dir, ".monodev-aside-"+owner+"-1-3"),
	}
	writeTestFile(t, owned[0], "interrupted temp")
	writeTestFile(t, filepath.Join(owned[1], "a.txt"), "interrupted aside")
	unrelated = []string{
		filepath.Join(dir, ".monodev-copy-user-notes"),
		filepath.Join(dir, ".monodev-aside-keep", "notes.txt"),
		filepath.Join(dir, ".monodev-copy-"+overlayTempOwner("other-workspace")+"-1-2"),
		filepath.Join(dir, ".monodev-copy-123-456"),
	}
	for _, path := range unrelated {
		writeTestFile(t, path, "must keep")
	}
	return owned, unrelated
}

func requireOwnedTempsSweptOnly(t *testing.T, owned, unrelated []string) {
	t.Helper()
	for _, path := range owned {
		if _, err := os.Lstat(path); !os.IsNotExist(err) {
			t.Fatalf("owned temp %s survived, lstat err=%v", path, err)
		}
	}
	for _, path := range unrelated {
		assertFileContent(t, path, "must keep")
	}
}

func TestOverlayTxn_RollbackSweepsOnlyOwnedTempsInsideWorkspace(t *testing.T) {
	fx := newOverlayTxnFixture(t, "nested/a.txt")
	fx.requireUserFile(t, "nested/a.txt", "user-original")
	owned, unrelated := seedInstallTempLeftovers(t, fx, "nested")
	eng := fx.engine(t, nil, nil)

	err := eng.runOverlayTxn(context.Background(), overlayTxnRequest{
		kind:          overlayTxnApply,
		workspaceID:   fx.workspaceID,
		workspaceRoot: fx.repoRoot,
		ops: []planner.Operation{{
			Type:       planner.OpCopy,
			SourcePath: filepath.Join(fx.overlayRoot, "nested", "a.txt"),
			DestPath:   filepath.Join(fx.repoRoot, "nested", "a.txt"),
			RelPath:    "nested/a.txt",
		}},
		finalize: func() (*state.WorkspaceState, bool, error) {
			return nil, false, errors.New("injected finalize failure")
		},
	})
	if err == nil || !strings.Contains(err.Error(), "injected finalize failure") {
		t.Fatalf("runOverlayTxn error = %v, want injected finalize failure", err)
	}
	if got := fx.readWorkspace(t, "nested/a.txt"); got != "user-original" {
		t.Fatalf("rolled back destination = %q, want user-original", got)
	}
	requireOwnedTempsSweptOnly(t, owned, unrelated)
}

func TestOverlayTxn_PreparedRecoverySweepsOnlyOwnedTemps(t *testing.T) {
	fx := newOverlayTxnFixture(t, "nested/a.txt")
	owned, unrelated := seedInstallTempLeftovers(t, fx, "nested")
	eng := fx.engine(t, nil, nil)
	journalPath, _, err := eng.overlayTxnPaths(fx.workspaceID)
	if err != nil {
		t.Fatalf("journal paths: %v", err)
	}
	if err := eng.writeOverlayTxn(journalPath, &overlayTxn{
		Kind:          overlayTxnApply,
		WorkspaceID:   fx.workspaceID,
		WorkspaceRoot: fx.repoRoot,
		Phase:         overlayTxnPrepared,
		Ops:           []overlayTxnOp{{RelPath: "nested/a.txt", Type: planner.OpCopy}},
	}); err != nil {
		t.Fatalf("write prepared journal: %v", err)
	}

	if err := eng.recoverOverlayTxn(context.Background(), fx.workspaceID, fx.repoRoot); err != nil {
		t.Fatalf("recover prepared journal: %v", err)
	}
	requireOwnedTempsSweptOnly(t, owned, unrelated)
	if _, err := os.Stat(journalPath); !os.IsNotExist(err) {
		t.Fatalf("recovered journal still exists, stat err=%v", err)
	}
}

// symlinkParentBeforeCopyFS redirects a destination parent outside the
// workspace after prepare, so install and rollback meet a symlinked ancestor.
type symlinkParentBeforeCopyFS struct {
	*fsops.RealFS
	parent, outside string
}

func (f *symlinkParentBeforeCopyFS) CopyWithinRootOwned(root, relPath, src, owner string) error {
	if err := os.Symlink(f.outside, f.parent); err != nil {
		return err
	}
	return f.RealFS.CopyWithinRootOwned(root, relPath, src, owner)
}

func TestOverlayTxn_RollbackRefusesSymlinkedAncestorWithoutTouchingOutside(t *testing.T) {
	fx := newOverlayTxnFixture(t, "nested/a.txt")
	outside := t.TempDir()
	owner := overlayTempOwner(fx.workspaceID)
	sentinels := map[string]string{
		filepath.Join(outside, ".monodev-copy-user-notes"):          "must keep",
		filepath.Join(outside, ".monodev-aside-keep", "notes.txt"):  "must keep",
		filepath.Join(outside, ".monodev-copy-"+owner+"-1-2"):       "outside owned-looking",
		filepath.Join(outside, ".monodev-aside-"+owner+"-1-3", "x"): "outside owned-looking",
		filepath.Join(outside, "unrelated.txt"):                     "must keep",
	}
	for path, content := range sentinels {
		writeTestFile(t, path, content)
	}
	fs := &symlinkParentBeforeCopyFS{RealFS: fsops.NewRealFS(), parent: filepath.Join(fx.repoRoot, "nested"), outside: outside}
	eng := fx.engine(t, fs, nil)

	err := eng.runOverlayTxn(context.Background(), overlayTxnRequest{
		kind:          overlayTxnApply,
		workspaceID:   fx.workspaceID,
		workspaceRoot: fx.repoRoot,
		ops: []planner.Operation{{
			Type:       planner.OpCopy,
			SourcePath: filepath.Join(fx.overlayRoot, "nested", "a.txt"),
			DestPath:   filepath.Join(fx.repoRoot, "nested", "a.txt"),
			RelPath:    "nested/a.txt",
		}},
		finalize: func() (*state.WorkspaceState, bool, error) { return nil, false, nil },
	})
	if err == nil || !strings.Contains(err.Error(), "symlinked destination ancestor") {
		t.Fatalf("runOverlayTxn error = %v, want symlinked destination ancestor refusal", err)
	}
	for path, content := range sentinels {
		assertFileContent(t, path, content)
	}
	if _, err := os.Lstat(filepath.Join(outside, "a.txt")); !os.IsNotExist(err) {
		t.Fatalf("refused install wrote outside the workspace, lstat err=%v", err)
	}
}

// replaceParentAfterRemoveFS swaps the destination parent for a symlink to
// outside as soon as the confined remove of relPath returns, the way an
// external process could between rollback's remove and its restore.
type replaceParentAfterRemoveFS struct {
	*fsops.RealFS
	relPath, outside string
	replaced         bool
}

func (f *replaceParentAfterRemoveFS) RemoveAllWithinRoot(root, relPath string) error {
	if err := f.RealFS.RemoveAllWithinRoot(root, relPath); err != nil {
		return err
	}
	if f.replaced || relPath != f.relPath {
		return nil
	}
	f.replaced = true
	parent := filepath.Join(root, filepath.Dir(relPath))
	if err := os.Remove(parent); err != nil {
		return err
	}
	return os.Symlink(f.outside, parent)
}

func TestOverlayTxn_RestoreRefusesParentReplacedAfterRemove(t *testing.T) {
	for _, mode := range []string{"rollback", "recovery"} {
		t.Run(mode, func(t *testing.T) {
			fx := newOverlayTxnFixture(t, "nested/a.txt")
			fx.requireUserFile(t, "nested/a.txt", "user-original")
			outside := t.TempDir()
			sentinels := map[string]string{
				filepath.Join(outside, "a.txt"):         "outside sentinel",
				filepath.Join(outside, "unrelated.txt"): "must keep",
			}
			for path, content := range sentinels {
				writeTestFile(t, path, content)
			}
			fs := &replaceParentAfterRemoveFS{RealFS: fsops.NewRealFS(), relPath: "nested/a.txt", outside: outside}
			eng := fx.engine(t, fs, nil)
			journalPath, txnDir, err := eng.overlayTxnPaths(fx.workspaceID)
			if err != nil {
				t.Fatalf("journal paths: %v", err)
			}

			switch mode {
			case "rollback":
				err = eng.runOverlayTxn(context.Background(), overlayTxnRequest{
					kind:          overlayTxnApply,
					workspaceID:   fx.workspaceID,
					workspaceRoot: fx.repoRoot,
					ops: []planner.Operation{{
						Type:       planner.OpCopy,
						SourcePath: filepath.Join(fx.overlayRoot, "nested", "a.txt"),
						DestPath:   filepath.Join(fx.repoRoot, "nested", "a.txt"),
						RelPath:    "nested/a.txt",
					}},
					finalize: func() (*state.WorkspaceState, bool, error) {
						return nil, false, errors.New("injected finalize failure")
					},
				})
				if err == nil || !strings.Contains(err.Error(), "injected finalize failure") {
					t.Fatalf("runOverlayTxn error = %v, want injected finalize failure", err)
				}
			case "recovery":
				writeTestFile(t, filepath.Join(fx.repoRoot, "nested", "a.txt"), "overlay content")
				writeTestFile(t, filepath.Join(txnDir, "backup", "0", "nested", "a.txt"), "user-original")
				if err := eng.writeOverlayTxn(journalPath, &overlayTxn{
					Kind:          overlayTxnApply,
					WorkspaceID:   fx.workspaceID,
					WorkspaceRoot: fx.repoRoot,
					Phase:         overlayTxnPrepared,
					Ops: []overlayTxnOp{{
						RelPath:     "nested/a.txt",
						Type:        planner.OpCopy,
						DestExisted: true,
						BackupRel:   "backup/0/nested/a.txt",
					}},
				}); err != nil {
					t.Fatalf("write prepared journal: %v", err)
				}
				err = eng.recoverOverlayTxn(context.Background(), fx.workspaceID, fx.repoRoot)
				if err == nil || !strings.Contains(err.Error(), "symlinked destination ancestor") {
					t.Fatalf("recoverOverlayTxn error = %v, want symlinked destination ancestor refusal", err)
				}
			}

			if !fs.replaced {
				t.Fatal("destination parent was never replaced")
			}
			for path, content := range sentinels {
				assertFileContent(t, path, content)
			}
			entries, err := os.ReadDir(outside)
			if err != nil {
				t.Fatal(err)
			}
			if len(entries) != len(sentinels) {
				t.Fatalf("restore wrote outside the workspace: %v", entries)
			}
			// The refused rollback keeps its journal and backup, so recovery
			// restores the original once the parent is a real directory again.
			if _, err := os.Stat(journalPath); err != nil {
				t.Fatalf("refused rollback discarded its journal: %v", err)
			}
			parent := filepath.Join(fx.repoRoot, "nested")
			if err := os.Remove(parent); err != nil {
				t.Fatal(err)
			}
			if err := os.Mkdir(parent, 0700); err != nil {
				t.Fatal(err)
			}
			if err := fx.engine(t, nil, nil).recoverOverlayTxn(context.Background(), fx.workspaceID, fx.repoRoot); err != nil {
				t.Fatalf("recover after repairing parent: %v", err)
			}
			if got := fx.readWorkspace(t, "nested/a.txt"); got != "user-original" {
				t.Fatalf("recovered destination = %q, want user-original", got)
			}
			for path, content := range sentinels {
				assertFileContent(t, path, content)
			}
		})
	}
}

func runOverlayKind(t *testing.T, eng *Engine, fx overlayTxnFixture, kind string) error {
	t.Helper()
	switch kind {
	case overlayTxnApply:
		_, err := eng.Apply(context.Background(), &ApplyRequest{CWD: fx.repoRoot, StoreIDs: []string{fx.storeID}, Mode: "copy", Force: true})
		return err
	case overlayTxnUnapply:
		_, err := eng.Unapply(context.Background(), &UnapplyRequest{CWD: fx.repoRoot})
		return err
	default:
		t.Fatalf("unknown kind %s", kind)
		return nil
	}
}

type cancelAfterCopyFS struct {
	*fsops.RealFS
	cancel context.CancelFunc
	once   sync.Once
}

func (f *cancelAfterCopyFS) CopyWithinRoot(root, relPath, src string) error {
	err := f.RealFS.CopyWithinRoot(root, relPath, src)
	f.once.Do(func() {
		if f.cancel != nil {
			f.cancel()
		}
	})
	return err
}

func (f *cancelAfterCopyFS) CopyWithinRootOwned(root, relPath, src, owner string) error {
	err := f.RealFS.CopyWithinRootOwned(root, relPath, src, owner)
	f.once.Do(func() {
		if f.cancel != nil {
			f.cancel()
		}
	})
	return err
}

func TestApplyRejectsSymlinkedStoreSourceAncestor(t *testing.T) {
	fx := newOverlayTxnFixture(t, "nested/private.txt")
	fx.requireUserFile(t, "nested/private.txt", "original workspace")
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "private.txt"), []byte("outside-secret-sentinel"), 0600); err != nil {
		t.Fatal(err)
	}
	ancestor := filepath.Join(fx.overlayRoot, "nested")
	if err := os.Rename(ancestor, ancestor+"-original"); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, ancestor); err != nil {
		t.Fatal(err)
	}
	_, err := fx.engine(t, nil, nil).Apply(context.Background(), &ApplyRequest{CWD: fx.repoRoot, StoreIDs: []string{fx.storeID}, Mode: "copy", Force: true})
	if err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("Apply source rejection = %v", err)
	}
	if got := fx.readWorkspace(t, "nested/private.txt"); got != "original workspace" {
		t.Fatalf("workspace changed to %q", got)
	}
}

// seedPermissionedUserTree lays out user content whose permission bits differ from
// anything a copy or temp file would produce by default.
func (fx overlayTxnFixture) seedPermissionedUserTree(t *testing.T) map[string]os.FileMode {
	t.Helper()
	files := map[string]struct {
		content string
		mode    os.FileMode
	}{
		"bin/run.sh":        {"#!/bin/sh\n", 0755},
		"bin/readonly.txt":  {"read-only\n", 0444},
		"tree/inner.txt":    {"inner\n", 0640},
		"tree/deep/leaf.sh": {"leaf\n", 0750},
	}
	for rel, f := range files {
		path := filepath.Join(fx.repoRoot, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatalf("mkdir %s: %v", rel, err)
		}
		if err := os.WriteFile(path, []byte(f.content), 0600); err != nil {
			t.Fatalf("write %s: %v", rel, err)
		}
		if err := os.Chmod(path, f.mode); err != nil {
			t.Fatalf("chmod %s: %v", rel, err)
		}
	}
	if err := os.Mkdir(filepath.Join(fx.repoRoot, "readonly-dir"), 0700); err != nil {
		t.Fatalf("mkdir readonly-dir: %v", err)
	}
	// Directory modes are set last, deepest first, so read-only parents do not
	// block the setup.
	dirs := map[string]os.FileMode{
		"tree/deep":    0750,
		"tree":         0710,
		"readonly-dir": 0555,
		"bin":          0750,
	}
	for _, rel := range []string{"tree/deep", "tree", "readonly-dir", "bin"} {
		if err := os.Chmod(filepath.Join(fx.repoRoot, rel), dirs[rel]); err != nil {
			t.Fatalf("chmod dir %s: %v", rel, err)
		}
	}
	t.Cleanup(func() { makeTreeRemovable(fx.repoRoot) })

	want := map[string]os.FileMode{}
	for rel, f := range files {
		want[rel] = f.mode
	}
	for rel, mode := range dirs {
		want[rel] = mode
	}
	return want
}

func requirePermissionModes(t *testing.T, root string, want map[string]os.FileMode) {
	t.Helper()
	for rel, mode := range want {
		info, err := os.Lstat(filepath.Join(root, rel))
		if err != nil {
			t.Fatalf("stat %s: %v", rel, err)
		}
		if got := info.Mode().Perm(); got != mode {
			t.Errorf("%s mode = %04o, want %04o", rel, got, mode)
		}
	}
}

func permissionTxnOps(fx overlayTxnFixture) []planner.Operation {
	return []planner.Operation{
		{
			Type:       planner.OpCopy,
			SourcePath: filepath.Join(fx.overlayRoot, "a.txt"),
			DestPath:   filepath.Join(fx.repoRoot, "bin", "run.sh"),
			RelPath:    "bin/run.sh",
		},
		{
			Type:       planner.OpCopy,
			SourcePath: filepath.Join(fx.overlayRoot, "a.txt"),
			DestPath:   filepath.Join(fx.repoRoot, "bin", "readonly.txt"),
			RelPath:    "bin/readonly.txt",
		},
		{Type: planner.OpRemove, DestPath: filepath.Join(fx.repoRoot, "tree"), RelPath: "tree"},
		{Type: planner.OpRemove, DestPath: filepath.Join(fx.repoRoot, "readonly-dir"), RelPath: "readonly-dir"},
	}
}

func TestOverlayTxn_RollbackRestoresPermissionModes(t *testing.T) {
	fx := newOverlayTxnFixture(t)
	want := fx.seedPermissionedUserTree(t)
	eng := fx.engine(t, nil, nil)

	err := eng.runOverlayTxn(context.Background(), overlayTxnRequest{
		kind:          overlayTxnApply,
		workspaceID:   fx.workspaceID,
		workspaceRoot: fx.repoRoot,
		ops:           permissionTxnOps(fx),
		finalize: func() (*state.WorkspaceState, bool, error) {
			// The destinations were replaced and removed by now.
			if _, statErr := os.Lstat(filepath.Join(fx.repoRoot, "tree")); !os.IsNotExist(statErr) {
				t.Errorf("tree still exists before rollback, stat err=%v", statErr)
			}
			return nil, false, errors.New("injected finalize failure")
		},
	})
	if err == nil || !strings.Contains(err.Error(), "injected finalize failure") {
		t.Fatalf("runOverlayTxn error = %v, want injected finalize failure", err)
	}

	requirePermissionModes(t, fx.repoRoot, want)
	if got := fx.readWorkspace(t, "bin/run.sh"); got != "#!/bin/sh\n" {
		t.Fatalf("bin/run.sh content = %q, want original", got)
	}
	if got := fx.readWorkspace(t, "tree/deep/leaf.sh"); got != "leaf\n" {
		t.Fatalf("tree/deep/leaf.sh content = %q, want original", got)
	}
	journalPath, txnDir, err := eng.overlayTxnPaths(fx.workspaceID)
	if err != nil {
		t.Fatalf("journal paths: %v", err)
	}
	for _, path := range []string{journalPath, txnDir} {
		if _, statErr := os.Lstat(path); !os.IsNotExist(statErr) {
			t.Fatalf("%s still exists after rollback, stat err=%v", path, statErr)
		}
	}
}

func TestOverlayTxn_InterruptedRecoveryRestoresPermissionModes(t *testing.T) {
	fx := newOverlayTxnFixture(t)
	want := fx.seedPermissionedUserTree(t)
	eng := fx.engine(t, nil, nil)
	journalPath, txnDir, err := eng.overlayTxnPaths(fx.workspaceID)
	if err != nil {
		t.Fatalf("journal paths: %v", err)
	}
	if err := os.MkdirAll(txnDir, 0700); err != nil {
		t.Fatalf("mkdir txn dir: %v", err)
	}

	// Simulate a process that died after swapping every destination.
	txn := overlayTxn{Kind: overlayTxnApply, WorkspaceID: fx.workspaceID, WorkspaceRoot: fx.repoRoot, Phase: overlayTxnPreparing}
	for seq, op := range permissionTxnOps(fx) {
		prepared, prepErr := eng.prepareOverlayOp(fx.repoRoot, txnDir, seq, op)
		if prepErr != nil {
			t.Fatalf("prepare %s: %v", op.RelPath, prepErr)
		}
		txn.Ops = append(txn.Ops, prepared)
	}
	txn.Phase = overlayTxnPrepared
	if err := eng.writeOverlayTxn(journalPath, &txn); err != nil {
		t.Fatalf("write journal: %v", err)
	}
	if err := eng.installOverlayTxn(context.Background(), &txn, fx.repoRoot, txnDir); err != nil {
		t.Fatalf("install: %v", err)
	}

	// A fresh engine stands in for the restarted process.
	if err := fx.engine(t, nil, nil).recoverOverlayTxn(context.Background(), fx.workspaceID, fx.repoRoot); err != nil {
		t.Fatalf("recoverOverlayTxn: %v", err)
	}
	requirePermissionModes(t, fx.repoRoot, want)
	if got := fx.readWorkspace(t, "bin/run.sh"); got != "#!/bin/sh\n" {
		t.Fatalf("bin/run.sh content = %q, want original", got)
	}
	for _, path := range []string{journalPath, txnDir} {
		if _, statErr := os.Lstat(path); !os.IsNotExist(statErr) {
			t.Fatalf("%s still exists after recovery, stat err=%v", path, statErr)
		}
	}
}
