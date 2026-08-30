package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/danieljhkim/monodev/internal/fsops"
	"github.com/danieljhkim/monodev/internal/planner"
	"github.com/danieljhkim/monodev/internal/state"
)

const (
	overlayTxnVersion = 1

	overlayTxnApply   = "apply"
	overlayTxnUnapply = "unapply"
	overlayTxnEject   = "eject"

	overlayTxnPreparing = "preparing"
	overlayTxnPrepared  = "prepared"
	overlayTxnCommitted = "committed"
)

// overlayTxn is the durable recovery record for one overlay mutation.
//
// Recovery after process restart is driven by Phase:
//   - preparing: destinations were not mutated; discard backups and the journal
//   - prepared: destinations may be partially swapped; restore each backup
//   - committed: destinations already match FinalState; retry the state save
//     (or delete), then discard backups
//
// See docs/overlay-recovery.md for the operator-facing restart path.
type overlayTxn struct {
	Version       int                   `json:"version"`
	Kind          string                `json:"kind"`
	WorkspaceID   string                `json:"workspaceId"`
	WorkspaceRoot string                `json:"workspaceRoot"`
	Phase         string                `json:"phase"`
	Ops           []overlayTxnOp        `json:"ops"`
	FinalState    *state.WorkspaceState `json:"finalState,omitempty"`
	DeleteState   bool                  `json:"deleteState,omitempty"`
}

type overlayTxnOp struct {
	RelPath      string `json:"relPath"`
	Type         string `json:"type"`
	SourcePath   string `json:"sourcePath,omitempty"`
	DestExisted  bool   `json:"destExisted"`
	BackupRel    string `json:"backupRel,omitempty"`
	StagedRel    string `json:"stagedRel,omitempty"`
	LinkTarget   string `json:"linkTarget,omitempty"`
	BackupIsLink bool   `json:"backupIsLink,omitempty"`
}

type overlayTxnRequest struct {
	kind          string
	workspaceID   string
	workspaceRoot string
	ops           []planner.Operation
	finalize      func() (*state.WorkspaceState, bool, error)
}

func (e *Engine) overlayTxnPaths(id string) (journalPath, txnDir string, err error) {
	if e.fs == nil {
		return "", "", fmt.Errorf("filesystem is not configured")
	}
	if err := e.fs.ValidateIdentifier(id); err != nil {
		return "", "", fmt.Errorf("invalid workspace ID: %w", err)
	}
	base := e.configPaths.Workspaces
	if strings.TrimSpace(base) == "" {
		return "", "", fmt.Errorf("workspace state directory is not configured")
	}
	return filepath.Join(base, id+".txn.json"), filepath.Join(base, id+".txn"), nil
}

func (e *Engine) writeOverlayTxn(journalPath string, txn *overlayTxn) error {
	data, err := json.MarshalIndent(txn, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal overlay transaction: %w", err)
	}
	if err := e.fs.AtomicWrite(journalPath, data, 0600); err != nil {
		return fmt.Errorf("failed to persist overlay transaction: %w", err)
	}
	return nil
}

func (e *Engine) loadOverlayTxn(journalPath string) (*overlayTxn, error) {
	data, err := e.fs.ReadFile(journalPath)
	if err != nil {
		return nil, err
	}
	if len(data) == 0 {
		return nil, os.ErrNotExist
	}
	var txn overlayTxn
	if err := json.Unmarshal(data, &txn); err != nil {
		return nil, fmt.Errorf("failed to parse overlay transaction journal: %w", err)
	}
	return &txn, nil
}

func (e *Engine) discardOverlayTxn(journalPath, txnDir string) error {
	if err := e.fs.RemoveAll(txnDir); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove overlay transaction directory: %w", err)
	}
	if err := e.fs.Remove(journalPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove overlay transaction journal: %w", err)
	}
	return nil
}

