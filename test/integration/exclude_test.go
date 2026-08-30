package integration

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/danieljhkim/monodev/internal/clock"
	"github.com/danieljhkim/monodev/internal/config"
	"github.com/danieljhkim/monodev/internal/engine"
	"github.com/danieljhkim/monodev/internal/fsops"
	"github.com/danieljhkim/monodev/internal/gitx"
	"github.com/danieljhkim/monodev/internal/hash"
	"github.com/danieljhkim/monodev/internal/state"
	"github.com/danieljhkim/monodev/internal/stores"
)

type excludeFixture struct {
	repoRoot      string
	workspaceRoot string
	storeID       string
	engine        *engine.Engine
}

func newExcludeFixture(t *testing.T, workspacePath string) excludeFixture {
	t.Helper()
	repoRoot := t.TempDir()
	runGit(t, repoRoot, "init")

	workspaceRoot := repoRoot
	if workspacePath != "." {
		workspaceRoot = filepath.Join(repoRoot, workspacePath)
		if err := os.MkdirAll(workspaceRoot, 0700); err != nil {
			t.Fatalf("create workspace directory: %v", err)
		}
	}

	dataRoot := t.TempDir()
	paths := config.Paths{
		Root:       dataRoot,
		Stores:     filepath.Join(dataRoot, "stores"),
		Workspaces: filepath.Join(dataRoot, "workspaces"),
		Config:     filepath.Join(dataRoot, "config.yaml"),
	}
	fs := fsops.NewRealFS()
	storeRepo := stores.NewFileStoreRepo(fs, paths.Stores)
	storeID := "exclude-store"
	if err := storeRepo.Create(storeID, stores.NewStoreMeta(storeID, stores.ScopeGlobal, time.Now())); err != nil {
		t.Fatalf("create store: %v", err)
	}
	if err := os.WriteFile(filepath.Join(storeRepo.OverlayRoot(storeID), "Makefile"), []byte("all:\n\t@true\n"), 0600); err != nil {
		t.Fatalf("write overlay Makefile: %v", err)
	}
	track := stores.NewTrackFile()
	track.Tracked = []stores.TrackedPath{{Path: "Makefile", Kind: "file"}}
	if err := storeRepo.SaveTrack(storeID, track); err != nil {
		t.Fatalf("save tracked paths: %v", err)
	}

	eng := engine.New(
		gitx.NewRealGitRepo(),
		storeRepo,
		state.NewFileStateStore(fs, paths.Workspaces),
		fs,
		hash.NewSHA256Hasher(),
		clock.NewFakeClock(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)),
		paths,
	)
	return excludeFixture{repoRoot: repoRoot, workspaceRoot: workspaceRoot, storeID: storeID, engine: eng}
}

func (fx excludeFixture) apply(t *testing.T) *engine.ApplyResult {
	t.Helper()
	result, err := fx.engine.Apply(context.Background(), &engine.ApplyRequest{
		CWD:     fx.workspaceRoot,
		Mode:    "copy",
		StoreID: fx.storeID,
	})
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	return result
}

func (fx excludeFixture) unapply(t *testing.T) *engine.UnapplyResult {
	t.Helper()
	result, err := fx.engine.Unapply(context.Background(), &engine.UnapplyRequest{CWD: fx.workspaceRoot})
	if err != nil {
		t.Fatalf("unapply: %v", err)
	}
	return result
}

func (fx excludeFixture) excludePath() string {
	return filepath.Join(fx.repoRoot, ".git", "info", "exclude")
}

