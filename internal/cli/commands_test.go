package cli

import (
	"bytes"
	"encoding/json"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/spf13/cobra"
)

// setupTestEnv creates a temporary directory structure for testing
func setupTestEnv(t *testing.T) (string, func()) {
	tmpDir, err := os.MkdirTemp("", "monodev-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}

	// Create a mock git repo structure
	gitDir := filepath.Join(tmpDir, "repo")
	if err := os.MkdirAll(gitDir, 0755); err != nil {
		t.Fatalf("Failed to create repo dir: %v", err)
	}

	// Create .git directory to simulate git repo
	gitDotDir := filepath.Join(gitDir, ".git")
	if err := os.MkdirAll(gitDotDir, 0755); err != nil {
		t.Fatalf("Failed to create .git dir: %v", err)
	}

	// Create workspace directory
	workspaceDir := filepath.Join(gitDir, "workspace")
	if err := os.MkdirAll(workspaceDir, 0755); err != nil {
		t.Fatalf("Failed to create workspace dir: %v", err)
	}

	// Set HOME to tmpDir so config uses test directory
	oldHome := os.Getenv("HOME")
	_ = os.Setenv("HOME", tmpDir)

	cleanup := func() {
		_ = os.Setenv("HOME", oldHome)
		_ = os.RemoveAll(tmpDir)
	}

	return workspaceDir, cleanup
}

func TestManagedExcludesPreserveOtherWorkspaceAndUserContent(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("MONODEV_ROOT", "")
	repo := initGitRepo(t, t.TempDir(), "https://example.com/monodev.git")
	runGit(t, repo, "commit", "--allow-empty", "-m", "initial")
	runCLIInDir(t, repo, "init")

	excludePath := filepath.Join(repo, ".git", "info", "exclude")
	userExclude := []byte("# user-owned\n/local-cache\n")
	if err := os.WriteFile(excludePath, userExclude, 0600); err != nil {
		t.Fatalf("seed user exclude content: %v", err)
	}

	for _, workspace := range []struct {
		name  string
		store string
		file  string
	}{
		{name: "a", store: "store-a", file: "context-a.txt"},
		{name: "b", store: "store-b", file: "context-b.txt"},
	} {
		workspaceDir := filepath.Join(repo, workspace.name)
		if err := os.MkdirAll(workspaceDir, 0755); err != nil {
			t.Fatalf("create workspace %s: %v", workspace.name, err)
		}
		runCLIInDir(t, workspaceDir, "checkout", "--new", workspace.store)
		path := filepath.Join(workspaceDir, workspace.file)
		if err := os.WriteFile(path, []byte(workspace.name+" overlay\n"), 0600); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
		runCLIInDir(t, workspaceDir, "track", workspace.file)
		runCLIInDir(t, workspaceDir, "commit", "--all")
		if err := os.Remove(path); err != nil {
			t.Fatalf("remove source %s: %v", path, err)
		}
		runCLIInDir(t, workspaceDir, "apply")
	}

	requireExcludeContains(t, excludePath, string(userExclude), "/a/context-a.txt", "/b/context-b.txt")
	if status := gitPorcelain(t, repo); status != "" {
		t.Fatalf("applied workspace files should be ignored, status = %q", status)
	}

	runCLIInDir(t, filepath.Join(repo, "b"), "unapply")
	requireExcludeContains(t, excludePath, string(userExclude), "/a/context-a.txt")
	requireExcludeNotContains(t, excludePath, "/b/context-b.txt")

	runCLIInDir(t, filepath.Join(repo, "a"), "eject", "--yes")
	contents, err := os.ReadFile(excludePath)
	if err != nil {
		t.Fatalf("read exclude after eject: %v", err)
	}
	if string(contents) != string(userExclude) {
		t.Fatalf("exclude after eject = %q, want user bytes %q", contents, userExclude)
	}
}

func TestManagedExcludesSerializeConcurrentLinkedWorktreeApplies(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("MONODEV_ROOT", "")
	repo := initGitRepo(t, t.TempDir(), "https://example.com/monodev.git")
	runGit(t, repo, "commit", "--allow-empty", "-m", "initial")
	linked := filepath.Join(t.TempDir(), "linked")
	runGit(t, repo, "worktree", "add", "-b", "linked-branch", linked)

	for _, workspace := range []struct {
		dir   string
		store string
		file  string
	}{
		{dir: repo, store: "main-store", file: "main-context.txt"},
		{dir: linked, store: "linked-store", file: "linked-context.txt"},
	} {
		runCLIInDir(t, workspace.dir, "init")
		runCLIInDir(t, workspace.dir, "checkout", "--new", workspace.store)
		path := filepath.Join(workspace.dir, workspace.file)
		if err := os.WriteFile(path, []byte(workspace.file+"\n"), 0600); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
		runCLIInDir(t, workspace.dir, "track", workspace.file)
		runCLIInDir(t, workspace.dir, "commit", "--all")
		if err := os.Remove(path); err != nil {
			t.Fatalf("remove source %s: %v", path, err)
		}
	}

	binary := filepath.Join(t.TempDir(), "monodev")
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("resolve source root: %v", err)
	}
	goCacheRoot := t.TempDir()
	goEnv := isolatedGoTestEnv(goCacheRoot)
	t.Cleanup(func() {
		clean := exec.Command("go", "clean", "-cache", "-modcache")
		clean.Dir = root
		clean.Env = goEnv
		if output, err := clean.CombinedOutput(); err != nil {
			t.Errorf("clean isolated Go caches: %v\n%s", err, output)
		}
	})
	build := exec.Command("go", "build", "-o", binary, "./cmd/monodev")
	build.Dir = root
	build.Env = goEnv
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build monodev test binary: %v\n%s", err, output)
	}

	type commandResult struct {
		dir    string
		output []byte
		err    error
	}
	results := make(chan commandResult, 2)
	for _, dir := range []string{repo, linked} {
		go func(dir string) {
			cmd := exec.Command(binary, "apply")
			cmd.Dir = dir
			output, err := cmd.CombinedOutput()
			results <- commandResult{dir: dir, output: output, err: err}
		}(dir)
	}
	for range 2 {
		result := <-results
		if result.err != nil {
			t.Fatalf("concurrent apply in %s: %v\n%s", result.dir, result.err, result.output)
		}
	}

	for _, dir := range []string{repo, linked} {
		cmd := exec.Command("git", "rev-parse", "--path-format=absolute", "--git-common-dir")
		cmd.Dir = dir
		output, err := cmd.Output()
		if err != nil {
			t.Fatalf("resolve common git dir for %s: %v", dir, err)
		}
		wantGitDir, err := filepath.EvalSymlinks(filepath.Join(repo, ".git"))
		if err != nil {
			t.Fatalf("resolve expected common git dir: %v", err)
		}
		if filepath.Clean(strings.TrimSpace(string(output))) != filepath.Clean(wantGitDir) {
			t.Fatalf("common git dir for %s = %q, want %q", dir, output, wantGitDir)
		}
	}
	requireExcludeContains(t, filepath.Join(repo, ".git", "info", "exclude"), "/main-context.txt", "/linked-context.txt")
}

