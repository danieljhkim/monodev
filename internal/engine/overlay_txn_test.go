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