func TestExclude_ApplyUnapplyKeepsRealGitWorkspaceClean(t *testing.T) {
	fx := newExcludeFixture(t, ".")
	original := []byte("# user-owned content\n/local-cache\n")
	if err := os.WriteFile(fx.excludePath(), original, 0644); err != nil {
		t.Fatalf("seed .git/info/exclude: %v", err)
	}

	fx.apply(t)
	afterApply, err := os.ReadFile(fx.excludePath())
	if err != nil {
		t.Fatalf("read applied exclude file: %v", err)
	}
	if !bytes.HasPrefix(afterApply, original) {
		t.Fatalf("user-owned exclude content changed:\n got %q\nwant prefix %q", afterApply, original)
	}
	if !bytes.Contains(afterApply, []byte("/Makefile\n")) {
		t.Fatalf("managed exclude block did not contain Makefile: %q", afterApply)
	}
	if status := runGit(t, fx.repoRoot, "status", "--porcelain"); status != "" {
		t.Fatalf("git status after apply = %q, want clean", status)
	}

	fx.unapply(t)
	afterUnapply, err := os.ReadFile(fx.excludePath())
	if err != nil {
		t.Fatalf("read unapplied exclude file: %v", err)
	}
	if !bytes.Equal(afterUnapply, original) {
		t.Fatalf("user-owned exclude content changed after unapply:\n got %q\nwant %q", afterUnapply, original)
	}
	if bytes.Contains(afterUnapply, []byte("monodev managed block")) {
		t.Fatalf("unapply left monodev block behind: %q", afterUnapply)
	}
	if status := runGit(t, fx.repoRoot, "status", "--porcelain"); status != "" {
		t.Fatalf("git status after unapply = %q, want clean", status)
	}

	fx.apply(t)
	afterRepeatedApply, err := os.ReadFile(fx.excludePath())
	if err != nil {
		t.Fatalf("read repeated applied exclude file: %v", err)
	}
	if !bytes.Equal(afterRepeatedApply, afterApply) {
		t.Fatalf("repeated apply changed exclude content:\n first %q\nagain %q", afterApply, afterRepeatedApply)
	}
	fx.unapply(t)
	afterRepeatedUnapply, err := os.ReadFile(fx.excludePath())
	if err != nil {
		t.Fatalf("read repeated unapplied exclude file: %v", err)
	}
	if !bytes.Equal(afterRepeatedUnapply, original) {
		t.Fatalf("user-owned exclude content changed after repeated cycle:\n got %q\nwant %q", afterRepeatedUnapply, original)
	}
}

func TestExclude_SubdirectoryWorkspaceUsesRepoRootRelativePattern(t *testing.T) {
	fx := newExcludeFixture(t, filepath.Join("packages", "service"))
	fx.apply(t)

	contents, err := os.ReadFile(fx.excludePath())
	if err != nil {
		t.Fatalf("read exclude file: %v", err)
	}
	if !bytes.Contains(contents, []byte("/packages/service/Makefile\n")) {
		t.Fatalf("exclude file did not use a repo-root-relative path: %q", contents)
	}
	if status := runGit(t, fx.repoRoot, "status", "--porcelain"); status != "" {
		t.Fatalf("git status after subdirectory apply = %q, want clean", status)
	}
}

func TestExclude_ReadOnlyFileWarnsAndDoesNotFailApply(t *testing.T) {
	fx := newExcludeFixture(t, ".")
	if err := os.WriteFile(fx.excludePath(), []byte("# user-owned content\n"), 0400); err != nil {
		t.Fatalf("seed read-only exclude file: %v", err)
	}
	if err := os.Chmod(fx.excludePath(), 0400); err != nil {
		t.Fatalf("mark exclude file read-only: %v", err)
	}

	result := fx.apply(t)
	if result.Plan == nil || len(result.Plan.Warnings) == 0 {
		t.Fatal("read-only .git/info/exclude did not produce a warning")
	}
	if !strings.Contains(strings.Join(result.Plan.Warnings, "\n"), ".git/info/exclude is read-only") {
		t.Fatalf("unexpected warnings: %v", result.Plan.Warnings)
	}
	if _, err := os.Stat(filepath.Join(fx.workspaceRoot, "Makefile")); err != nil {
		t.Fatalf("overlay did not apply while exclude file was read-only: %v", err)
	}
}

func runGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, output)
	}
	return string(output)
}