func isolatedGoTestEnv(cacheRoot string) []string {
	overrides := map[string]struct{}{
		"GOCACHE":    {},
		"GOMODCACHE": {},
		"GOPATH":     {},
	}
	env := make([]string, 0, len(os.Environ())+3)
	for _, entry := range os.Environ() {
		key, _, ok := strings.Cut(entry, "=")
		if _, overridden := overrides[key]; ok && overridden {
			continue
		}
		env = append(env, entry)
	}
	env = append(env,
		"GOCACHE="+filepath.Join(cacheRoot, "build"),
		"GOMODCACHE="+filepath.Join(cacheRoot, "mod"),
		"GOPATH="+filepath.Join(cacheRoot, "go"),
	)
	return env
}

func runCLIInDir(t *testing.T, dir string, args ...string) {
	t.Helper()
	old, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir %s: %v", dir, err)
	}
	defer func() { _ = os.Chdir(old) }()
	runCLI(t, args...)
}

func requireExcludeContains(t *testing.T, path string, want ...string) {
	t.Helper()
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	for _, entry := range want {
		if !strings.Contains(string(contents), entry) {
			t.Fatalf("exclude %s missing %q:\n%s", path, entry, contents)
		}
	}
}

func requireExcludeNotContains(t *testing.T, path string, unwanted ...string) {
	t.Helper()
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	for _, entry := range unwanted {
		if strings.Contains(string(contents), entry) {
			t.Fatalf("exclude %s unexpectedly contains %q:\n%s", path, entry, contents)
		}
	}
}

func TestStoreLsCommand_NoStores(t *testing.T) {
	workspaceDir, cleanup := setupTestEnv(t)
	defer cleanup()

	// Change to workspace directory
	oldDir, _ := os.Getwd()
	_ = os.Chdir(workspaceDir)
	defer func() {
		_ = os.Chdir(oldDir)
	}()

	rootCmd.SetArgs([]string{"store", "ls"})
	var bufOut, bufErr bytes.Buffer
	rootCmd.SetOut(&bufOut)
	rootCmd.SetErr(&bufErr)

	err := rootCmd.Execute()
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	output := bufOut.String() + bufErr.String()
	if output != "" {
		// Should be valid JSON
		var v interface{}
		if err := json.Unmarshal([]byte(output), &v); err != nil {
			t.Errorf("expected valid JSON output, got error: %v, output: %q", err, output)
		}
	}
}

