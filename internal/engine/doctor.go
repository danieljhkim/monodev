package engine

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/danieljhkim/monodev/internal/lockfile"
	"github.com/danieljhkim/monodev/internal/remote"
	"github.com/danieljhkim/monodev/internal/state"
)

// Doctor finding codes identify the kind of issue a check surfaced.
const (
	DoctorPendingTransaction = "pending-transaction"
	DoctorOrphanedBackup     = "orphaned-backup"
	DoctorMissingStore       = "missing-store"
	DoctorMissingCheckout    = "missing-checkout"
	DoctorStaleLock          = "stale-lock"
	DoctorExcludeDrift       = "exclude-drift"
	DoctorUnknownRemote      = "unknown-remote"
)

// DoctorSeverity classifies whether a finding should fail doctor's exit code.
type DoctorSeverity string

const (
	// DoctorSeverityProblem marks an actionable inconsistency.
	DoctorSeverityProblem DoctorSeverity = "problem"
	// DoctorSeverityInfo marks a benign observation, such as a lock file that
	// is not itself evidence of a currently held lock.
	DoctorSeverityInfo DoctorSeverity = "info"
)

// DoctorRequest configures a health-check pass over monodev's local state.
type DoctorRequest struct {
	// CWD resolves the current repository for repo-scoped checks (remote
	// configuration, managed excludes). Global checks run regardless.
	CWD string

	// Fix applies the safe repairs for every fixable finding.
	Fix bool
}

// DoctorFinding is one diagnosed issue, or a benign observation worth
// surfacing to the user.
type DoctorFinding struct {
	Code        string
	Severity    DoctorSeverity
	WorkspaceID string `json:",omitempty"`
	Message     string
	Recovery    string
	Fixable     bool
	Fixed       bool
	FixError    string `json:",omitempty"`
}

// DoctorResult reports every finding from one doctor pass.
type DoctorResult struct {
	Findings []DoctorFinding
	Fixed    bool
}

// Healthy reports whether any unresolved problem-severity finding remains.
// Benign info findings (e.g. stale lock files) never affect this.
func (r *DoctorResult) Healthy() bool {
	for _, f := range r.Findings {
		if f.Severity == DoctorSeverityProblem && !f.Fixed {
			return false
		}
	}
	return true
}

// Doctor inspects monodev's on-disk state for drift and interrupted
// transactions, optionally applying the safe repairs.
func (e *Engine) Doctor(ctx context.Context, req *DoctorRequest) (*DoctorResult, error) {
	if err := checkContext(ctx); err != nil {
		return nil, err
	}
	if req == nil {
		req = &DoctorRequest{}
	}

	result := &DoctorResult{Fixed: req.Fix}

	txnFindings, err := e.doctorScanTransactions(ctx, req.Fix)
	if err != nil {
		return nil, err
	}
	result.Findings = append(result.Findings, txnFindings...)

	stateFindings, err := e.doctorScanWorkspaceStates(ctx, req.Fix)
	if err != nil {
		return nil, err
	}
	result.Findings = append(result.Findings, stateFindings...)

	result.Findings = append(result.Findings, e.doctorScanLocks()...)

	if strings.TrimSpace(req.CWD) != "" {
		repoFindings, err := e.doctorScanRepo(ctx, req.CWD, req.Fix)
		if err != nil {
			return nil, err
		}
		result.Findings = append(result.Findings, repoFindings...)
	}

	return result, nil
}

// doctorScanTransactions finds pending overlay transaction journals and
// orphaned backup directories. Journals always live beside the global
// workspaces directory (see overlayTxnPaths), regardless of which scope a
// workspace's own state belongs to.
func (e *Engine) doctorScanTransactions(ctx context.Context, fix bool) ([]DoctorFinding, error) {
	dir := e.configPaths.Workspaces
	if strings.TrimSpace(dir) == "" {
		return nil, nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to read workspaces directory: %w", err)
	}

	hasJournal := make(map[string]bool)
	hasTxnDir := make(map[string]bool)
	for _, entry := range entries {
		name := entry.Name()
		switch {
		case entry.IsDir():
			if id, ok := strings.CutSuffix(name, ".txn"); ok {
				hasTxnDir[id] = true
			}
		case strings.HasSuffix(name, ".txn.json"):
			hasJournal[strings.TrimSuffix(name, ".txn.json")] = true
		}
	}

	ids := make([]string, 0, len(hasJournal)+len(hasTxnDir))
	seen := make(map[string]bool)
	for id := range hasJournal {
		if !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}
	for id := range hasTxnDir {
		if !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)

	var findings []DoctorFinding
	for _, id := range ids {
		if hasJournal[id] {
			finding, err := e.doctorCheckTransaction(ctx, id, fix)
			if err != nil {
				return nil, err
			}
			if finding != nil {
				findings = append(findings, *finding)
			}
			continue
		}
		finding, err := e.doctorCheckOrphanedBackup(id, fix)
		if err != nil {
			return nil, err
		}
		if finding != nil {
			findings = append(findings, *finding)
		}
	}
	return findings, nil
}

