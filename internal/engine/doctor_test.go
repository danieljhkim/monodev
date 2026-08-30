package engine

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/danieljhkim/monodev/internal/config"
	"github.com/danieljhkim/monodev/internal/fsops"
	"github.com/danieljhkim/monodev/internal/hash"
	"github.com/danieljhkim/monodev/internal/state"
	"github.com/danieljhkim/monodev/internal/stores"
)

func findDoctorFinding(findings []DoctorFinding, code string) *DoctorFinding {
	for i := range findings {
		if findings[i].Code == code {
			return &findings[i]
		}
	}
	return nil
}

func countDoctorProblems(findings []DoctorFinding) int {
	n := 0
	for _, f := range findings {
		if f.Severity == DoctorSeverityProblem && !f.Fixed {
			n++
		}
	}
	return n
}

// TestDoctor_PendingTransactionReportedAndRolledBack covers: a synthetic
// journal left in "prepared" phase is reported with its phase and recovery
// action and makes doctor exit non-zero (Healthy()==false); --fix rolls it
// back; a subsequent doctor run is healthy again.
func TestDoctor_PendingTransactionReportedAndRolledBack(t *testing.T) {
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
		Version:       overlayTxnVersion,
		Kind:          overlayTxnApply,
		WorkspaceID:   fx.workspaceID,
		WorkspaceRoot: fx.repoRoot,
		Phase:         overlayTxnPrepared,
	}); err != nil {
		t.Fatalf("write prepared journal: %v", err)
	}

	result, err := eng.Doctor(context.Background(), &DoctorRequest{CWD: fx.repoRoot})
	if err != nil {
		t.Fatalf("doctor: %v", err)
	}
	if result.Healthy() {
		t.Fatal("expected doctor to report the pending transaction as unhealthy")
	}
	finding := findDoctorFinding(result.Findings, DoctorPendingTransaction)
	if finding == nil {
		t.Fatal("expected a pending-transaction finding")
	}
	if finding.WorkspaceID != fx.workspaceID {
		t.Errorf("finding.WorkspaceID = %q, want %q", finding.WorkspaceID, fx.workspaceID)
	}
	if !strings.Contains(finding.Message, "prepared") {
		t.Errorf("finding.Message = %q, want it to mention phase %q", finding.Message, "prepared")
	}
	if finding.Recovery == "" || !finding.Fixable {
		t.Errorf("expected finding to be fixable with a recovery description, got %+v", finding)
	}

	fixResult, err := eng.Doctor(context.Background(), &DoctorRequest{CWD: fx.repoRoot, Fix: true})
	if err != nil {
		t.Fatalf("doctor --fix: %v", err)
	}
	fixedFinding := findDoctorFinding(fixResult.Findings, DoctorPendingTransaction)
	if fixedFinding == nil || !fixedFinding.Fixed || fixedFinding.FixError != "" {
		t.Fatalf("expected pending transaction to be fixed, got %+v", fixedFinding)
	}

	if _, err := os.Stat(journalPath); !os.IsNotExist(err) {
		t.Fatalf("journal still exists after fix, err=%v", err)
	}

	finalWs, err := store.LoadWorkspace(fx.workspaceID)
	if err != nil {
		t.Fatalf("load workspace after fix: %v", err)
	}
	if _, ok := finalWs.Paths["a.txt"]; !ok {
		t.Fatal("expected workspace ledger to still own a.txt after rollback")
	}

	finalResult, err := eng.Doctor(context.Background(), &DoctorRequest{CWD: fx.repoRoot})
	if err != nil {
		t.Fatalf("doctor after fix: %v", err)
	}
	if !finalResult.Healthy() {
		t.Fatalf("expected doctor to be healthy after fix, findings: %+v", finalResult.Findings)
	}
	if f := findDoctorFinding(finalResult.Findings, DoctorPendingTransaction); f != nil {
		t.Fatalf("expected no remaining pending-transaction finding, got %+v", f)
	}
}

