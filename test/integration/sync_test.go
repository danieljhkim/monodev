//go:build integration
// +build integration

package integration

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fatih/color"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/danieljhkim/monodev/internal/cli"
)

// resetCLIFlags restores every flag on cmd and its subcommands to its
// default value. cli.RootCommand() returns a process-wide singleton, so
// without this a flag set by one invocation (e.g. checkout --new) would
// leak into a later invocation that omits it.
func resetCLIFlags(cmd *cobra.Command) {
	reset := func(fs *pflag.FlagSet) {
		fs.VisitAll(func(f *pflag.Flag) {
			_ = f.Value.Set(f.DefValue)
			f.Changed = false
		})
	}
	reset(cmd.PersistentFlags())
	reset(cmd.Flags())
	for _, child := range cmd.Commands() {
		resetCLIFlags(child)
	}
}

func runSyncTestGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

// captureMonodevStdout redirects the process-wide stdout (and fatih/color's
// output target) while fn runs. The cli package's Print* helpers write
// directly to os.Stdout rather than through cobra's OutOrStdout, so this is
// the only way to observe command output from outside the cli package.
func captureMonodevStdout(t *testing.T, fn func()) string {
	t.Helper()
	oldStdout := os.Stdout
	oldColorOutput := color.Output
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("failed to create pipe: %v", err)
	}
	os.Stdout = w
	color.Output = w

	fn()

	_ = w.Close()
	os.Stdout = oldStdout
	color.Output = oldColorOutput

	var buf bytes.Buffer
	_, _ = buf.ReadFrom(r)
	return buf.String()
}

func runMonodev(t *testing.T, args ...string) string {
	t.Helper()
	root := cli.RootCommand()
	resetCLIFlags(root)
	root.SetArgs(args)
	var execErr error
	out := captureMonodevStdout(t, func() {
		execErr = root.Execute()
	})
	if execErr != nil {
		t.Fatalf("monodev %v failed: %v\n%s", args, execErr, out)
	}
	return out
}

func setupSyncClientRepo(t *testing.T, dir, bareRemote string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	runSyncTestGit(t, dir, "init")
	runSyncTestGit(t, dir, "config", "user.email", "monodev-sync-test@example.com")
	runSyncTestGit(t, dir, "config", "user.name", "Monodev Sync Test")
	runSyncTestGit(t, dir, "remote", "add", "origin", bareRemote)
}