// doctorCheckTransaction peeks a workspace's journal without recovering it,
// then recovers it under an exclusive lock when fix is requested.
func (e *Engine) doctorCheckTransaction(ctx context.Context, id string, fix bool) (*DoctorFinding, error) {
	journalPath, _, err := e.overlayTxnPaths(id)
	if err != nil {
		return nil, err
	}

	unlock, err := e.lockWorkspace(ctx, id, lockfile.Shared)
	if err != nil {
		return nil, err
	}
	txn, err := e.loadOverlayTxn(journalPath)
	unlock()
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	finding := &DoctorFinding{
		Code:        DoctorPendingTransaction,
		Severity:    DoctorSeverityProblem,
		WorkspaceID: id,
		Message:     fmt.Sprintf("workspace %s has an interrupted %s transaction in phase %q", id, txn.Kind, txn.Phase),
		Recovery:    doctorTxnRecoveryText(txn.Phase),
		Fixable:     true,
	}

	if fix {
		unlockEx, err := e.lockWorkspace(ctx, id, lockfile.Exclusive)
		if err != nil {
			finding.FixError = err.Error()
			return finding, nil
		}
		defer unlockEx()
		if err := e.recoverOverlayTxn(ctx, id, txn.WorkspaceRoot); err != nil {
			finding.FixError = err.Error()
		} else {
			finding.Fixed = true
		}
	}

	return finding, nil
}

func doctorTxnRecoveryText(phase string) string {
	switch phase {
	case overlayTxnPreparing, "":
		return "run `monodev doctor --fix` to discard the journal; no destination files were changed"
	case overlayTxnPrepared:
		return "run `monodev doctor --fix` to restore the original files from backup"
	case overlayTxnCommitted:
		return "run `monodev doctor --fix` to finish saving the workspace ledger and discard the backup"
	default:
		return "run `monodev doctor --fix` to recover this transaction"
	}
}

// doctorCheckOrphanedBackup reports a txn backup directory with no journal.
// This is always safe to remove: the only way a directory can exist without
// a journal is a crash between creating it and writing the initial journal,
// or a completed recovery that removed the journal but not the directory.
func (e *Engine) doctorCheckOrphanedBackup(id string, fix bool) (*DoctorFinding, error) {
	_, txnDir, err := e.overlayTxnPaths(id)
	if err != nil {
		return nil, err
	}
	finding := &DoctorFinding{
		Code:        DoctorOrphanedBackup,
		Severity:    DoctorSeverityProblem,
		WorkspaceID: id,
		Message:     fmt.Sprintf("workspace %s has a leftover transaction backup directory with no journal (%s)", id, txnDir),
		Recovery:    "run `monodev doctor --fix` to remove the orphaned backup directory",
		Fixable:     true,
	}
	if fix {
		if err := e.fs.RemoveAll(txnDir); err != nil && !os.IsNotExist(err) {
			finding.FixError = err.Error()
		} else {
			finding.Fixed = true
		}
	}
	return finding, nil
}

// doctorScanWorkspaceStates walks every known workspace ledger (both global
// and component scopes) checking for a missing checkout directory and
// ledger entries owned by a store that no longer exists.
func (e *Engine) doctorScanWorkspaceStates(ctx context.Context, fix bool) ([]DoctorFinding, error) {
	var findings []DoctorFinding
	seen := make(map[string]bool)

	for _, source := range e.workspaceStateSources() {
		entries, err := os.ReadDir(source.dir)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, fmt.Errorf("failed to read workspaces directory: %w", err)
		}

		ids := make([]string, 0, len(entries))
		for _, entry := range entries {
			if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
				continue
			}
			ids = append(ids, strings.TrimSuffix(entry.Name(), ".json"))
		}
		sort.Strings(ids)

		for _, id := range ids {
			if seen[id] {
				continue
			}
			seen[id] = true

			unlock, err := lockWorkspace(ctx, source.store, id, lockfile.Shared)
			if err != nil {
				return nil, err
			}
			ws, err := source.store.LoadWorkspace(id)
			unlock()
			if err != nil {
				if os.IsNotExist(err) {
					continue
				}
				return nil, fmt.Errorf("failed to load workspace %s: %w", id, err)
			}
			ws.MigrateDeprecatedStack()

			findings = append(findings, e.doctorCheckCheckout(id, ws)...)

			storeFindings, err := e.doctorCheckLedgerStores(ctx, id, source.store, ws, fix)
			if err != nil {
				return nil, err
			}
			findings = append(findings, storeFindings...)
		}
	}

	return findings, nil
}

