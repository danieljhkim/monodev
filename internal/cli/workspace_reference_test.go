package cli

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/danieljhkim/monodev/internal/config"
	"github.com/danieljhkim/monodev/internal/fsops"
	"github.com/danieljhkim/monodev/internal/persist"
	"github.com/danieljhkim/monodev/internal/sync"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

func TestScopedSyncerRepos_FindsEngineStateAfterInit(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("MONODEV_ROOT", "")

	repo := initGitRepo(t, t.TempDir(), "origin-repo")
	chdir(t, repo)
	runCLI(t, "init")

	if err := os.MkdirAll(filepath.Join(repo, "tools"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "tools", "hello.sh"), []byte("echo hi\n"), 0644); err != nil {
		t.Fatal(err)
	}

	runCLI(t, "checkout", "--new", "qa-store")
	runCLI(t, "track", "tools")
	runCLI(t, "commit", "--all")

	scopedPaths, err := config.NewScopedPaths()
	if err != nil {
		t.Fatal(err)
	}
	if scopedPaths.Component == nil {
		t.Fatal("expected component scope after monodev init")
	}

	statusOut := runCLI(t, "status", "--json")
	workspaceID := statusWorkspaceID(t, statusOut)
	if workspaceID == "" {
		t.Fatalf("status JSON missing WorkspaceID: %s", statusOut)
	}

	globalWorkspace := filepath.Join(home, ".monodev", "workspaces", workspaceID+".json")
	componentWorkspace := filepath.Join(repo, ".monodev", "workspaces", workspaceID+".json")
	if _, err := os.Stat(globalWorkspace); err != nil {
		t.Fatalf("engine workspace state should be global %s: %v", globalWorkspace, err)
	}
	if _, err := os.Stat(componentWorkspace); !os.IsNotExist(err) {
		t.Fatalf("engine should not write workspace state to repo-local %s", componentWorkspace)
	}
	if _, err := os.Stat(filepath.Join(repo, ".monodev", "stores", "qa-store")); err != nil {
		t.Fatalf("default store after init should be component-scoped: %v", err)
	}

	fs := fsops.NewRealFS()
	storeRepo, stateStore := scopedSyncerRepos(fs, scopedPaths)
	if _, err := stateStore.LoadWorkspace(workspaceID); err != nil {
		t.Fatalf("syncer state store should load engine workspace: %v", err)
	}
	exists, err := storeRepo.Exists("qa-store")
	if err != nil {
		t.Fatal(err)
	}
	if !exists {
		t.Fatal("syncer store repo should find component store qa-store")
	}

	syncer, err := newSyncer()
	if err != nil {
		t.Fatal(err)
	}
	result, err := syncer.PushStore(context.Background(), &sync.PushRequest{
		RepoRoot:           repo,
		StoreIDs:           []string{"qa-store"},
		WorkspaceID:        workspaceID,
		WithWorkspace:      true,
		RepositoryIdentity: "origin-repo",
		DryRun:             true,
	})
	if err != nil {
		t.Fatalf("dry-run push --with-workspace after init: %v", err)
	}
	if !result.PushedWorkspace {
		t.Fatalf("expected workspace reference to be selected, got %#v", result)
	}

	if err := persist.NewSnapshotManager(fs).Materialize("qa-store", storeRepo, repo); err != nil {
		t.Fatalf("syncer store repo should materialize component store: %v", err)
	}
}

func TestScopedSyncerRepos_FindsComponentStoreWhenMonodevRootSet(t *testing.T) {
	home := t.TempDir()
	monodevRoot := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("MONODEV_ROOT", monodevRoot)

	repo := initGitRepo(t, t.TempDir(), "origin-repo")
	chdir(t, repo)
	runCLI(t, "init")
	runCLI(t, "checkout", "--new", "qa-store")

	statusOut := runCLI(t, "status", "--json")
	workspaceID := statusWorkspaceID(t, statusOut)

	if _, err := os.Stat(filepath.Join(monodevRoot, "workspaces", workspaceID+".json")); err != nil {
		t.Fatalf("workspace should live under MONODEV_ROOT: %v", err)
	}
	if _, err := os.Stat(filepath.Join(repo, ".monodev", "stores", "qa-store")); err != nil {
		t.Fatalf("store should stay component-scoped under MONODEV_ROOT: %v", err)
	}

	scopedPaths, err := config.NewScopedPaths()
	if err != nil {
		t.Fatal(err)
	}
	storeRepo, stateStore := scopedSyncerRepos(fsops.NewRealFS(), scopedPaths)
	if _, err := stateStore.LoadWorkspace(workspaceID); err != nil {
		t.Fatalf("syncer should load workspace from MONODEV_ROOT: %v", err)
	}
	exists, err := storeRepo.Exists("qa-store")
	if err != nil {
		t.Fatal(err)
	}
	if !exists {
		t.Fatal("syncer should find component store while MONODEV_ROOT is set")
	}
}