func TestStoreLsCommand_JSONOutput(t *testing.T) {
	workspaceDir, cleanup := setupTestEnv(t)
	defer cleanup()

	oldDir, _ := os.Getwd()
	_ = os.Chdir(workspaceDir)
	defer func() {
		_ = os.Chdir(oldDir)
	}()

	rootCmd.SetArgs([]string{"store", "ls", "--json"})
	var buf bytes.Buffer
	rootCmd.SetOut(&buf)

	err := rootCmd.Execute()
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	output := buf.String()
	// Trim whitespace and newlines
	output = string(bytes.TrimSpace([]byte(output)))
	if output == "" {
		t.Skip("No output to validate (empty stores list)")
		return
	}
	// Should be valid JSON
	var v interface{}
	if err := json.Unmarshal([]byte(output), &v); err != nil {
		t.Errorf("expected valid JSON output, got error: %v, output: %q", err, output)
	}
}

func TestStatusCommand_NoWorkspaceState(t *testing.T) {
	workspaceDir, cleanup := setupTestEnv(t)
	defer cleanup()

	oldDir, _ := os.Getwd()
	_ = os.Chdir(workspaceDir)
	defer func() {
		_ = os.Chdir(oldDir)
	}()

	rootCmd.SetArgs([]string{"status"})
	var buf bytes.Buffer
	rootCmd.SetOut(&buf)

	err := rootCmd.Execute()
	if err != nil {
		t.Fatalf("Status command error = %v", err)
	}
}

func TestStatusCommand_JSONOutput(t *testing.T) {
	workspaceDir, cleanup := setupTestEnv(t)
	defer cleanup()

	oldDir, _ := os.Getwd()
	_ = os.Chdir(workspaceDir)
	defer func() {
		_ = os.Chdir(oldDir)
	}()

	rootCmd.SetArgs([]string{"status", "--json"})
	var buf bytes.Buffer
	rootCmd.SetOut(&buf)

	err := rootCmd.Execute()
	if err != nil {
		t.Fatalf("Status command error = %v", err)
	}

	output := buf.String()
	if output != "" {
		// If there's output, it should be valid JSON
		var v interface{}
		if err := json.Unmarshal([]byte(output), &v); err != nil {
			t.Errorf("expected valid JSON output, got error: %v", err)
		}
	}
}

func TestCheckoutCommand_RemovedOwnerFlag(t *testing.T) {
	resetCommandFlags(rootCmd)
	rootCmd.SetArgs([]string{"checkout", "-n", "foo", "--owner", "bar"})
	err := rootCmd.Execute()
	if err == nil {
		t.Fatal("expected error for removed --owner flag")
	}
	msg := err.Error()
	if !strings.Contains(msg, "--owner") || !strings.Contains(strings.ToLower(msg), "removed") {
		t.Fatalf("error = %q, want an error that names --owner as removed", msg)
	}
	if strings.Contains(msg, "unknown flag") {
		t.Fatalf("error = %q, must not be a bare unknown-flag message", msg)
	}
}

func TestCheckoutCommand_DescriptionRoundTripAndDescribeJSON(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("MONODEV_ROOT", "")
	repo := initGitRepo(t, t.TempDir(), "https://example.com/monodev.git")
	chdir(t, repo)
	runCLI(t, "init")
	runCLI(t, "checkout", "-n", "foo", "--description", "x")

	out := runCLI(t, "store", "describe", "foo")
	if !strings.Contains(out, "x") {
		t.Fatalf("describe output = %q, want description x", out)
	}

	jsonOut := runCLI(t, "store", "describe", "--json", "foo")
	var parsed any
	if err := json.Unmarshal([]byte(jsonOut), &parsed); err != nil {
		t.Fatalf("describe --json is not JSON: %v\n%s", err, jsonOut)
	}
	lower := strings.ToLower(jsonOut)
	for _, key := range []string{`"owner"`, `"taskid"`, `"scope"`} {
		if strings.Contains(lower, key) {
			t.Fatalf("describe --json contains %s: %s", key, jsonOut)
		}
	}
}