// TestDoctor_OrphanedBackupDirectoryIsPrunedByFix covers a leftover .txn/
// directory with no journal: reported, and removed by --fix.
func TestDoctor_OrphanedBackupDirectoryIsPrunedByFix(t *testing.T) {
	fx := newOverlayTxnFixture(t, "a.txt")
	store := state.NewFileStateStore(fsops.NewRealFS(), fx.workspacesDir)
	eng := fx.engine(t, nil, store)

	_, txnDir, err := eng.overlayTxnPaths(fx.workspaceID)
	if err != nil {
		t.Fatalf("txn paths: %v", err)
	}
	if err := os.MkdirAll(txnDir, 0700); err != nil {
		t.Fatalf("seed orphaned txn dir: %v", err)
	}

	result, err := eng.Doctor(context.Background(), &DoctorRequest{CWD: fx.repoRoot})
	if err != nil {
		t.Fatalf("doctor: %v", err)
	}
	finding := findDoctorFinding(result.Findings, DoctorOrphanedBackup)
	if finding == nil {
		t.Fatal("expected an orphaned-backup finding")
	}

	fixResult, err := eng.Doctor(context.Background(), &DoctorRequest{CWD: fx.repoRoot, Fix: true})
	if err != nil {
		t.Fatalf("doctor --fix: %v", err)
	}
	fixedFinding := findDoctorFinding(fixResult.Findings, DoctorOrphanedBackup)
	if fixedFinding == nil || !fixedFinding.Fixed {
		t.Fatalf("expected orphaned backup to be fixed, got %+v", fixedFinding)
	}
	if _, err := os.Stat(txnDir); !os.IsNotExist(err) {
		t.Fatalf("txn dir still exists after fix, err=%v", err)
	}
}

// TestDoctor_LedgerEntryForDeletedStoreIsPrunedByFix covers: a ledger entry
// naming a deleted store is reported and pruned by --fix.
func TestDoctor_LedgerEntryForDeletedStoreIsPrunedByFix(t *testing.T) {
	repoRoot := t.TempDir()
	storesDir := t.TempDir()
	workspacesDir := filepath.Join(repoRoot, ".state")
	fs := fsops.NewRealFS()

	storeRepo := stores.NewFileStoreRepo(fs, storesDir)
	if err := storeRepo.Create("keep-store", stores.NewStoreMeta("keep", "global", time.Now())); err != nil {
		t.Fatalf("create store: %v", err)
	}

	stateStore := state.NewFileStateStore(fs, workspacesDir)
	workspaceID := state.ComputeWorkspaceID("fp1", ".")
	ws := state.NewWorkspaceState("fp1", ".", "copy")
	ws.Paths["a.txt"] = state.PathOwnership{Store: "keep-store", Type: "copy"}
	ws.Paths["ghost.txt"] = state.PathOwnership{Store: "ghost-store", Type: "copy"}
	ws.AddAppliedStore("keep-store", "copy")
	ws.AddAppliedStore("ghost-store", "copy")
	ws.Applied = true
	if err := stateStore.SaveWorkspace(workspaceID, ws); err != nil {
		t.Fatalf("seed workspace: %v", err)
	}

	eng := New(
		&trackGitRepo{root: repoRoot, fingerprint: "fp1", workspacePath: "."},
		storeRepo,
		stateStore,
		fs,
		hash.NewSHA256Hasher(),
		&mockClock{},
		config.Paths{Root: filepath.Join(repoRoot, ".monodev"), Stores: storesDir, Workspaces: workspacesDir},
	)

	result, err := eng.Doctor(context.Background(), &DoctorRequest{CWD: repoRoot})
	if err != nil {
		t.Fatalf("doctor: %v", err)
	}
	finding := findDoctorFinding(result.Findings, DoctorMissingStore)
	if finding == nil {
		t.Fatal("expected a missing-store finding")
	}
	if !strings.Contains(finding.Message, "ghost-store") || !strings.Contains(finding.Message, "ghost.txt") {
		t.Errorf("finding.Message = %q, want it to mention ghost-store and ghost.txt", finding.Message)
	}
	if finding.WorkspaceID != workspaceID {
		t.Errorf("finding.WorkspaceID = %q, want %q", finding.WorkspaceID, workspaceID)
	}

	fixResult, err := eng.Doctor(context.Background(), &DoctorRequest{CWD: repoRoot, Fix: true})
	if err != nil {
		t.Fatalf("doctor --fix: %v", err)
	}
	fixedFinding := findDoctorFinding(fixResult.Findings, DoctorMissingStore)
	if fixedFinding == nil || !fixedFinding.Fixed {
		t.Fatalf("expected missing-store finding to be fixed, got %+v", fixedFinding)
	}

	reloaded, err := stateStore.LoadWorkspace(workspaceID)
	if err != nil {
		t.Fatalf("reload workspace: %v", err)
	}
	if _, ok := reloaded.Paths["ghost.txt"]; ok {
		t.Fatal("expected ghost.txt to be pruned from the ledger")
	}
	if _, ok := reloaded.Paths["a.txt"]; !ok {
		t.Fatal("expected a.txt (owned by an existing store) to remain")
	}
	if reloaded.GetAppliedStore("ghost-store") != nil {
		t.Fatal("expected ghost-store to be pruned from AppliedStores")
	}
	if reloaded.GetAppliedStore("keep-store") == nil {
		t.Fatal("expected keep-store to remain in AppliedStores")
	}
}