func TestPushPull_WorkspaceReferenceAfterInit(t *testing.T) {
	configureTestGitIdentity(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("MONODEV_ROOT", "")

	base := t.TempDir()
	bareRemote := filepath.Join(base, "remote.git")
	if err := os.MkdirAll(bareRemote, 0755); err != nil {
		t.Fatal(err)
	}
	runGit(t, bareRemote, "init", "--bare")

	source := initGitRepo(t, filepath.Join(base, "source"), bareRemote)
	chdir(t, source)
	if err := os.MkdirAll(filepath.Join(source, "tools"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "tools", "hello.sh"), []byte("echo hi\n"), 0644); err != nil {
		t.Fatal(err)
	}

	runCLI(t, "init")
	runCLI(t, "checkout", "--new", "qa-store")
	runCLI(t, "track", "tools")
	runCLI(t, "commit", "--all")
	runCLI(t, "checkout", "qa-store")
	runCLI(t, "apply")
	workspaceID := statusWorkspaceID(t, runCLI(t, "status", "--json"))
	runCLI(t, "push", "qa-store", "--with-workspace")

	target := initGitRepo(t, filepath.Join(base, "target"), bareRemote)
	chdir(t, target)
	runCLI(t, "init")
	runCLI(t, "remote", "use", "origin")
	runCLI(t, "pull", "--workspace", workspaceID, "--with-stores")

	if _, err := os.Stat(filepath.Join(target, ".monodev", "stores", "qa-store")); err != nil {
		t.Fatalf("pulled store missing from target component scope: %v", err)
	}
	status := runCLI(t, "status", "--json")
	var parsed map[string]any
	if err := json.Unmarshal([]byte(status), &parsed); err != nil {
		t.Fatalf("status JSON: %v\n%s", err, status)
	}
	if parsed["ActiveStore"] != "qa-store" {
		t.Fatalf("restored ActiveStore = %#v, want qa-store; status=%s", parsed["ActiveStore"], status)
	}
	if parsed["WorkspaceID"] == workspaceID {
		t.Fatal("restored workspace should use the target checkout identity")
	}
	if parsed["Applied"] == true {
		t.Fatal("restored workspace must not claim source applied files")
	}
}

func initGitRepo(t *testing.T, dir, origin string) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "init")
	runGit(t, dir, "config", "user.email", "monodev-test@example.com")
	runGit(t, dir, "config", "user.name", "Monodev Test")
	runGit(t, dir, "remote", "add", "origin", origin)
	return dir
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func configureTestGitIdentity(t *testing.T) {
	t.Helper()
	t.Setenv("GIT_AUTHOR_NAME", "Monodev Test")
	t.Setenv("GIT_AUTHOR_EMAIL", "monodev-test@example.com")
	t.Setenv("GIT_COMMITTER_NAME", "Monodev Test")
	t.Setenv("GIT_COMMITTER_EMAIL", "monodev-test@example.com")
}

func chdir(t *testing.T, dir string) {
	t.Helper()
	old, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(old) })
}

func runCLI(t *testing.T, args ...string) string {
	t.Helper()
	resetCommandFlags(rootCmd)
	rootCmd.SetArgs(append([]string{}, args...))
	var execErr error
	out := captureStdout(t, func() {
		execErr = rootCmd.Execute()
	})
	if execErr != nil {
		t.Fatalf("monodev %v: %v\n%s", args, execErr, out)
	}
	return out
}

func resetCommandFlags(cmd *cobra.Command) {
	resetFlagSet(cmd.PersistentFlags())
	resetFlagSet(cmd.Flags())
	for _, child := range cmd.Commands() {
		resetCommandFlags(child)
	}
}

func resetFlagSet(fs *pflag.FlagSet) {
	fs.VisitAll(func(f *pflag.Flag) {
		_ = f.Value.Set(f.DefValue)
		f.Changed = false
	})
}

func statusWorkspaceID(t *testing.T, out string) string {
	t.Helper()
	var parsed struct {
		WorkspaceID string
	}
	if err := json.Unmarshal([]byte(out), &parsed); err != nil {
		t.Fatalf("status JSON: %v\n%s", err, out)
	}
	if parsed.WorkspaceID == "" {
		t.Fatalf("status JSON missing WorkspaceID: %s", out)
	}
	return parsed.WorkspaceID
}