// recoverOverlayTxn reapplies or rolls back an interrupted overlay transaction.
// It is idempotent: a completed journal is committed then discarded, and a
// prepared journal restores destination backups even if some swaps already ran.
func (e *Engine) recoverOverlayTxn(ctx context.Context, workspaceID, workspaceRoot string) error {
	if err := checkContext(ctx); err != nil {
		return err
	}
	journalPath, txnDir, err := e.overlayTxnPaths(workspaceID)
	if err != nil {
		return err
	}
	txn, err := e.loadOverlayTxn(journalPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	switch txn.Phase {
	case overlayTxnPreparing, "":
		return e.discardOverlayTxn(journalPath, txnDir)
	case overlayTxnPrepared:
		if err := e.rollbackOverlayTxn(txn, workspaceRoot, txnDir); err != nil {
			return fmt.Errorf("failed to roll back interrupted overlay transaction: %w", err)
		}
		return e.discardOverlayTxn(journalPath, txnDir)
	case overlayTxnCommitted:
		if err := e.commitOverlayTxnState(workspaceID, txn); err != nil {
			return err
		}
		return e.discardOverlayTxn(journalPath, txnDir)
	default:
		return fmt.Errorf("unknown overlay transaction phase %q", txn.Phase)
	}
}

// recoverWorkspaceOverlay finishes any interrupted overlay mutation, then
// repairs the best-effort git exclusion block from the durable ledger.
func (e *Engine) recoverWorkspaceOverlay(ctx context.Context, workspaceID, repoRoot, workspaceRoot, workspacePath string) ([]string, error) {
	if err := e.recoverOverlayTxn(ctx, workspaceID, workspaceRoot); err != nil {
		return nil, err
	}

	ws, err := e.stateStore.LoadWorkspace(workspaceID)
	if err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("failed to reload workspace state after recovery: %w", err)
	}
	if os.IsNotExist(err) {
		return appendExcludeWarning(nil, e.syncManagedExcludes(repoRoot, workspacePath, nil)), nil
	}
	return appendExcludeWarning(nil, e.syncManagedExcludes(repoRoot, ws.WorkspacePath, ws)), nil
}

func (e *Engine) commitOverlayTxnState(workspaceID string, txn *overlayTxn) error {
	if txn.DeleteState {
		if err := e.stateStore.DeleteWorkspace(workspaceID); err != nil {
			return fmt.Errorf("failed to delete workspace state during recovery: %w", err)
		}
		return nil
	}
	if txn.FinalState == nil {
		return nil
	}
	if err := e.stateStore.SaveWorkspace(workspaceID, txn.FinalState); err != nil {
		return fmt.Errorf("failed to save workspace state during recovery: %w", err)
	}
	return nil
}

func (e *Engine) runOverlayTxn(ctx context.Context, req overlayTxnRequest) error {
	if err := checkContext(ctx); err != nil {
		return err
	}
	if req.finalize == nil {
		return fmt.Errorf("overlay transaction is missing a finalize callback")
	}

	journalPath, txnDir, err := e.overlayTxnPaths(req.workspaceID)
	if err != nil {
		return err
	}
	if err := e.fs.MkdirAll(txnDir, 0700); err != nil {
		return fmt.Errorf("failed to create overlay transaction directory: %w", err)
	}

	txn := overlayTxn{
		Version:       overlayTxnVersion,
		Kind:          req.kind,
		WorkspaceID:   req.workspaceID,
		WorkspaceRoot: req.workspaceRoot,
		Phase:         overlayTxnPreparing,
		Ops:           make([]overlayTxnOp, 0, len(req.ops)),
	}
	if err := e.writeOverlayTxn(journalPath, &txn); err != nil {
		_ = e.discardOverlayTxn(journalPath, txnDir)
		return err
	}

	for _, op := range req.ops {
		if err := checkContext(ctx); err != nil {
			_ = e.discardOverlayTxn(journalPath, txnDir)
			return err
		}
		prepared, prepErr := e.prepareOverlayOp(req.workspaceRoot, txnDir, op)
		if prepErr != nil {
			_ = e.discardOverlayTxn(journalPath, txnDir)
			return prepErr
		}
		txn.Ops = append(txn.Ops, prepared)
	}

	txn.Phase = overlayTxnPrepared
	if err := e.writeOverlayTxn(journalPath, &txn); err != nil {
		_ = e.rollbackOverlayTxn(&txn, req.workspaceRoot, txnDir)
		_ = e.discardOverlayTxn(journalPath, txnDir)
		return err
	}

	if err := e.installOverlayTxn(ctx, &txn, req.workspaceRoot, txnDir); err != nil {
		if rollbackErr := e.rollbackOverlayTxn(&txn, req.workspaceRoot, txnDir); rollbackErr == nil {
			_ = e.discardOverlayTxn(journalPath, txnDir)
		}
		return err
	}

	final, deleteState, err := req.finalize()
	if err != nil {
		if rollbackErr := e.rollbackOverlayTxn(&txn, req.workspaceRoot, txnDir); rollbackErr == nil {
			_ = e.discardOverlayTxn(journalPath, txnDir)
		}
		return err
	}

	txn.FinalState = final
	txn.DeleteState = deleteState
	txn.Phase = overlayTxnCommitted
	if err := e.writeOverlayTxn(journalPath, &txn); err != nil {
		return err
	}
	if err := e.commitOverlayTxnState(req.workspaceID, &txn); err != nil {
		return err
	}
	return e.discardOverlayTxn(journalPath, txnDir)
}

