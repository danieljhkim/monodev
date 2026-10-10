package engine

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/danieljhkim/monodev/internal/fsops"
	"github.com/danieljhkim/monodev/internal/hash"
	"github.com/danieljhkim/monodev/internal/lockfile"
	"github.com/danieljhkim/monodev/internal/state"
	"github.com/danieljhkim/monodev/internal/stores"
)

func newWorkspaceOwnerEngine(t *testing.T) (*Engine, string, string) {
	t.Helper()
	e, _, component := newScopedWorkspaceStateTestEngine(t)
	root := e.scopedPaths.RepoRoot
	e.gitRepo = &trackGitRepo{root: root, fingerprint: "fp1", workspacePath: "."}
	e.storeResolver = newScopedEngineStoreResolver(e.fs, e.scopedPaths)
	e.hasher = hash.NewSHA256Hasher()
	e.clock = &mockClock{}
	repo := e.storeResolver.component
	if err := repo.Create("local", stores.NewStoreMeta("local", e.clock.Now())); err != nil {
		t.Fatal(err)
	}
	if err := repo.Create("prior", stores.NewStoreMeta("prior", e.clock.Now())); err != nil {
		t.Fatal(err)
	}
	writeOverlayFile(t, repo.OverlayRoot("local"), "new.txt")
	track := stores.NewTrackFile()
	track.Tracked = append(track.Tracked, stores.TrackedPath{Path: "new.txt", Kind: "file"})
	if err := repo.SaveTrack("local", track); err != nil {
		t.Fatal(err)
	}
	id := state.ComputeWorkspaceID("fp1", ".")
	ws := state.NewWorkspaceState("fp1", ".", "copy")
	ws.AbsolutePath = root
	ws.ActiveStore = "local"
	ws.ActiveStoreScope = stores.ScopeComponent
	ws.Applied = true
	ws.AddAppliedStore("prior", "copy")
	ws.Paths["existing.txt"] = state.PathOwnership{Store: "prior", Type: "copy"}
	if err := component.SaveWorkspace(id, ws); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "existing.txt"), []byte("existing"), 0600); err != nil {
		t.Fatal(err)
	}
	return e, root, id
}

func requireNoGlobalWorkspace(t *testing.T, e *Engine, id string) {
	t.Helper()
	if _, err := e.stateStore.LoadWorkspace(id); !os.IsNotExist(err) {
		t.Fatalf("unexpected global workspace: %v", err)
	}
}

func TestWorkspaceOwnerLockContention(t *testing.T) {
	e, root, id := newWorkspaceOwnerEngine(t)
	component := e.componentStateStore.(*state.FileStateStore)
	held, err := component.LockWorkspace(context.Background(), id, lockfile.Exclusive)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := held.Close(); err != nil {
			t.Error(err)
		}
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	_, unlock, err := e.lockWorkspaceIdentity(ctx, root, "fp1", ".", lockfile.Exclusive)
	if err == nil {
		unlock()
		t.Fatal("mutation did not wait for component owner lock")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("lock contention = %v", err)
	}
}

func TestWorkspaceOwnerApplyAndGlobalPrecedence(t *testing.T) {
	for _, duplicate := range []bool{false, true} {
		t.Run(map[bool]string{false: "component-only", true: "global-precedence"}[duplicate], func(t *testing.T) {
			e, root, id := newWorkspaceOwnerEngine(t)
			owner := e.componentStateStore
			componentPath := filepath.Join(e.scopedPaths.Component.Workspaces, id+".json")
			before, err := os.ReadFile(componentPath)
			if err != nil {
				t.Fatal(err)
			}
			if duplicate {
				ws, err := owner.LoadWorkspace(id)
				if err != nil {
					t.Fatal(err)
				}
				ws.Paths["global.txt"] = state.PathOwnership{Store: "global-prior", Type: "copy"}
				ws.AddAppliedStore("global-prior", "copy")
				if err := e.stateStore.SaveWorkspace(id, ws); err != nil {
					t.Fatal(err)
				}
				owner = e.stateStore
			}
			preview, err := e.Apply(context.Background(), &ApplyRequest{CWD: root, Mode: "copy", DryRun: true})
			if err != nil {
				t.Fatalf("preview: %v", err)
			}
			actual, err := e.Apply(context.Background(), &ApplyRequest{CWD: root, Mode: "copy"})
			if err != nil {
				t.Fatalf("apply: %v", err)
			}
			if preview.WorkspaceID != actual.WorkspaceID || !reflect.DeepEqual(preview.Plan.Operations, actual.Plan.Operations) {
				t.Fatalf("preview and apply differ: %#v / %#v", preview, actual)
			}
			ws, err := owner.LoadWorkspace(id)
			if err != nil {
				t.Fatal(err)
			}
			if ws.ActiveStore != "local" || ws.ActiveStoreScope != stores.ScopeComponent || ws.Paths["existing.txt"].Store != "prior" || ws.Paths["new.txt"].Store != "local" || ws.GetAppliedStore("prior") == nil || ws.GetAppliedStore("local") == nil {
				t.Fatalf("lost workspace state: %#v", ws)
			}
			if duplicate {
				after, err := os.ReadFile(componentPath)
				if err != nil || string(after) != string(before) {
					t.Fatalf("component duplicate changed: %v", err)
				}
				if ws.Paths["global.txt"].Store != "global-prior" {
					t.Fatal("global state did not win")
				}
			} else {
				requireNoGlobalWorkspace(t, e, id)
				if _, err := os.Stat(filepath.Join(e.scopedPaths.Component.Workspaces, ".locks", id+".lock")); err != nil {
					t.Fatalf("component owner was not locked: %v", err)
				}
			}
		})
	}
}