func TestStoreCloneCommand_CreatesIndependentStoreWithoutChangingActiveStore(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("MONODEV_ROOT", "")
	repo := initGitRepo(t, t.TempDir(), "https://example.com/monodev.git")
	chdir(t, repo)
	runCLI(t, "init")
	runCLI(t, "checkout", "-n", "source")

	sourceFile := filepath.Join(repo, "bin", "tool")
	if err := os.MkdirAll(filepath.Dir(sourceFile), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(sourceFile, []byte("#!/bin/sh\necho source\n"), 0755); err != nil {
		t.Fatal(err)
	}
	runCLI(t, "track", "bin/tool", "--role", "script", "--origin", "agent")
	runCLI(t, "commit", "--all")

	before := runCLI(t, "status", "--json")
	runCLI(t, "store", "clone", "source", "variant")
	after := runCLI(t, "status", "--json")
	var beforeStatus, afterStatus struct {
		ActiveStore string
		WorkspaceID string
	}
	if err := json.Unmarshal([]byte(before), &beforeStatus); err != nil {
		t.Fatalf("status before clone JSON: %v\n%s", err, before)
	}
	if err := json.Unmarshal([]byte(after), &afterStatus); err != nil {
		t.Fatalf("status after clone JSON: %v\n%s", err, after)
	}
	if afterStatus != beforeStatus {
		t.Fatalf("clone changed workspace state: before=%#v after=%#v", beforeStatus, afterStatus)
	}

	sourceOverlay := filepath.Join(repo, ".monodev", "stores", "source", "overlay", "bin", "tool")
	destinationOverlay := filepath.Join(repo, ".monodev", "stores", "variant", "overlay", "bin", "tool")
	sourceData, err := os.ReadFile(sourceOverlay)
	if err != nil {
		t.Fatalf("ReadFile(source overlay): %v", err)
	}
	destinationData, err := os.ReadFile(destinationOverlay)
	if err != nil {
		t.Fatalf("ReadFile(destination overlay): %v", err)
	}
	if string(destinationData) != string(sourceData) {
		t.Errorf("destination content = %q, want %q", destinationData, sourceData)
	}
	if err := os.WriteFile(destinationOverlay, []byte("#!/bin/sh\necho variant\n"), 0700); err != nil {
		t.Fatalf("WriteFile(destination overlay): %v", err)
	}
	sourceData, err = os.ReadFile(sourceOverlay)
	if err != nil {
		t.Fatalf("ReadFile(source overlay after mutation): %v", err)
	}
	if string(sourceData) != "#!/bin/sh\necho source\n" {
		t.Errorf("source content after destination mutation = %q", sourceData)
	}
}

func TestCheckoutCommand_InvalidArgs(t *testing.T) {
	workspaceDir, cleanup := setupTestEnv(t)
	defer cleanup()

	oldDir, _ := os.Getwd()
	_ = os.Chdir(workspaceDir)
	defer func() {
		_ = os.Chdir(oldDir)
	}()

	// Test with no args (should fail)
	rootCmd.SetArgs([]string{"checkout"})
	var buf bytes.Buffer
	rootCmd.SetErr(&buf)

	err := rootCmd.Execute()
	if err == nil {
		t.Error("expected error for checkout command with no args")
	}
}

func TestTrackCommand_InvalidArgs(t *testing.T) {
	workspaceDir, cleanup := setupTestEnv(t)
	defer cleanup()

	oldDir, _ := os.Getwd()
	_ = os.Chdir(workspaceDir)
	defer func() {
		_ = os.Chdir(oldDir)
	}()

	// Test with no args (should fail)
	rootCmd.SetArgs([]string{"track"})
	var buf bytes.Buffer
	rootCmd.SetErr(&buf)

	err := rootCmd.Execute()
	if err == nil {
		t.Error("expected error for track command with no args")
	}
}

func TestResolveAgentPresetPaths_UsesProvidedFilesystem(t *testing.T) {
	workspace := fstest.MapFS{
		".claude":         &fstest.MapFile{Mode: fs.ModeDir},
		"CLAUDE.md":       &fstest.MapFile{Data: []byte("instructions")},
		".aider.conf.yml": &fstest.MapFile{Data: []byte("model: test")},
		"unrelated.txt":   &fstest.MapFile{Data: []byte("not a preset")},
	}

	found, missing, err := resolveAgentPresetPaths(workspace)
	if err != nil {
		t.Fatalf("resolveAgentPresetPaths() error = %v", err)
	}

	if !containsString(found, ".claude") || !containsString(found, "CLAUDE.md") || !containsString(found, ".aider.conf.yml") {
		t.Fatalf("found = %v, want .claude, CLAUDE.md, and wildcard .aider match", found)
	}
	if containsString(found, "unrelated.txt") {
		t.Fatalf("found unrelated path: %v", found)
	}
	if !containsString(missing, ".cursor/") {
		t.Fatalf("missing = %v, want .cursor/", missing)
	}
}

func TestTrackAgentsCommand_TracksExistingAndReportsAbsent(t *testing.T) {
	repo := setupTrackAgentsTestRepo(t)
	if err := os.Mkdir(filepath.Join(repo, ".claude"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "CLAUDE.md"), []byte("instructions\n"), 0644); err != nil {
		t.Fatal(err)
	}

	output := runCLI(t, "track", "--agents")
	if !strings.Contains(output, "Agent paths found: .claude, CLAUDE.md") {
		t.Fatalf("output = %q, want found agent paths", output)
	}
	if !strings.Contains(output, "Agent path skipped-absent: .cursor/") {
		t.Fatalf("output = %q, want skipped .cursor/", output)
	}

	paths := readTrackedPaths(t, filepath.Join(repo, ".monodev", "stores", "agents", "track.json"))
	if len(paths) != 2 || !containsString(paths, ".claude") || !containsString(paths, "CLAUDE.md") {
		t.Fatalf("tracked paths = %v, want exactly .claude and CLAUDE.md", paths)
	}
}

func TestTrackAgentsCommand_UnionsExplicitPaths(t *testing.T) {
	repo := setupTrackAgentsTestRepo(t)
	if err := os.Mkdir(filepath.Join(repo, ".claude"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "extra_file.py"), []byte("print('hi')\n"), 0644); err != nil {
		t.Fatal(err)
	}

	runCLI(t, "track", "--agents", "extra_file.py")
	paths := readTrackedPaths(t, filepath.Join(repo, ".monodev", "stores", "agents", "track.json"))
	if len(paths) != 2 || !containsString(paths, ".claude") || !containsString(paths, "extra_file.py") {
		t.Fatalf("tracked paths = %v, want .claude and extra_file.py", paths)
	}
}

func TestTrackAgentsCommand_SurvivesCommitAndApplyInSecondWorkspace(t *testing.T) {
	repo := setupTrackAgentsTestRepo(t)
	if err := os.Mkdir(filepath.Join(repo, ".claude"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "CLAUDE.md"), []byte("instructions\n"), 0644); err != nil {
		t.Fatal(err)
	}

	runCLI(t, "track", "--agents")
	runCLI(t, "commit", "--all")

	secondWorkspace := filepath.Join(repo, "second-workspace")
	if err := os.Mkdir(secondWorkspace, 0755); err != nil {
		t.Fatal(err)
	}
	chdir(t, secondWorkspace)
	runCLI(t, "checkout", "agents")
	runCLI(t, "apply")

	if _, err := os.Stat(filepath.Join(secondWorkspace, ".claude")); err != nil {
		t.Fatalf("applied .claude directory: %v", err)
	}
	if _, err := os.Stat(filepath.Join(secondWorkspace, "CLAUDE.md")); err != nil {
		t.Fatalf("applied CLAUDE.md: %v", err)
	}
}

func TestTrackAgentsCommand_NoMatchesSucceedsWithoutTracking(t *testing.T) {
	repo := setupTrackAgentsTestRepo(t)
	output := runCLI(t, "track", "--agents")
	if !strings.Contains(output, "No agent context paths found") {
		t.Fatalf("output = %q, want clear no-match message", output)
	}
	if !strings.Contains(output, "No paths tracked") {
		t.Fatalf("output = %q, want no-tracking message", output)
	}

	paths := readTrackedPaths(t, filepath.Join(repo, ".monodev", "stores", "agents", "track.json"))
	if len(paths) != 0 {
		t.Fatalf("tracked paths = %v, want none", paths)
	}
}

func setupTrackAgentsTestRepo(t *testing.T) string {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("MONODEV_ROOT", "")
	repo := initGitRepo(t, t.TempDir(), "https://example.com/monodev.git")
	chdir(t, repo)
	runCLI(t, "init")
	runCLI(t, "checkout", "--new", "agents")
	return repo
}

func readTrackedPaths(t *testing.T, trackPath string) []string {
	t.Helper()
	data, err := os.ReadFile(trackPath)
	if err != nil {
		t.Fatalf("read track file: %v", err)
	}
	var track struct {
		Tracked []struct {
			Path string `json:"path"`
		} `json:"tracked"`
	}
	if err := json.Unmarshal(data, &track); err != nil {
		t.Fatalf("unmarshal track file: %v", err)
	}
	paths := make([]string, 0, len(track.Tracked))
	for _, tracked := range track.Tracked {
		paths = append(paths, tracked.Path)
	}
	return paths
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func TestApplyCommand_InvalidStore(t *testing.T) {
	workspaceDir, cleanup := setupTestEnv(t)
	defer cleanup()

	oldDir, _ := os.Getwd()
	_ = os.Chdir(workspaceDir)
	defer func() {
		_ = os.Chdir(oldDir)
	}()

	// Try to apply a non-existent store
	rootCmd.SetArgs([]string{"apply", "nonexistent-store"})
	var bufOut, bufErr bytes.Buffer
	rootCmd.SetOut(&bufOut)
	rootCmd.SetErr(&bufErr)

	err := rootCmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "store 'nonexistent-store' not found") {
		t.Fatalf("Apply error = %v, want missing-store error", err)
	}
}

func TestApplyCommand_JSONConflictReturnsError(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("MONODEV_ROOT", "")
	repo := initGitRepo(t, t.TempDir(), "https://example.com/monodev.git")
	chdir(t, repo)
	runCLI(t, "checkout", "-n", "source")

	storeFile := filepath.Join(repo, "Makefile")
	if err := os.WriteFile(storeFile, []byte("store\n"), 0600); err != nil {
		t.Fatal(err)
	}
	runCLI(t, "track", "Makefile")
	runCLI(t, "commit", "--all")

	workspace := filepath.Join(repo, "conflict-workspace")
	if err := os.Mkdir(workspace, 0755); err != nil {
		t.Fatal(err)
	}
	chdir(t, workspace)
	runCLI(t, "checkout", "source")
	destination := filepath.Join(workspace, "Makefile")
	if err := os.WriteFile(destination, []byte("user\n"), 0600); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(destination)
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name string
		args []string
		json bool
	}{
		{name: "json apply", args: []string{"apply", "--json"}, json: true},
		{name: "plain apply", args: []string{"apply"}},
		{name: "json dry run", args: []string{"apply", "--json", "--dry-run"}, json: true},
		{name: "plain dry run", args: []string{"apply", "--dry-run"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resetCommandFlags(rootCmd)
			rootCmd.SetArgs(tt.args)
			var execErr error
			output := captureStdout(t, func() {
				execErr = rootCmd.Execute()
			})
			if execErr == nil {
				t.Fatal("expected conflict to return an error")
			}
			if !strings.Contains(execErr.Error(), "conflicts detected") {
				t.Fatalf("error = %q, want conflict failure", execErr)
			}

			if tt.json {
				var result struct {
					Plan struct {
						Conflicts []struct {
							Path   string
							Reason string
						} `json:"Conflicts"`
					} `json:"Plan"`
					Applied []any `json:"Applied"`
				}
				if err := json.Unmarshal([]byte(output), &result); err != nil {
					t.Fatalf("conflict output is not valid JSON: %v\n%s", err, output)
				}
				if len(result.Plan.Conflicts) != 1 || result.Plan.Conflicts[0].Path != "Makefile" || result.Plan.Conflicts[0].Reason == "" {
					t.Fatalf("JSON conflicts = %#v, want useful Makefile conflict", result.Plan.Conflicts)
				}
				if len(result.Applied) != 0 {
					t.Fatalf("JSON applied = %#v, want empty", result.Applied)
				}
			}

			after, err := os.ReadFile(destination)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(after, before) {
				t.Fatalf("destination changed after refused apply: got %q, want %q", after, before)
			}
		})
	}

	resetCommandFlags(rootCmd)
	rootCmd.SetArgs([]string{"apply", "--force", "--json"})
	var applyErr error
	output := captureStdout(t, func() {
		applyErr = rootCmd.Execute()
	})
	if applyErr != nil {
		t.Fatalf("forced JSON apply error = %v", applyErr)
	}
	var result struct {
		Applied []any `json:"Applied"`
	}
	if err := json.Unmarshal([]byte(output), &result); err != nil {
		t.Fatalf("successful apply output is not valid JSON: %v\n%s", err, output)
	}
	if len(result.Applied) == 0 {
		t.Fatalf("successful JSON applied = %#v, want applied operations", result.Applied)
	}
	after, err := os.ReadFile(destination)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != "store\n" {
		t.Fatalf("destination after forced apply = %q, want store content", after)
	}
}