func (e *Engine) prepareOverlayOp(workspaceRoot, txnDir string, op planner.Operation) (overlayTxnOp, error) {
	if err := e.fs.ValidateRelPath(op.RelPath); err != nil {
		return overlayTxnOp{}, fmt.Errorf("invalid operation path %q: %w", op.RelPath, err)
	}
	absDest := filepath.Join(workspaceRoot, op.RelPath)
	exists, err := e.fs.Exists(absDest)
	if err != nil {
		return overlayTxnOp{}, fmt.Errorf("failed to inspect %s: %w", op.RelPath, err)
	}

	prepared := overlayTxnOp{
		RelPath:     op.RelPath,
		Type:        op.Type,
		SourcePath:  op.SourcePath,
		DestExisted: exists,
	}

	if exists {
		info, lstatErr := e.fs.Lstat(absDest)
		if lstatErr != nil {
			return overlayTxnOp{}, fmt.Errorf("failed to stat %s: %w", op.RelPath, lstatErr)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			target, readErr := e.fs.Readlink(absDest)
			if readErr != nil {
				return overlayTxnOp{}, fmt.Errorf("failed to read symlink %s: %w", op.RelPath, readErr)
			}
			prepared.BackupIsLink = true
			prepared.LinkTarget = target
		} else {
			prepared.BackupRel = filepath.ToSlash(filepath.Join("backup", op.RelPath))
			if err := e.backupDest(absDest, filepath.Join(txnDir, filepath.FromSlash(prepared.BackupRel))); err != nil {
				return overlayTxnOp{}, fmt.Errorf("failed to backup %s: %w", op.RelPath, err)
			}
		}
	}

	switch op.Type {
	case planner.OpCopy:
		prepared.StagedRel = filepath.ToSlash(filepath.Join("staged", op.RelPath))
		stagedPath := filepath.Join(txnDir, filepath.FromSlash(prepared.StagedRel))
		if err := e.fs.MkdirAll(filepath.Dir(stagedPath), 0700); err != nil {
			return overlayTxnOp{}, fmt.Errorf("failed to create staged path for %s: %w", op.RelPath, err)
		}
		if err := e.fs.Copy(op.SourcePath, stagedPath); err != nil {
			return overlayTxnOp{}, fmt.Errorf("failed to stage %s: %w", op.RelPath, err)
		}
	case planner.OpCreateSymlink:
		prepared.SourcePath = op.SourcePath
	case planner.OpRemove:
	default:
		return overlayTxnOp{}, fmt.Errorf("unknown operation type: %s", op.Type)
	}
	return prepared, nil
}

func (e *Engine) installOverlayTxn(ctx context.Context, txn *overlayTxn, workspaceRoot, txnDir string) error {
	for _, op := range txn.Ops {
		if err := checkContext(ctx); err != nil {
			return err
		}
		if err := e.installOverlayOp(workspaceRoot, txnDir, op); err != nil {
			return err
		}
	}
	return nil
}

func (e *Engine) installOverlayOp(workspaceRoot, txnDir string, op overlayTxnOp) error {
	switch op.Type {
	case planner.OpCopy:
		stagedPath := filepath.Join(txnDir, filepath.FromSlash(op.StagedRel))
		return e.executeOperation(workspaceRoot, planner.Operation{
			Type:       planner.OpCopy,
			SourcePath: stagedPath,
			DestPath:   filepath.Join(workspaceRoot, op.RelPath),
			RelPath:    op.RelPath,
		})
	case planner.OpCreateSymlink:
		if err := e.removeRelPath(workspaceRoot, op.RelPath); err != nil {
			return err
		}
		return e.executeOperation(workspaceRoot, planner.Operation{
			Type:       planner.OpCreateSymlink,
			SourcePath: op.SourcePath,
			DestPath:   filepath.Join(workspaceRoot, op.RelPath),
			RelPath:    op.RelPath,
		})
	case planner.OpRemove:
		return e.removeRelPath(workspaceRoot, op.RelPath)
	default:
		return fmt.Errorf("unknown operation type: %s", op.Type)
	}
}

