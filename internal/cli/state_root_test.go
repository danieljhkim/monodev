package cli

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/danieljhkim/monodev/internal/config"
	"github.com/danieljhkim/monodev/internal/fsops"
	"github.com/danieljhkim/monodev/internal/stores"
)

func TestCheckoutNew_AutoCreatesRepoLocalRoot(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("MONODEV_ROOT", "")

	repo := initGitRepo(t, t.TempDir(), "https://example.com/monodev.git")
	chdir(t, repo)

	runCLI(t, "checkout", "-n", "foo")

	monodevPath := filepath.Join(repo, config.RepoLocalDirName)
	info, err := os.Stat(monodevPath)
	if err != nil {
		t.Fatalf("expected auto-created .monodev: %v", err)
	}
	if !info.IsDir() {
		t.Fatal("expected .monodev to be a directory")
	}
	if got := info.Mode().Perm(); got != 0700 {
		t.Errorf(".monodev mode = %04o, want 0700", got)
	}

	gitignore, err := os.ReadFile(filepath.Join(monodevPath, ".gitignore"))
	if err != nil {
		t.Fatalf("read .gitignore: %v", err)
	}
	if string(gitignore) != config.RepoLocalGitignore {
		t.Errorf(".gitignore = %q, want %q", gitignore, config.RepoLocalGitignore)
	}

	if _, err := os.Stat(filepath.Join(monodevPath, "stores", "foo")); err != nil {
		t.Fatalf("expected repo-local store foo: %v", err)
	}

	status := gitPorcelain(t, repo)
	if status != "" {
		t.Fatalf("git status --porcelain not clean after auto-create:\n%s", status)
	}
}

func TestExistingHomeStoresRemainReachable(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("MONODEV_ROOT", "")

	seedHomeStore(t, home, "legacy-home")

	repo := initGitRepo(t, t.TempDir(), "https://example.com/monodev.git")
	chdir(t, repo)

	out := runCLI(t, "store", "ls", "--json")
	if !storeListContains(t, out, "legacy-home") {
		t.Fatalf("existing ~/.monodev store was silently unreachable; store ls --json = %s", out)
	}

	runCLI(t, "checkout", "legacy-home")
}

func TestMonodevRootOptInUsesHomeRoot(t *testing.T) {
	home := t.TempDir()
	monodevRoot := filepath.Join(home, config.RepoLocalDirName)
	t.Setenv("HOME", home)
	t.Setenv("MONODEV_ROOT", monodevRoot)

	repo := initGitRepo(t, t.TempDir(), "https://example.com/monodev.git")
	chdir(t, repo)

	runCLI(t, "checkout", "-n", "shared")

	if _, err := os.Stat(filepath.Join(monodevRoot, "stores", "shared")); err != nil {
		t.Fatalf("expected store under MONODEV_ROOT: %v", err)
	}
	if _, err := os.Stat(filepath.Join(repo, config.RepoLocalDirName)); !os.IsNotExist(err) {
		t.Fatal("MONODEV_ROOT opt-in should not auto-create repo-local .monodev")
	}
}

func TestCommandsOutsideGitRepoMatchInitError(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("MONODEV_ROOT", "")

	dir := t.TempDir()
	chdir(t, dir)

	initErr := runCLIErr(t, "init")
	if initErr == nil {
		t.Fatal("expected init to fail outside a git repository")
	}
	checkoutErr := runCLIErr(t, "checkout", "-n", "foo")
	if checkoutErr == nil {
		t.Fatal("expected checkout to fail outside a git repository")
	}

	for _, err := range []error{initErr, checkoutErr} {
		got := err.Error()
		if !strings.Contains(got, "not in a git repository") ||
			!strings.Contains(got, "monodev init must be run inside a git repository") {
			t.Errorf("error = %q, want the historical init not-in-git-repository message", got)
		}
	}
}