// TestDoctor_StaleLockFileIsBenign covers: a stale .lock file is reported,
// but as an informational finding that never makes doctor unhealthy.
func TestDoctor_StaleLockFileIsBenign(t *testing.T) {
	fx := newOverlayTxnFixture(t)
	store := state.NewFileStateStore(fsops.NewRealFS(), fx.workspacesDir)
	eng := fx.engine(t, nil, store)

	locksDir := filepath.Join(fx.workspacesDir, ".locks")
	if err := os.MkdirAll(locksDir, 0700); err != nil {
		t.Fatalf("mkdir locks dir: %v", err)
	}
	lockPath := filepath.Join(locksDir, "some-workspace.lock")
	if err := os.WriteFile(lockPath, []byte{}, 0600); err != nil {
		t.Fatalf("seed stale lock file: %v", err)
	}

	result, err := eng.Doctor(context.Background(), &DoctorRequest{CWD: fx.repoRoot})
	if err != nil {
		t.Fatalf("doctor: %v", err)
	}
	finding := findDoctorFinding(result.Findings, DoctorStaleLock)
	if finding == nil {
		t.Fatal("expected a stale-lock finding")
	}
	if finding.Severity != DoctorSeverityInfo {
		t.Errorf("stale lock severity = %q, want %q", finding.Severity, DoctorSeverityInfo)
	}
	if countDoctorProblems(result.Findings) != 0 {
		t.Fatalf("stale lock file should not count as a problem, findings: %+v", result.Findings)
	}
	if !result.Healthy() {
		t.Fatal("a stale lock file alone should not make doctor unhealthy")
	}

	if _, err := os.Stat(lockPath); err != nil {
		t.Fatalf("doctor (read-only) should not remove lock files: %v", err)
	}
}

// TestDoctor_HealthyWorkspaceExitsClean covers: doctor on a healthy
// workspace reports no problems.
func TestDoctor_HealthyWorkspaceExitsClean(t *testing.T) {
	fx := newOverlayTxnFixture(t, "a.txt")
	fx.seedApplied(t)

	store := state.NewFileStateStore(fsops.NewRealFS(), fx.workspacesDir)
	eng := fx.engine(t, nil, store)

	result, err := eng.Doctor(context.Background(), &DoctorRequest{CWD: fx.repoRoot})
	if err != nil {
		t.Fatalf("doctor: %v", err)
	}
	if !result.Healthy() {
		t.Fatalf("expected a healthy workspace to report no problems, findings: %+v", result.Findings)
	}
	if countDoctorProblems(result.Findings) != 0 {
		t.Fatalf("expected zero problem findings, got: %+v", result.Findings)
	}
}