func TestDoctorCommand_HealthyWorkspace(t *testing.T) {
	workspaceDir, cleanup := setupTestEnv(t)
	defer cleanup()

	oldDir, _ := os.Getwd()
	_ = os.Chdir(workspaceDir)
	defer func() {
		_ = os.Chdir(oldDir)
	}()

	rootCmd.SetArgs([]string{"doctor"})
	var bufOut, bufErr bytes.Buffer
	rootCmd.SetOut(&bufOut)
	rootCmd.SetErr(&bufErr)

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("doctor on a healthy workspace should exit zero, got error: %v", err)
	}
}

func TestDoctorCommand_JSONOutput(t *testing.T) {
	workspaceDir, cleanup := setupTestEnv(t)
	defer cleanup()

	oldDir, _ := os.Getwd()
	_ = os.Chdir(workspaceDir)
	defer func() {
		_ = os.Chdir(oldDir)
	}()

	rootCmd.SetArgs([]string{"doctor", "--json"})
	var buf bytes.Buffer
	rootCmd.SetOut(&buf)

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("doctor --json on a healthy workspace should exit zero, got error: %v", err)
	}

	output := bytes.TrimSpace(buf.Bytes())
	if len(output) == 0 {
		return
	}
	var v interface{}
	if err := json.Unmarshal(output, &v); err != nil {
		t.Errorf("expected valid JSON output, got error: %v, output: %q", err, output)
	}
}