func TestInitForceRemainsIdempotent(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("MONODEV_ROOT", "")

	repo := initGitRepo(t, t.TempDir(), "https://example.com/monodev.git")
	chdir(t, repo)

	runCLI(t, "init")
	if err := runCLIErr(t, "init"); err == nil {
		t.Fatal("expected second init without --force to fail")
	} else if !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("second init error = %v, want already exists", err)
	}

	runCLI(t, "init", "--force")

	info, err := os.Stat(filepath.Join(repo, config.RepoLocalDirName))
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0700 {
		t.Errorf(".monodev mode = %04o, want 0700", got)
	}
	gitignore, err := os.ReadFile(filepath.Join(repo, config.RepoLocalDirName, ".gitignore"))
	if err != nil {
		t.Fatal(err)
	}
	if string(gitignore) != config.RepoLocalGitignore {
		t.Errorf(".gitignore after --force = %q, want %q", gitignore, config.RepoLocalGitignore)
	}
}

func TestInitForceJSONOutputIsParseable(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("MONODEV_ROOT", "")

	repo := initGitRepo(t, t.TempDir(), "https://example.com/monodev.git")
	chdir(t, repo)

	runCLI(t, "init")
	runCLI(t, "checkout", "-n", "keep")
	storeDir := filepath.Join(repo, config.RepoLocalDirName, "stores", "keep")
	if _, err := os.Stat(storeDir); err != nil {
		t.Fatalf("expected store before reinit: %v", err)
	}

	out := runCLI(t, "init", "--force", "--json")

	var result struct {
		Initialized bool   `json:"initialized"`
		Path        string `json:"path"`
	}
	dec := json.NewDecoder(strings.NewReader(out))
	if err := dec.Decode(&result); err != nil {
		t.Fatalf("init --force --json stdout is not JSON: %v\n%s", err, out)
	}
	if dec.More() {
		t.Fatalf("init --force --json stdout has trailing content:\n%s", out)
	}
	if !result.Initialized {
		t.Errorf("initialized = false, want true\n%s", out)
	}
	if want := filepath.Join(repo, config.RepoLocalDirName); !samePath(t, result.Path, want) {
		t.Errorf("path = %q, want %q", result.Path, want)
	}
	if _, err := os.Stat(storeDir); err != nil {
		t.Errorf("existing store lost after init --force --json: %v", err)
	}
}

func TestInitForcePlainOutputReportsReinitialization(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("MONODEV_ROOT", "")

	repo := initGitRepo(t, t.TempDir(), "https://example.com/monodev.git")
	chdir(t, repo)

	runCLI(t, "init")
	out := runCLI(t, "init", "--force")

	if !strings.Contains(out, "reinitializing with --force") {
		t.Errorf("plain init --force output missing reinitialization notice:\n%s", out)
	}
	if !strings.Contains(out, "Initialized .monodev") {
		t.Errorf("plain init --force output missing success message:\n%s", out)
	}
}

func samePath(t *testing.T, a, b string) bool {
	t.Helper()
	ra, errA := filepath.EvalSymlinks(a)
	rb, errB := filepath.EvalSymlinks(b)
	if errA != nil || errB != nil {
		return a == b
	}
	return ra == rb
}

func seedHomeStore(t *testing.T, home, id string) {
	t.Helper()
	storesDir := filepath.Join(home, config.RepoLocalDirName, "stores")
	if err := os.MkdirAll(storesDir, 0700); err != nil {
		t.Fatal(err)
	}
	repo := stores.NewFileStoreRepo(fsops.NewRealFS(), storesDir)
	if err := repo.Create(id, stores.NewStoreMeta(id, time.Now())); err != nil {
		t.Fatal(err)
	}
}

func storeListContains(t *testing.T, raw, id string) bool {
	t.Helper()
	var listed []struct {
		ID string
	}
	if err := json.Unmarshal([]byte(raw), &listed); err != nil {
		t.Fatalf("store ls JSON: %v\n%s", err, raw)
	}
	for _, store := range listed {
		if store.ID == id {
			return true
		}
	}
	return false
}

func gitPorcelain(t *testing.T, dir string) string {
	t.Helper()
	cmd := exec.Command("git", "status", "--porcelain", "--untracked-files=all")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git status --porcelain: %v\n%s", err, out)
	}
	return strings.TrimSpace(string(out))
}

func runCLIErr(t *testing.T, args ...string) error {
	t.Helper()
	resetCommandFlags(rootCmd)
	rootCmd.SetArgs(append([]string{}, args...))
	var execErr error
	_ = captureStdout(t, func() {
		execErr = rootCmd.Execute()
	})
	return execErr
}