func TestWorkspaceOwnerMutationPaths(t *testing.T) {
	for _, operation := range []string{"commit", "track", "untrack", "use", "create", "unapply", "eject", "active", "status", "diff", "discover"} {
		t.Run(operation, func(t *testing.T) {
			e, root, id := newWorkspaceOwnerEngine(t)
			if _, err := e.Apply(context.Background(), &ApplyRequest{CWD: root, Mode: "copy"}); err != nil {
				t.Fatal(err)
			}
			ctx := context.Background()
			var err error
			switch operation {
			case "commit":
				_, err = e.Commit(ctx, &CommitRequest{CWD: root, All: true})
			case "track":
				_, err = e.Track(ctx, &TrackRequest{CWD: root, Paths: []string{"existing.txt"}})
			case "untrack":
				_, err = e.Untrack(ctx, &UntrackRequest{CWD: root, Paths: []string{"new.txt"}})
			case "use":
				err = e.storeResolver.component.Create("second", stores.NewStoreMeta("second", e.clock.Now()))
				if err == nil {
					err = e.UseStore(ctx, &UseStoreRequest{CWD: root, StoreID: "second", Scope: stores.ScopeComponent})
				}
			case "create":
				err = e.CreateStore(ctx, &CreateStoreRequest{CWD: root, StoreID: "second", Name: "second", Scope: stores.ScopeComponent})
			case "unapply":
				_, err = e.Unapply(ctx, &UnapplyRequest{CWD: root, All: true, Force: true})
			case "eject":
				_, err = e.Eject(ctx, &EjectRequest{CWD: root})
			case "active":
				var active, scope string
				active, scope, err = e.GetActiveStoreID(ctx, root)
				if active != "local" || scope != stores.ScopeComponent {
					t.Fatalf("active = %q, %q", active, scope)
				}
			case "status":
				_, err = e.Status(ctx, &StatusRequest{CWD: root})
			case "diff":
				_, err = e.Diff(ctx, &DiffRequest{CWD: root})
			case "discover":
				_, err = e.DiscoverNewTracked(ctx, &DiscoverNewTrackedRequest{CWD: root})
			}
			if err != nil {
				t.Fatal(err)
			}
			requireNoGlobalWorkspace(t, e, id)
			ws, loadErr := e.componentStateStore.LoadWorkspace(id)
			if operation == "eject" {
				if !os.IsNotExist(loadErr) {
					t.Fatalf("component ledger not deleted: %v", loadErr)
				}
			} else {
				if loadErr != nil {
					t.Fatal(loadErr)
				}
				if (operation == "use" || operation == "create") && ws.ActiveStore != "second" {
					t.Fatalf("mutation was not saved to component: %#v", ws)
				}
				if operation == "unapply" && len(ws.Paths) != 0 {
					t.Fatalf("unapply not saved: %#v", ws)
				}
			}
		})
	}
}