func TestUnapplyCommand_NoState(t *testing.T) {
	workspaceDir, cleanup := setupTestEnv(t)
	defer cleanup()

	oldDir, _ := os.Getwd()
	_ = os.Chdir(workspaceDir)
	defer func() {
		_ = os.Chdir(oldDir)
	}()

	// Try to unapply when nothing is applied
	rootCmd.SetArgs([]string{"unapply"})
	var buf bytes.Buffer
	rootCmd.SetErr(&buf)

	err := rootCmd.Execute()
	// Should error because nothing is applied
	if err == nil {
		t.Error("expected error for unapply when nothing is applied")
	}
}

func TestUnapplyCommand_DryRun(t *testing.T) {
	workspaceDir, cleanup := setupTestEnv(t)
	defer cleanup()

	oldDir, _ := os.Getwd()
	_ = os.Chdir(workspaceDir)
	defer func() {
		_ = os.Chdir(oldDir)
	}()

	// Try dry-run unapply
	rootCmd.SetArgs([]string{"unapply", "--dry-run"})
	var buf bytes.Buffer
	rootCmd.SetOut(&buf)

	err := rootCmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "workspace has no managed paths") {
		t.Fatalf("Unapply --dry-run error = %v, want missing-workspace error", err)
	}
}

func TestApplyCommand_Flags(t *testing.T) {
	tests := []struct {
		name      string
		args      []string
		wantError string
	}{
		{"unsupported mode flag", []string{"apply", "--mode", "copy"}, "unknown flag: --mode"},
		{"force flag", []string{"apply", "--force"}, "no active store set"},
		{"dry-run flag", []string{"apply", "--dry-run"}, "no active store set"},
		{"all supported flags", []string{"apply", "--force", "--dry-run"}, "no active store set"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			workspaceDir, cleanup := setupTestEnv(t)
			defer cleanup()
			oldDir, _ := os.Getwd()
			if err := os.Chdir(workspaceDir); err != nil {
				t.Fatalf("Chdir() error = %v", err)
			}
			defer func() { _ = os.Chdir(oldDir) }()

			rootCmd.SetArgs(tt.args)
			err := rootCmd.Execute()
			if err == nil || !strings.Contains(err.Error(), tt.wantError) {
				t.Fatalf("Execute() error = %v, want %q", err, tt.wantError)
			}
		})
	}
}

