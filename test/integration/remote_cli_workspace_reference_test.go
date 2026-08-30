//go:build integration
// +build integration

package integration

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// buildMonodevBinary compiles cmd/monodev so the regressions below exercise
// the real CLI entry point rather than the syncer API. Go's build cache keeps
// repeat builds cheap.
func buildMonodevBinary(t *testing.T) string {
	t.Helper()
	binary := filepath.Join(t.TempDir(), "monodev")
	cmd := exec.Command("go", "build", "-o", binary, "./cmd/monodev")
	cmd.Dir = filepath.Join("..", "..")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build monodev CLI: %v\n%s", err, out)
	}
	return binary
}

// cliClient is one checkout driven through the real monodev binary, with its
// own MONODEV_ROOT and HOME so clients never share state.
type cliClient struct {
	t           *testing.T
	binary      string
	repoRoot    string
	monodevRoot string
	env         []string
}

func newCLIClient(t *testing.T, binary, baseDir, name, originURL string) *cliClient {
	t.Helper()
	repoRoot := filepath.Join(baseDir, name)
	monodevRoot := filepath.Join(baseDir, name+"-monodev-root")
	home := filepath.Join(baseDir, name+"-home")
	for _, dir := range []string{repoRoot, monodevRoot, home} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatal(err)
		}
	}
	runGit(t, repoRoot, "init")
	runGit(t, repoRoot, "config", "user.email", "monodev-integration@example.com")
	runGit(t, repoRoot, "config", "user.name", "Monodev Integration Test")
	runGit(t, repoRoot, "remote", "add", "origin", originURL)

	return &cliClient{
		t:        t,
		binary:   binary,
		repoRoot: repoRoot,
		env: append(os.Environ(),
			"HOME="+home,
			"MONODEV_ROOT="+monodevRoot,
			"GIT_AUTHOR_NAME=Monodev Integration Test",
			"GIT_AUTHOR_EMAIL=monodev-integration@example.com",
			"GIT_COMMITTER_NAME=Monodev Integration Test",
			"GIT_COMMITTER_EMAIL=monodev-integration@example.com",
		),
		monodevRoot: monodevRoot,
	}
}