func TestWorkspaceOwnerCommittedRecovery(t *testing.T) {
	e, root, id := newWorkspaceOwnerEngine(t)
	component := e.componentStateStore.(*state.FileStateStore)
	e.componentStateStore = &failSaveStore{FileStateStore: component, fail: true}
	if _, err := e.Apply(context.Background(), &ApplyRequest{CWD: root, Mode: "copy"}); err == nil || !strings.Contains(err.Error(), "injected state save failure") {
		t.Fatalf("apply = %v", err)
	}
	journal, _, err := e.overlayTxnPaths(id)
	if err != nil {
		t.Fatal(err)
	}
	txn, err := e.loadOverlayTxn(journal)
	if err != nil {
		t.Fatal(err)
	}
	if txn.Phase != overlayTxnCommitted || txn.StateScope != stores.ScopeComponent {
		t.Fatalf("journal = %#v", txn)
	}
	// A fresh engine must recover solely from durable bytes, even without a ledger.
	if err := component.DeleteWorkspace(id); err != nil {
		t.Fatal(err)
	}
	fresh := NewScoped(e.gitRepo, e.scopedPaths, fsops.NewRealFS(), e.hasher, e.clock)
	report, err := fresh.Doctor(context.Background(), &DoctorRequest{Fix: true})
	if err != nil {
		t.Fatalf("doctor recovery: %v", err)
	}
	fixed := false
	for _, finding := range report.Findings {
		if finding.Code == DoctorPendingTransaction && finding.WorkspaceID == id && finding.Fixed {
			fixed = true
		}
	}
	if !fixed {
		t.Fatalf("doctor did not recover component transaction: %#v", report)
	}
	ws, err := fresh.componentStateStore.LoadWorkspace(id)
	if err != nil || ws.Paths["new.txt"].Store != "local" || ws.Paths["existing.txt"].Store != "prior" {
		t.Fatalf("recovered state = %#v, %v", ws, err)
	}
	requireNoGlobalWorkspace(t, fresh, id)
	if _, err := os.Stat(journal); !os.IsNotExist(err) {
		t.Fatalf("journal remains: %v", err)
	}
	if err := fresh.recoverOverlayTxn(context.Background(), id, root); err != nil {
		t.Fatalf("second recovery: %v", err)
	}
}

func TestWorkspaceOwnerJournalScopeAndDryRun(t *testing.T) {
	e, root, id := newWorkspaceOwnerEngine(t)
	journal, _, err := e.overlayTxnPaths(id)
	if err != nil {
		t.Fatal(err)
	}
	// A version-2 journal must not be migrated by preview or shared-lock reads.
	data := []byte(`{"schemaVersion":2,"kind":"apply","phase":"preparing","ops":[]}`)
	if err := os.WriteFile(journal, data, 0600); err != nil {
		t.Fatal(err)
	}
	listed, err := e.ListWorkspaces(context.Background())
	if err != nil || len(listed.Workspaces) != 1 || listed.Workspaces[0].WorkspaceID != id {
		t.Fatalf("workspace scan included the journal: %#v, %v", listed, err)
	}
	if _, err := e.Apply(context.Background(), &ApplyRequest{CWD: root, Mode: "copy", DryRun: true}); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(journal)
	if err != nil || string(after) != string(data) {
		t.Fatalf("preview rewrote journal: %v", err)
	}
	txn := overlayTxn{StateScope: stores.ScopeGlobal, Phase: overlayTxnCommitted}
	if err := e.writeOverlayTxn(journal, &txn); err != nil {
		t.Fatal(err)
	}
	if _, err := e.Apply(context.Background(), &ApplyRequest{CWD: root, Mode: "copy"}); err == nil || !strings.Contains(err.Error(), "owner conflicts") {
		t.Fatalf("conflicting journal accepted: %v", err)
	}
	// Global precedence also applies when recovering a legacy scopeless journal.
	ws, err := e.componentStateStore.LoadWorkspace(id)
	if err != nil {
		t.Fatal(err)
	}
	if err := e.stateStore.SaveWorkspace(id, ws); err != nil {
		t.Fatal(err)
	}
	txn.StateScope = ""
	txn.FinalState = state.CloneWorkspaceState(ws)
	txn.FinalState.ActiveStore = "recovered-global"
	if err := e.writeOverlayTxn(journal, &txn); err != nil {
		t.Fatal(err)
	}
	unlock, err := e.lockWorkspace(context.Background(), id, lockfile.Exclusive)
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	if err := e.recoverOverlayTxn(context.Background(), id, root); err != nil {
		t.Fatal(err)
	}
	global, err := e.stateStore.LoadWorkspace(id)
	if err != nil || global.ActiveStore != "recovered-global" {
		t.Fatalf("global recovery = %#v, %v", global, err)
	}
	component, err := e.componentStateStore.LoadWorkspace(id)
	if err != nil || component.ActiveStore != "local" {
		t.Fatalf("component duplicate changed = %#v, %v", component, err)
	}
	// The migrated journal still used the version boundary while on disk.
	encoded, err := json.Marshal(txn)
	if err != nil || !strings.Contains(string(encoded), `"schemaVersion":3`) {
		t.Fatalf("journal schema = %s, %v", encoded, err)
	}
}