// TestSyncCommand_CommitsPushesAndPulls drives the actual `monodev sync` CLI
// command against a real local bare remote, covering DANI-10103's
// commit-then-push-then-pull composition end to end: client A commits a
// newly tracked file and pushes it, client B bootstraps the store via pull,
// then client B's own `sync` call demonstrates the same command completing
// the full commit/push/pull cycle for an already-known store.
func TestSyncCommand_CommitsPushesAndPulls(t *testing.T) {
	t.Setenv("GIT_AUTHOR_NAME", "Monodev Sync Test")
	t.Setenv("GIT_AUTHOR_EMAIL", "monodev-sync-test@example.com")
	t.Setenv("GIT_COMMITTER_NAME", "Monodev Sync Test")
	t.Setenv("GIT_COMMITTER_EMAIL", "monodev-sync-test@example.com")

	baseDir := t.TempDir()
	bareRemote := filepath.Join(baseDir, "remote.git")
	if err := os.MkdirAll(bareRemote, 0755); err != nil {
		t.Fatal(err)
	}
	runSyncTestGit(t, bareRemote, "init", "--bare")

	oldWd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldWd) })

	// Client A: track a file and use sync to commit and push it.
	clientARepo := filepath.Join(baseDir, "client-a")
	clientAHome := filepath.Join(baseDir, "client-a-home")
	setupSyncClientRepo(t, clientARepo, bareRemote)

	t.Setenv("HOME", clientAHome)
	t.Setenv("MONODEV_ROOT", "")
	if err := os.Chdir(clientARepo); err != nil {
		t.Fatal(err)
	}

	runMonodev(t, "init")
	if err := os.MkdirAll(filepath.Join(clientARepo, "tools"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(clientARepo, "tools", "hello.sh"), []byte("echo hi\n"), 0644); err != nil {
		t.Fatal(err)
	}
	runMonodev(t, "checkout", "--new", "shared-store")
	runMonodev(t, "track", "tools")
	runMonodev(t, "remote", "use", "origin")

	syncOutA := runMonodev(t, "sync")
	if !strings.Contains(syncOutA, "Committed") {
		t.Fatalf("client A sync output = %q, want a commit summary", syncOutA)
	}
	if !strings.Contains(syncOutA, "Pushed") || !strings.Contains(syncOutA, "shared-store") {
		t.Fatalf("client A sync output = %q, want a push summary naming shared-store", syncOutA)
	}

	// Client B: a separate checkout of the same remote. It bootstraps the
	// store with a plain pull (sync only keeps stores it already knows about
	// current), then its own sync call exercises the full commit/push/pull
	// composition for that now-known store.
	clientBRepo := filepath.Join(baseDir, "client-b")
	clientBHome := filepath.Join(baseDir, "client-b-home")
	setupSyncClientRepo(t, clientBRepo, bareRemote)

	t.Setenv("HOME", clientBHome)
	if err := os.Chdir(clientBRepo); err != nil {
		t.Fatal(err)
	}
	runMonodev(t, "init")
	runMonodev(t, "remote", "use", "origin")

	pullOutB := runMonodev(t, "pull")
	if !strings.Contains(pullOutB, "shared-store") {
		t.Fatalf("client B bootstrap pull output = %q, want shared-store", pullOutB)
	}

	pulledFile := filepath.Join(clientBRepo, ".monodev", "stores", "shared-store", "overlay", "tools", "hello.sh")
	pulledContent, err := os.ReadFile(pulledFile)
	if err != nil {
		t.Fatalf("expected client B to have pulled store content at %s: %v", pulledFile, err)
	}
	if string(pulledContent) != "echo hi\n" {
		t.Fatalf("pulled content = %q, want %q", pulledContent, "echo hi\n")
	}

	// A pulled store isn't active until selected - commit (and therefore
	// sync) needs an active store to know what to commit.
	runMonodev(t, "checkout", "shared-store")

	syncOutB := runMonodev(t, "sync")
	if !strings.Contains(syncOutB, "Pushed") || !strings.Contains(syncOutB, "shared-store") {
		t.Fatalf("client B sync output = %q, want a push summary naming shared-store", syncOutB)
	}
	if !strings.Contains(syncOutB, "Pulled") || !strings.Contains(syncOutB, "shared-store") {
		t.Fatalf("client B sync output = %q, want a pull summary naming shared-store", syncOutB)
	}
}

// TestSyncCommand_NoRemoteConfiguredNamesRemoteUse verifies that, without a
// configured remote, `monodev sync` fails fast with a message pointing at
// the fix instead of surfacing a raw transport error.
func TestSyncCommand_NoRemoteConfiguredNamesRemoteUse(t *testing.T) {
	baseDir := t.TempDir()
	repo := filepath.Join(baseDir, "repo")
	if err := os.MkdirAll(repo, 0755); err != nil {
		t.Fatal(err)
	}
	runSyncTestGit(t, repo, "init")

	oldWd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldWd) })

	t.Setenv("HOME", filepath.Join(baseDir, "home"))
	t.Setenv("MONODEV_ROOT", "")
	if err := os.Chdir(repo); err != nil {
		t.Fatal(err)
	}

	root := cli.RootCommand()
	resetCLIFlags(root)
	root.SetArgs([]string{"sync"})
	var execErr error
	_ = captureMonodevStdout(t, func() {
		execErr = root.Execute()
	})
	if execErr == nil {
		t.Fatal("expected sync to fail when no remote is configured")
	}
	if !strings.Contains(execErr.Error(), "monodev remote use") {
		t.Fatalf("error = %v, want it to name 'monodev remote use'", execErr)
	}
}