func TestCheckoutCommand_Flags(t *testing.T) {
	tests := []struct {
		name      string
		args      []string
		wantError string
	}{
		{"new flag", []string{"checkout", "test-store", "--new"}, ""},
		{"description flag", []string{"checkout", "test-store", "--new", "--description", "test desc"}, ""},
		{"all supported flags", []string{"checkout", "test-store", "--new", "--description", "test"}, ""},
		{"removed scope flag", []string{"checkout", "test-store", "--new", "--scope", "global"}, "flag --scope has been removed"},
		{"removed owner flag", []string{"checkout", "test-store", "--new", "--owner", "test-owner"}, "flag --owner has been removed"},
		{"removed task-id flag", []string{"checkout", "test-store", "--new", "--task-id", "DANI-1"}, "flag --task-id has been removed"},
		{"retired type flag", []string{"checkout", "test-store", "--new", "--type", "issue"}, "unknown flag: --type"},
		{"retired priority flag", []string{"checkout", "test-store", "--new", "--priority", "high"}, "unknown flag: --priority"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			workspaceDir, cleanup := setupTestEnv(t)
			defer cleanup()
			oldDir, _ := os.Getwd()
			if err := os.Chdir(workspaceDir); err != nil {
				t.Fatalf("Chdir() error = %v", err)
			}
			defer func() { _ = os.Chdir(oldDir) }()

			rootCmd.SetArgs(tt.args)
			err := rootCmd.Execute()
			if tt.wantError == "" {
				if err != nil {
					t.Fatalf("Execute() error = %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantError) {
				t.Fatalf("Execute() error = %v, want %q", err, tt.wantError)
			}
		})
	}
}

func TestStoreUpdateCommand_Flags(t *testing.T) {
	tests := []struct {
		name      string
		args      []string
		wantError string
	}{
		{"description flag", []string{"store", "update", "test-store", "--description", "test desc"}, ""},
		{"removed scope flag", []string{"store", "update", "test-store", "--scope", "global"}, "flag --scope has been removed"},
		{"removed owner flag", []string{"store", "update", "test-store", "--owner", "test-owner"}, "flag --owner has been removed"},
		{"removed task-id flag", []string{"store", "update", "test-store", "--task-id", "DANI-1"}, "flag --task-id has been removed"},
		{"retired status flag", []string{"store", "update", "test-store", "--status", "done"}, "unknown flag: --status"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			workspaceDir, cleanup := setupTestEnv(t)
			defer cleanup()
			oldDir, _ := os.Getwd()
			if err := os.Chdir(workspaceDir); err != nil {
				t.Fatalf("Chdir() error = %v", err)
			}
			defer func() { _ = os.Chdir(oldDir) }()

			resetCommandFlags(rootCmd)
			rootCmd.SetArgs([]string{"checkout", "test-store", "--new"})
			if err := rootCmd.Execute(); err != nil {
				t.Fatalf("setup checkout error = %v", err)
			}

			rootCmd.SetArgs(tt.args)
			err := rootCmd.Execute()
			if tt.wantError == "" {
				if err != nil {
					t.Fatalf("Execute() error = %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantError) {
				t.Fatalf("Execute() error = %v, want %q", err, tt.wantError)
			}
		})
	}
}

func TestREADMECommandExamplesUseRegisteredSurface(t *testing.T) {
	assertDocumentedCommandsMatchBinary(t, filepath.Join("..", "..", "README.md"))
}

func TestDocsCommandExamplesUseRegisteredSurface(t *testing.T) {
	docsDir := filepath.Join("..", "..", "docs")
	entries, err := os.ReadDir(docsDir)
	if err != nil {
		t.Fatalf("ReadDir(docs) error = %v", err)
	}
	found := 0
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
			continue
		}
		found++
		assertDocumentedCommandsMatchBinary(t, filepath.Join(docsDir, entry.Name()))
	}
	if found == 0 {
		t.Fatal("docs/ contained no markdown files to check")
	}
}