func (e *Engine) rollbackOverlayTxn(txn *overlayTxn, workspaceRoot, txnDir string) error {
	for i := len(txn.Ops) - 1; i >= 0; i-- {
		op := txn.Ops[i]
		if err := e.restoreOverlayOp(workspaceRoot, txnDir, op); err != nil {
			return err
		}
	}
	return nil
}

func (e *Engine) restoreOverlayOp(workspaceRoot, txnDir string, op overlayTxnOp) error {
	cleanupInstallTemps(filepath.Join(workspaceRoot, filepath.Dir(op.RelPath)))
	absDest := filepath.Join(workspaceRoot, op.RelPath)
	if !op.DestExisted {
		return e.removeRelPath(workspaceRoot, op.RelPath)
	}
	if err := e.removeRelPath(workspaceRoot, op.RelPath); err != nil {
		return err
	}
	if op.BackupIsLink {
		return e.executeOperation(workspaceRoot, planner.Operation{
			Type:       planner.OpCreateSymlink,
			SourcePath: op.LinkTarget,
			DestPath:   absDest,
			RelPath:    op.RelPath,
		})
	}
	if op.BackupRel == "" {
		return nil
	}
	backupPath := filepath.Join(txnDir, filepath.FromSlash(op.BackupRel))
	return e.restoreDest(backupPath, absDest, workspaceRoot, op.RelPath)
}

func (e *Engine) restoreDest(backupPath, absDest, workspaceRoot, relPath string) error {
	if _, err := os.Lstat(backupPath); err == nil {
		if err := restoreTree(backupPath, absDest); err != nil {
			return fmt.Errorf("failed to restore %s: %w", relPath, err)
		}
		return nil
	}
	return e.executeOperation(workspaceRoot, planner.Operation{
		Type:       planner.OpCopy,
		SourcePath: backupPath,
		DestPath:   absDest,
		RelPath:    relPath,
	})
}

func (e *Engine) removeRelPath(workspaceRoot, relPath string) error {
	if err := e.fs.ValidateRelPath(relPath); err != nil {
		return fmt.Errorf("invalid operation path %q: %w", relPath, err)
	}
	if rootFS, ok := e.fs.(fsops.RootFS); ok {
		if err := rootFS.RemoveAllWithinRoot(workspaceRoot, relPath); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("failed to remove %s: %w", relPath, err)
		}
		return nil
	}
	absPath := filepath.Join(workspaceRoot, relPath)
	if err := e.fs.RemoveAll(absPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove %s: %w", relPath, err)
	}
	return nil
}

func (e *Engine) backupDest(src, dst string) error {
	if _, err := os.Lstat(src); err == nil {
		if err := backupTree(src, dst); err != nil {
			return err
		}
		return nil
	}
	if err := e.fs.MkdirAll(filepath.Dir(dst), 0700); err != nil {
		return err
	}
	return e.fs.Copy(src, dst)
}

func backupTree(src, dst string) error {
	info, err := os.Lstat(src)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0700); err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		target, err := os.Readlink(src)
		if err != nil {
			return err
		}
		return os.Symlink(target, dst)
	}
	if info.IsDir() {
		if err := os.MkdirAll(dst, 0700); err != nil {
			return err
		}
		entries, err := os.ReadDir(src)
		if err != nil {
			return err
		}
		for _, entry := range entries {
			if err := backupTree(filepath.Join(src, entry.Name()), filepath.Join(dst, entry.Name())); err != nil {
				return err
			}
		}
		return nil
	}
	return copyRegularFile(src, dst)
}

func restoreTree(src, dst string) error {
	return backupTree(src, dst)
}

func copyRegularFile(src, dst string) error {
	srcFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer func() { _ = srcFile.Close() }()

	if err := os.MkdirAll(filepath.Dir(dst), 0700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(dst), ".monodev-copy-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	success := false
	defer func() {
		_ = tmp.Close()
		if !success {
			_ = os.Remove(tmpPath)
		}
	}()
	if _, err := io.Copy(tmp, srcFile); err != nil {
		return err
	}
	if err := tmp.Sync(); err != nil {
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, dst); err != nil {
		return err
	}
	success = true
	return nil
}

func cleanupInstallTemps(parent string) {
	if parent == "" {
		return
	}
	entries, err := os.ReadDir(parent)
	if err != nil {
		return
	}
	for _, entry := range entries {
		name := entry.Name()
		if strings.HasPrefix(name, ".monodev-copy-") || strings.HasPrefix(name, ".monodev-aside-") {
			_ = os.RemoveAll(filepath.Join(parent, name))
		}
	}
}