// doctorCheckCheckout reports a workspace whose checkout directory no
// longer exists. This is report-only: monodev cannot know whether the
// checkout was deleted intentionally.
func (e *Engine) doctorCheckCheckout(id string, ws *state.WorkspaceState) []DoctorFinding {
	path := strings.TrimSpace(ws.AbsolutePath)
	if path == "" {
		return nil
	}
	if info, err := os.Stat(path); err == nil && info.IsDir() {
		return nil
	}
	return []DoctorFinding{{
		Code:        DoctorMissingCheckout,
		Severity:    DoctorSeverityProblem,
		WorkspaceID: id,
		Message:     fmt.Sprintf("workspace %s points at checkout directory %s, which no longer exists", id, path),
		Recovery:    fmt.Sprintf("once you have confirmed the checkout is gone for good, remove the stale record with `monodev workspace rm %s --force`", id),
		Fixable:     false,
	}}
}

// doctorCheckLedgerStores reports (and, with fix, prunes) ledger entries
// owned by a store that no longer exists in any scope.
func (e *Engine) doctorCheckLedgerStores(ctx context.Context, id string, workspaceStore state.StateStore, ws *state.WorkspaceState, fix bool) ([]DoctorFinding, error) {
	missing := make(map[string][]string)
	for relPath, ownership := range ws.Paths {
		locations, err := e.storeResolver.findStore(ownership.Store)
		if err != nil {
			return nil, err
		}
		if len(locations) == 0 {
			missing[ownership.Store] = append(missing[ownership.Store], relPath)
		}
	}
	if len(missing) == 0 {
		return nil, nil
	}

	storeIDs := make([]string, 0, len(missing))
	for storeID := range missing {
		storeIDs = append(storeIDs, storeID)
	}
	sort.Strings(storeIDs)

	findings := make([]DoctorFinding, 0, len(storeIDs))
	for _, storeID := range storeIDs {
		paths := append([]string{}, missing[storeID]...)
		sort.Strings(paths)
		findings = append(findings, DoctorFinding{
			Code:        DoctorMissingStore,
			Severity:    DoctorSeverityProblem,
			WorkspaceID: id,
			Message:     fmt.Sprintf("workspace %s ledger has %d path(s) owned by store %q, which no longer exists: %s", id, len(paths), storeID, strings.Join(paths, ", ")),
			Recovery:    "run `monodev doctor --fix` to prune these ledger entries",
			Fixable:     true,
		})
	}

	if !fix {
		return findings, nil
	}

	unlockEx, err := lockWorkspace(ctx, workspaceStore, id, lockfile.Exclusive)
	if err != nil {
		for i := range findings {
			findings[i].FixError = err.Error()
		}
		return findings, nil
	}
	defer unlockEx()

	fresh, err := workspaceStore.LoadWorkspace(id)
	if err != nil {
		for i := range findings {
			findings[i].FixError = err.Error()
		}
		return findings, nil
	}
	pruned := state.CloneWorkspaceState(fresh)
	for _, paths := range missing {
		for _, relPath := range paths {
			delete(pruned.Paths, relPath)
		}
	}
	pruned.PruneAppliedStores()
	if len(pruned.Paths) == 0 {
		pruned.Applied = false
	}
	if err := workspaceStore.SaveWorkspace(id, pruned); err != nil {
		for i := range findings {
			findings[i].FixError = err.Error()
		}
		return findings, nil
	}
	for i := range findings {
		findings[i].Fixed = true
	}
	return findings, nil
}

// doctorScanLocks reports lock files as benign, informational findings. A
// lock file's presence is a durable coordination name, not evidence that the
// resource is currently locked (see internal/lockfile).
func (e *Engine) doctorScanLocks() []DoctorFinding {
	dirSet := make(map[string]bool)
	add := func(base string) {
		if strings.TrimSpace(base) == "" {
			return
		}
		dirSet[filepath.Join(base, ".locks")] = true
	}
	add(e.configPaths.Workspaces)
	add(e.configPaths.Stores)
	if e.scopedPaths != nil && e.scopedPaths.Component != nil {
		add(e.scopedPaths.Component.Workspaces)
		add(e.scopedPaths.Component.Stores)
	}

	lockDirs := make([]string, 0, len(dirSet))
	for dir := range dirSet {
		lockDirs = append(lockDirs, dir)
	}
	sort.Strings(lockDirs)

	var findings []DoctorFinding
	for _, dir := range lockDirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		names := make([]string, 0, len(entries))
		for _, entry := range entries {
			if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".lock") {
				names = append(names, entry.Name())
			}
		}
		sort.Strings(names)
		for _, name := range names {
			findings = append(findings, DoctorFinding{
				Code:     DoctorStaleLock,
				Severity: DoctorSeverityInfo,
				Message:  fmt.Sprintf("lock file %s exists; this only records lock file presence, not a currently held lock, and is safe to ignore", filepath.Join(dir, name)),
				Fixable:  false,
			})
		}
	}
	return findings
}