// TestDoctor_ManagedExcludeDriftReportedAndFixed covers: doctor detects
// drift between .git/info/exclude and the ledger, and --fix reconciles it.
func TestDoctor_ManagedExcludeDriftReportedAndFixed(t *testing.T) {
	fx := newOverlayTxnFixture(t, "a.txt")
	store := state.NewFileStateStore(fsops.NewRealFS(), fx.workspacesDir)
	ws := state.NewWorkspaceState("fp1", ".", "copy")
	ws.Paths["a.txt"] = state.PathOwnership{Store: fx.storeID, Type: "copy"}
	if err := store.SaveWorkspace(fx.workspaceID, ws); err != nil {
		t.Fatalf("seed workspace state: %v", err)
	}
	eng := fx.engine(t, nil, store)

	excludePath := filepath.Join(fx.repoRoot, ".git", "info", "exclude")
	if err := os.MkdirAll(filepath.Dir(excludePath), 0700); err != nil {
		t.Fatalf("create git info dir: %v", err)
	}
	staleBlock := "# >>> monodev managed block — do not edit <<<\n/stale.txt\n# <<< monodev managed block <<<\n"
	if err := os.WriteFile(excludePath, []byte(staleBlock), 0600); err != nil {
		t.Fatalf("seed stale exclude block: %v", err)
	}

	result, err := eng.Doctor(context.Background(), &DoctorRequest{CWD: fx.repoRoot})
	if err != nil {
		t.Fatalf("doctor: %v", err)
	}
	finding := findDoctorFinding(result.Findings, DoctorExcludeDrift)
	if finding == nil {
		t.Fatal("expected an exclude-drift finding")
	}
	if !finding.Fixable {
		t.Fatal("expected exclude drift to be fixable")
	}

	fixResult, err := eng.Doctor(context.Background(), &DoctorRequest{CWD: fx.repoRoot, Fix: true})
	if err != nil {
		t.Fatalf("doctor --fix: %v", err)
	}
	fixedFinding := findDoctorFinding(fixResult.Findings, DoctorExcludeDrift)
	if fixedFinding == nil || !fixedFinding.Fixed {
		t.Fatalf("expected exclude drift to be fixed, got %+v", fixedFinding)
	}

	contents, err := os.ReadFile(excludePath)
	if err != nil {
		t.Fatalf("read reconciled exclude file: %v", err)
	}
	want := "# >>> monodev managed block — do not edit <<<\n/a.txt\n# <<< monodev managed block <<<\n"
	if string(contents) != want {
		t.Fatalf("reconciled exclude file = %q, want %q", contents, want)
	}

	finalResult, err := eng.Doctor(context.Background(), &DoctorRequest{CWD: fx.repoRoot})
	if err != nil {
		t.Fatalf("doctor after fix: %v", err)
	}
	if f := findDoctorFinding(finalResult.Findings, DoctorExcludeDrift); f != nil {
		t.Fatalf("expected no remaining exclude-drift finding, got %+v", f)
	}
}

// TestDoctor_MissingCheckoutIsReportedOnly covers: a workspace whose
// checkout directory no longer exists is reported but never auto-fixed.
func TestDoctor_MissingCheckoutIsReportedOnly(t *testing.T) {
	repoRoot := t.TempDir()
	storesDir := t.TempDir()
	workspacesDir := filepath.Join(repoRoot, ".state")
	fs := fsops.NewRealFS()

	storeRepo := stores.NewFileStoreRepo(fs, storesDir)
	stateStore := state.NewFileStateStore(fs, workspacesDir)

	deletedCheckout := filepath.Join(t.TempDir(), "deleted-checkout")
	workspaceID := state.ComputeWorkspaceID("fp2", ".")
	ws := state.NewWorkspaceState("fp2", ".", "copy")
	ws.AbsolutePath = deletedCheckout
	if err := stateStore.SaveWorkspace(workspaceID, ws); err != nil {
		t.Fatalf("seed workspace: %v", err)
	}

	eng := New(
		&trackGitRepo{root: repoRoot, fingerprint: "fp1", workspacePath: "."},
		storeRepo,
		stateStore,
		fs,
		hash.NewSHA256Hasher(),
		&mockClock{},
		config.Paths{Root: filepath.Join(repoRoot, ".monodev"), Stores: storesDir, Workspaces: workspacesDir},
	)

	result, err := eng.Doctor(context.Background(), &DoctorRequest{CWD: repoRoot, Fix: true})
	if err != nil {
		t.Fatalf("doctor --fix: %v", err)
	}
	finding := findDoctorFinding(result.Findings, DoctorMissingCheckout)
	if finding == nil {
		t.Fatal("expected a missing-checkout finding")
	}
	if finding.Fixable {
		t.Fatal("missing-checkout should not be auto-fixable")
	}
	if finding.Fixed {
		t.Fatal("missing-checkout should never be marked fixed")
	}
	if result.Healthy() {
		t.Fatal("a missing checkout should make doctor unhealthy")
	}
}