func assertDocumentedCommandsMatchBinary(t *testing.T, path string) {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%s) error = %v", path, err)
	}
	rel := path
	if abs, absErr := filepath.Abs(path); absErr == nil {
		if repoRoot, repoErr := filepath.Abs(filepath.Join("..", "..")); repoErr == nil {
			if trimmed, trimErr := filepath.Rel(repoRoot, abs); trimErr == nil {
				rel = trimmed
			}
		}
	}

	flagPattern := regexp.MustCompile(`(?:^|\s)(--?[[:alpha:]][[:alnum:]-]*)`)
	for lineNumber, line := range strings.Split(string(body), "\n") {
		line = strings.TrimSpace(strings.SplitN(line, " #", 2)[0])
		if !strings.HasPrefix(line, "monodev ") {
			continue
		}

		fields := strings.Fields(line)
		var commandPath []string
		current := rootCmd
		for _, field := range fields[1:] {
			if strings.HasPrefix(field, "-") || strings.HasPrefix(field, "<") || strings.HasPrefix(field, "[") {
				break
			}
			var child *cobra.Command
			for _, candidate := range current.Commands() {
				if candidate.Name() == field {
					child = candidate
					break
				}
			}
			if child == nil {
				break
			}
			commandPath = append(commandPath, field)
			current = child
		}
		if len(commandPath) == 0 {
			t.Errorf("%s:%d: command path missing in %q", rel, lineNumber+1, line)
			continue
		}

		cmd, _, findErr := rootCmd.Find(commandPath)
		if findErr != nil || cmd == nil || cmd.Name() != commandPath[len(commandPath)-1] {
			t.Errorf("%s:%d: command %q is not registered (error: %v)", rel, lineNumber+1, strings.Join(commandPath, " "), findErr)
			continue
		}

		for _, match := range flagPattern.FindAllStringSubmatch(line, -1) {
			flagName := match[1]
			normalizedFlagName := strings.TrimLeft(flagName, "-")
			registered := cmd.Flags().Lookup(normalizedFlagName)
			if registered == nil && strings.HasPrefix(flagName, "-") && !strings.HasPrefix(flagName, "--") {
				registered = cmd.Flags().ShorthandLookup(normalizedFlagName)
			}
			if registered == nil {
				registered = cmd.InheritedFlags().Lookup(normalizedFlagName)
			}
			if registered == nil {
				t.Errorf("%s:%d: flag %s is not registered for %q", rel, lineNumber+1, flagName, strings.Join(commandPath, " "))
			}
		}
	}
}

func TestGlobalJSONFlag(t *testing.T) {
	workspaceDir, cleanup := setupTestEnv(t)
	defer cleanup()

	oldDir, _ := os.Getwd()
	_ = os.Chdir(workspaceDir)
	defer func() {
		_ = os.Chdir(oldDir)
	}()

	// Test that --json flag works globally
	rootCmd.SetArgs([]string{"store", "ls", "--json"})
	var buf bytes.Buffer
	rootCmd.SetOut(&buf)

	err := rootCmd.Execute()
	if err != nil {
		t.Fatalf("Command error = %v", err)
	}

	output := buf.String()
	if output != "" {
		// Should be valid JSON
		var v interface{}
		if err := json.Unmarshal([]byte(output), &v); err != nil {
			t.Errorf("expected valid JSON with --json flag, got error: %v", err)
		}
	}
}

func TestCommandHelp(t *testing.T) {
	commands := []string{"apply", "unapply", "status", "checkout", "track", "store", "workspace"}

	for _, cmd := range commands {
		t.Run(cmd, func(t *testing.T) {
			rootCmd.SetArgs([]string{cmd, "--help"})
			var buf bytes.Buffer
			rootCmd.SetOut(&buf)

			err := rootCmd.Execute()
			if err != nil {
				t.Errorf("Execute() for %s --help error = %v", cmd, err)
			}

			output := buf.String()
			if output == "" {
				t.Errorf("expected help output for %s, got empty", cmd)
			}
		})
	}
}

func TestRetiredStackAddNamesApplyReplacement(t *testing.T) {
	rootCmd.SetArgs([]string{"stack", "add", "x"})
	err := rootCmd.Execute()
	if err == nil {
		t.Fatal("expected stack add to fail")
	}
	if !strings.Contains(err.Error(), "apply") {
		t.Fatalf("stack add error = %q, want it to name apply", err.Error())
	}
}

func TestRetiredClearNamesWorkspaceRmReplacement(t *testing.T) {
	rootCmd.SetArgs([]string{"clear"})
	err := rootCmd.Execute()
	if err == nil {
		t.Fatal("expected clear to fail")
	}
	if !strings.Contains(err.Error(), "workspace rm") {
		t.Fatalf("clear error = %q, want it to name workspace rm", err.Error())
	}
}
