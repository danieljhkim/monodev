package cli

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/danieljhkim/monodev/internal/config"
	"github.com/danieljhkim/monodev/internal/engine"
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

func TestInitSucceedsThroughSymlinkedRepositoryAncestor(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("MONODEV_ROOT", "")

	base := t.TempDir()
	realParent := filepath.Join(base, "real-parent")
	repo := initGitRepo(t, filepath.Join(realParent, "repo"), "https://example.com/monodev.git")
	aliasParent := filepath.Join(base, "parent-alias")
	if err := os.Symlink(realParent, aliasParent); err != nil {
		t.Fatal(err)
	}
	aliasRepo := filepath.Join(aliasParent, filepath.Base(repo))
	chdir(t, aliasRepo)
	t.Setenv("PWD", aliasRepo)

	runCLI(t, "init")
	for _, name := range []string{"stores", "workspaces"} {
		if info, err := os.Stat(filepath.Join(repo, config.RepoLocalDirName, name)); err != nil || !info.IsDir() {
			t.Fatalf("repo-local %s missing through symlinked ancestor: info %v, error %v", name, info, err)
		}
	}
}

func TestManagedCommandsThroughSymlinkedAncestors(t *testing.T) {
	for _, sharedRoot := range []bool{false, true} {
		t.Run(map[bool]string{false: "repo-local", true: "shared-root"}[sharedRoot], func(t *testing.T) {
			base, err := filepath.EvalSymlinks(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			realParent := filepath.Join(base, "real")
			repo := initGitRepo(t, filepath.Join(realParent, "repo"), "https://example.com/monodev.git")
			home := filepath.Join(realParent, "home")
			if err := os.MkdirAll(home, 0700); err != nil {
				t.Fatal(err)
			}
			alias := filepath.Join(base, "alias")
			if err := os.Symlink(realParent, alias); err != nil {
				t.Fatal(err)
			}
			t.Setenv("HOME", filepath.Join(alias, "home"))
			t.Setenv("MONODEV_ROOT", "")
			if sharedRoot {
				t.Setenv("MONODEV_ROOT", filepath.Join(alias, "home", ".monodev"))
			}
			logicalRepo := filepath.Join(alias, "repo")
			chdir(t, logicalRepo)
			t.Setenv("PWD", logicalRepo)
			if cwd, err := os.Getwd(); err != nil || cwd != logicalRepo {
				t.Fatalf("test must retain logical cwd: %q, %v", cwd, err)
			}
			runCLI(t, "init")
			runCLI(t, "checkout", "-n", "qa")
			notes := filepath.Join(repo, "notes.txt")
			tree := filepath.Join(repo, "tree")
			if err := os.Mkdir(tree, 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(tree, "file.txt"), []byte("tree contents"), 0600); err != nil {
				t.Fatal(err)
			}
			write := func(content string) {
				t.Helper()
				if err := os.WriteFile(notes, []byte(content), 0600); err != nil {
					t.Fatal(err)
				}
			}
			write("committed")
			runCLI(t, "track", "notes.txt", "tree")
			runCLI(t, "commit", "--all")
			write("saved")
			runCLI(t, "save")
			write("workspace backup")
			runCLI(t, "apply", "--force")
			if got, err := os.ReadFile(notes); err != nil || string(got) != "saved" {
				t.Fatalf("applied file = %q, %v", got, err)
			}
			if got, err := os.ReadFile(filepath.Join(tree, "file.txt")); err != nil || string(got) != "tree contents" {
				t.Fatalf("applied directory leaf = %q, %v", got, err)
			}
			runCLI(t, "unapply")
			if _, err := os.Lstat(notes); !os.IsNotExist(err) {
				t.Fatalf("unapply did not remove notes: %v", err)
			}
			if _, err := os.Lstat(tree); !os.IsNotExist(err) {
				t.Fatalf("unapply did not remove directory: %v", err)
			}
			// Earlier versions wrote logical store paths into symlink-mode
			// overlays. Canonicalizing store locations must retain that ownership.
			eng, err := newEngine()
			if err != nil {
				t.Fatal(err)
			}
			if _, err := eng.Apply(context.Background(), &engine.ApplyRequest{CWD: logicalRepo, Mode: "symlink"}); err != nil {
				t.Fatal(err)
			}
			for _, path := range []string{notes, tree} {
				target, err := os.Readlink(path)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
				logicalTarget := strings.Replace(target, realParent, alias, 1)
				if err := os.Symlink(logicalTarget, path); err != nil {
					t.Fatal(err)
				}
			}
			runCLI(t, "commit", "--all")
			runCLI(t, "unapply")
			// The same source policy must still reject workspace-controlled
			// symlinks, naming the link's canonical location rather than alias.
			outside := filepath.Join(base, "private.txt")
			if err := os.WriteFile(outside, []byte("private"), 0600); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(outside, notes); err != nil {
				t.Fatal(err)
			}
			if err := runCLIErr(t, "commit", "--all"); err == nil || !strings.Contains(err.Error(), "refusing to copy symlink \""+notes+"\"") {
				t.Fatalf("symlink refusal = %v, want real path %q", err, notes)
			}
		})
	}
}

func TestInitReportsRepoLocalSymlinksClearly(t *testing.T) {
	for _, entry := range []string{config.RepoLocalDirName, "stores", "workspaces"} {
		t.Run(entry, func(t *testing.T) {
			t.Setenv("HOME", t.TempDir())
			t.Setenv("MONODEV_ROOT", "")
			repo := initGitRepo(t, filepath.Join(t.TempDir(), "repo"), "https://example.com/monodev.git")
			outside := filepath.Join(t.TempDir(), "outside")
			if err := os.Mkdir(outside, 0700); err != nil {
				t.Fatal(err)
			}
			root := filepath.Join(repo, config.RepoLocalDirName)
			linkPath := root
			if entry != config.RepoLocalDirName {
				if err := os.Mkdir(root, 0700); err != nil {
					t.Fatal(err)
				}
				linkPath = filepath.Join(root, entry)
			}
			if err := os.Symlink(outside, linkPath); err != nil {
				t.Fatal(err)
			}
			chdir(t, repo)

			err := runCLIErr(t, "init")
			if err == nil {
				t.Fatal("expected init to refuse a symlinked repo-local path")
			}
			got := strings.ToLower(err.Error())
			if !strings.Contains(got, "symlink") || !strings.Contains(got, strings.ToLower(entry)) {
				t.Errorf("error = %q, want the offending symlink path %q", err, entry)
			}
			if strings.Contains(got, "not a directory") {
				t.Errorf("error = %q, must not report a misleading not-a-directory failure", err)
			}
		})
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