// doctorScanRepo runs the checks scoped to the repository discovered from
// cwd: remote persistence configuration and the managed exclude block.
// It is a silent no-op outside a git repository.
func (e *Engine) doctorScanRepo(ctx context.Context, cwd string, fix bool) ([]DoctorFinding, error) {
	root, err := e.gitRepo.Discover(cwd)
	if err != nil {
		return nil, nil
	}

	var findings []DoctorFinding
	findings = append(findings, e.doctorCheckRemoteConfig(ctx, root)...)

	excludeFindings, err := e.doctorCheckExcludeDrift(ctx, cwd, root, fix)
	if err != nil {
		return nil, err
	}
	findings = append(findings, excludeFindings...)

	return findings, nil
}

// doctorCheckRemoteConfig reports remote persistence configuration that
// names a git remote the repository does not have. There is no safe
// automatic fix: monodev cannot guess which remote the user intended.
func (e *Engine) doctorCheckRemoteConfig(ctx context.Context, root string) []DoctorFinding {
	configStore := remote.NewFileRemoteConfigStore(e.fs)
	cfg, err := configStore.Load(root)
	if err != nil || strings.TrimSpace(cfg.Remote) == "" {
		return nil
	}

	gitPersist := remote.NewRealGitPersistence()
	if _, err := gitPersist.GetRemoteURL(ctx, root, cfg.Remote); err != nil {
		return []DoctorFinding{{
			Code:     DoctorUnknownRemote,
			Severity: DoctorSeverityProblem,
			Message:  fmt.Sprintf("remote persistence is configured to use git remote %q, which this repository does not have", cfg.Remote),
			Recovery: "run `monodev remote use <name>` with a remote that exists, or add the missing remote with `git remote add`",
			Fixable:  false,
		}}
	}
	return nil
}

// doctorCheckExcludeDrift reports drift between the managed block in
// .git/info/exclude and the current workspace's ledger, mirroring the logic
// syncManagedExcludes uses to write that block.
func (e *Engine) doctorCheckExcludeDrift(ctx context.Context, cwd, root string, fix bool) ([]DoctorFinding, error) {
	repoFingerprint, err := e.gitRepo.Fingerprint(root)
	if err != nil {
		return nil, nil
	}
	workspacePath, err := e.gitRepo.RelPath(root, cwd)
	if err != nil {
		return nil, nil
	}

	workspaceID := state.ComputeWorkspaceID(repoFingerprint, workspacePath)
	unlockWorkspace, err := e.lockWorkspace(ctx, workspaceID, lockfile.Shared)
	if err != nil {
		return nil, err
	}
	ws, workspaceID, err := e.loadWorkspaceState(root, repoFingerprint, workspacePath)
	unlockWorkspace()
	if err != nil {
		return nil, fmt.Errorf("failed to load workspace state: %w", err)
	}

	gitDir, err := e.gitRepo.CommonGitDir(root)
	if err != nil {
		return nil, nil
	}
	excludePath := filepath.Join(gitDir, "info", "exclude")

	entries, err := e.managedExcludeEntries(workspacePath, ws)
	if err != nil {
		return nil, fmt.Errorf("failed to compute managed excludes: %w", err)
	}
	contents, err := e.fs.ReadFile(excludePath)
	if err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("failed to read .git/info/exclude: %w", err)
	}
	if os.IsNotExist(err) {
		contents = nil
	}

	replacement := managedExcludeBlock(entries)
	_, changed, err := replaceManagedExcludeBlock(contents, replacement)
	if err != nil {
		return nil, fmt.Errorf("failed to inspect .git/info/exclude: %w", err)
	}
	if !changed {
		return nil, nil
	}

	finding := DoctorFinding{
		Code:        DoctorExcludeDrift,
		Severity:    DoctorSeverityProblem,
		WorkspaceID: workspaceID,
		Message:     fmt.Sprintf("%s does not match the workspace ledger for %s", excludePath, workspaceID),
		Recovery:    "run `monodev doctor --fix` to rewrite the managed block from the ledger",
		Fixable:     true,
	}

	if fix {
		unlockEx, err := e.lockWorkspace(ctx, workspaceID, lockfile.Exclusive)
		if err != nil {
			finding.FixError = err.Error()
			return []DoctorFinding{finding}, nil
		}
		defer unlockEx()
		if err := e.syncManagedExcludes(root, workspacePath, ws); err != nil {
			finding.FixError = err.Error()
		} else {
			finding.Fixed = true
		}
	}

	return []DoctorFinding{finding}, nil
}
