package engine

import (
	"context"
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

func (fx overlayTxnFixture) withStack(t *testing.T, store state.StateStore) {
	t.Helper()
	ws, err := store.LoadWorkspace(fx.workspaceID)
	if os.IsNotExist(err) {
		ws = state.NewWorkspaceState("fp1", ".", "copy")
	} else if err != nil {
		t.Fatalf("load workspace for stack seed: %v", err)
	}
	ws.Stack = []string{fx.storeID}
	if err := store.SaveWorkspace(fx.workspaceID, ws); err != nil {
		t.Fatalf("seed stack workspace: %v", err)
	}
}

func (fx overlayTxnFixture) seedApplied(t *testing.T) {
	t.Helper()
	store := state.NewFileStateStore(fsops.NewRealFS(), fx.workspacesDir)
	eng := fx.engine(t, nil, store)
	if _, err := eng.Apply(context.Background(), &ApplyRequest{CWD: fx.repoRoot, StoreID: fx.storeID, Mode: "copy", Force: true}); err != nil {
		t.Fatalf("seed apply: %v", err)
	}
}

func (fx overlayTxnFixture) seedStackApplied(t *testing.T) {
	t.Helper()
	store := state.NewFileStateStore(fsops.NewRealFS(), fx.workspacesDir)
	fx.withStack(t, store)
	eng := fx.engine(t, nil, store)
	if _, err := eng.StackApply(context.Background(), &StackApplyRequest{CWD: fx.repoRoot, Mode: "copy", Force: true}); err != nil {
		t.Fatalf("seed stack apply: %v", err)
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
	kinds := []string{overlayTxnApply, overlayTxnUnapply, overlayTxnStackApply, overlayTxnStackUnapply}
	for _, kind := range kinds {
		t.Run(kind, func(t *testing.T) {
			recovered := false
			for failAt := 1; failAt <= 40; failAt++ {
				fx := newOverlayTxnFixture(t)
				fx.requireUserFile(t, "a.txt", "user-original")
				seedStore := state.NewFileStateStore(fsops.NewRealFS(), fx.workspacesDir)
				switch kind {
				case overlayTxnUnapply:
					fx.seedApplied(t)
				case overlayTxnStackApply:
					fx.withStack(t, seedStore)
				case overlayTxnStackUnapply:
					fx.seedStackApplied(t)
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
				if kind == overlayTxnStackApply {
					fx.withStack(t, cleanStore)
				}
				clean := fx.engine(t, nil, cleanStore)
				if recoverErr := runOverlayKind(t, clean, fx, kind); recoverErr != nil {
					if (kind != overlayTxnUnapply && kind != overlayTxnStackUnapply) || !errors.Is(recoverErr, ErrStateMissing) {
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
	kinds := []string{overlayTxnApply, overlayTxnUnapply, overlayTxnStackApply, overlayTxnStackUnapply}
	for _, kind := range kinds {
		t.Run(kind, func(t *testing.T) {
			fx := newOverlayTxnFixture(t)
			base := state.NewFileStateStore(fsops.NewRealFS(), fx.workspacesDir)
			switch kind {
			case overlayTxnUnapply:
				fx.seedApplied(t)
			case overlayTxnStackApply:
				fx.withStack(t, base)
			case overlayTxnStackUnapply:
				fx.seedStackApplied(t)
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
			if kind == overlayTxnStackApply {
				fx.withStack(t, cleanStore)
			}
			clean := fx.engine(t, nil, cleanStore)
			if recoverErr := runOverlayKind(t, clean, fx, kind); recoverErr != nil {
				if (kind != overlayTxnUnapply && kind != overlayTxnStackUnapply) || !errors.Is(recoverErr, ErrStateMissing) {
					t.Fatalf("recovery: %v", recoverErr)
				}
			}
			if kind == overlayTxnApply || kind == overlayTxnStackApply {
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
	_, err := eng.Apply(ctx, &ApplyRequest{CWD: fx.repoRoot, StoreID: fx.storeID, Mode: "copy", Force: true})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Apply error = %v, want context.Canceled", err)
	}

	got := fx.readWorkspace(t, "a.txt")
	if got != "user-original" && got != "overlay content" {
		t.Fatalf("destination after cancel = %q, want original or fully applied content", got)
	}

	clean := fx.engine(t, nil, state.NewFileStateStore(fsops.NewRealFS(), fx.workspacesDir))
	if _, err := clean.Apply(context.Background(), &ApplyRequest{CWD: fx.repoRoot, StoreID: fx.storeID, Mode: "copy", Force: true}); err != nil {
		t.Fatalf("recovery apply: %v", err)
	}
	fx.requireCoherentApplied(t, state.NewFileStateStore(fsops.NewRealFS(), fx.workspacesDir))
}

func TestOverlayTxn_DryRunDoesNotMutate(t *testing.T) {
	fx := newOverlayTxnFixture(t)
	fx.requireUserFile(t, "a.txt", "user-original")
	store := state.NewFileStateStore(fsops.NewRealFS(), fx.workspacesDir)
	eng := fx.engine(t, nil, store)
	if _, err := eng.Apply(context.Background(), &ApplyRequest{CWD: fx.repoRoot, StoreID: fx.storeID, Mode: "copy", DryRun: true, Force: true}); err != nil {
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
	if _, err := eng.Apply(context.Background(), &ApplyRequest{CWD: fx.repoRoot, StoreID: fx.storeID, Mode: "copy"}); err != nil {
		t.Fatalf("apply: %v", err)
	}
	fx.requireCoherentApplied(t, store)
	if _, err := os.Stat(filepath.Join(fx.workspacesDir, fx.workspaceID+".txn.json")); !os.IsNotExist(err) {
		t.Fatal("successful apply left a transaction journal")
	}
}

func runOverlayKind(t *testing.T, eng *Engine, fx overlayTxnFixture, kind string) error {
	t.Helper()
	switch kind {
	case overlayTxnApply:
		_, err := eng.Apply(context.Background(), &ApplyRequest{CWD: fx.repoRoot, StoreID: fx.storeID, Mode: "copy", Force: true})
		return err
	case overlayTxnUnapply:
		_, err := eng.Unapply(context.Background(), &UnapplyRequest{CWD: fx.repoRoot})
		return err
	case overlayTxnStackApply:
		_, err := eng.StackApply(context.Background(), &StackApplyRequest{CWD: fx.repoRoot, Mode: "copy", Force: true})
		return err
	case overlayTxnStackUnapply:
		_, err := eng.StackUnapply(context.Background(), &StackUnapplyRequest{CWD: fx.repoRoot})
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