func (c *cliClient) run(args ...string) (string, error) {
	c.t.Helper()
	cmd := exec.Command(c.binary, args...)
	cmd.Dir = c.repoRoot
	cmd.Env = c.env
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func (c *cliClient) mustRun(args ...string) string {
	c.t.Helper()
	out, err := c.run(args...)
	if err != nil {
		c.t.Fatalf("monodev %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return out
}

// TestCLI_PullWorkspaceReferenceAcceptsAliasedLocalRemote reproduces the
// macOS `/tmp` versus `/private/tmp` report with a symlinked parent
// directory, which behaves the same way on every platform CI runs on.
func TestCLI_PullWorkspaceReferenceAcceptsAliasedLocalRemote(t *testing.T) {
	binary := buildMonodevBinary(t)
	baseDir := t.TempDir()
	realDir := filepath.Join(baseDir, "real")
	if err := os.MkdirAll(realDir, 0755); err != nil {
		t.Fatal(err)
	}
	aliasDir := filepath.Join(baseDir, "alias")
	if err := os.Symlink(realDir, aliasDir); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	bareRemote := filepath.Join(realDir, "remote.git")
	if err := os.MkdirAll(bareRemote, 0755); err != nil {
		t.Fatal(err)
	}
	runGit(t, bareRemote, "init", "--bare")

	aliasedRemote := filepath.Join(aliasDir, "remote.git")
	if aliasedRemote == bareRemote {
		t.Fatal("alias and canonical remote spellings must differ")
	}

	producer := newCLIClient(t, binary, baseDir, "producer", bareRemote)
	producer.mustRun("init")
	producer.mustRun("remote", "use", "origin")
	if err := os.MkdirAll(filepath.Join(producer.repoRoot, "tools"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(producer.repoRoot, "tools", "hello.sh"), []byte("echo hi\n"), 0644); err != nil {
		t.Fatal(err)
	}
	producer.mustRun("checkout", "--new", "qa-store")
	producer.mustRun("track", "tools")
	producer.mustRun("commit", "--all")

	workspaceID := cliStatusWorkspaceID(t, producer.mustRun("status", "--json"))
	producer.mustRun("push", "qa-store", "--with-workspace")

	// The consumer reaches the very same bare repository through the alias.
	consumer := newCLIClient(t, binary, baseDir, "consumer", aliasedRemote)
	consumer.mustRun("init")
	consumer.mustRun("remote", "use", "origin")

	out := consumer.mustRun("pull", "--workspace", workspaceID, "--with-stores", "--json")
	var result struct {
		PulledStores                []string
		PulledWorkspace             bool
		WorkspaceReferenceFound     bool
		WorkspaceReferenceValidated bool
		WorkspaceID                 string
		Verified                    bool
	}
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("pull JSON: %v\n%s", err, out)
	}
	if !result.WorkspaceReferenceFound || !result.WorkspaceReferenceValidated || !result.PulledWorkspace {
		t.Fatalf("pull result = %#v, want the reference found, validated, and restored", result)
	}
	if result.WorkspaceID == "" || result.WorkspaceID == workspaceID {
		t.Fatalf("restored WorkspaceID = %q, want the consumer's own local workspace ID", result.WorkspaceID)
	}
	if !result.Verified {
		t.Errorf("pull result = %#v, want verified stores", result)
	}
	restored := consumer.stateFilePath(result.WorkspaceID)
	if _, err := os.Stat(restored); err != nil {
		t.Fatalf("restored workspace state %s: %v", restored, err)
	}
}

// TestCLI_PullWorkspaceReferenceRejectsUnrelatedRepository keeps the guard
// closed: the consumer can reach the persistence branch through a second
// remote, but its own repository identity belongs to a different project.
func TestCLI_PullWorkspaceReferenceRejectsUnrelatedRepository(t *testing.T) {
	binary := buildMonodevBinary(t)
	baseDir := t.TempDir()
	bareRemote := filepath.Join(baseDir, "remote.git")
	if err := os.MkdirAll(bareRemote, 0755); err != nil {
		t.Fatal(err)
	}
	runGit(t, bareRemote, "init", "--bare")

	producer := newCLIClient(t, binary, baseDir, "producer", bareRemote)
	producer.mustRun("init")
	producer.mustRun("remote", "use", "origin")
	if err := os.WriteFile(filepath.Join(producer.repoRoot, "notes.txt"), []byte("notes\n"), 0644); err != nil {
		t.Fatal(err)
	}
	producer.mustRun("checkout", "--new", "qa-store")
	producer.mustRun("track", "notes.txt")
	producer.mustRun("commit", "--all")
	workspaceID := cliStatusWorkspaceID(t, producer.mustRun("status", "--json"))
	producer.mustRun("push", "qa-store", "--with-workspace")

	stranger := newCLIClient(t, binary, baseDir, "stranger", "https://example.test/unrelated/project.git")
	runGit(t, stranger.repoRoot, "remote", "add", "persist", bareRemote)
	stranger.mustRun("init")
	stranger.mustRun("remote", "use", "persist")

	out, err := stranger.run("pull", "--workspace", workspaceID, "--with-stores")
	if err == nil {
		t.Fatalf("pull from an unrelated repository succeeded:\n%s", out)
	}
	if !strings.Contains(out, "workspace reference repository mismatch") {
		t.Fatalf("pull error = %q, want a repository mismatch", out)
	}
	entries, readErr := os.ReadDir(filepath.Join(stranger.monodevRoot, "workspaces"))
	if readErr == nil && len(entries) > 0 {
		t.Fatalf("refused pull wrote workspace state: %v", entries)
	}
}

func (c *cliClient) stateFilePath(workspaceID string) string {
	return filepath.Join(c.monodevRoot, "workspaces", workspaceID+".json")
}

func cliStatusWorkspaceID(t *testing.T, out string) string {
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
