package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEjectCommandKeepFilesDryRunConfirmationAndGitVisibility(t *testing.T) {
	repo, _, overlaidPath := setupEjectCommandWorkspace(t)

	dryRunOutput, err := runEjectCLI(t, "", "eject", "--dry-run")
	if err != nil {
		t.Fatalf("eject --dry-run: %v", err)
	}
	if !strings.Contains(dryRunOutput, "Eject plan (keep files)") || !strings.Contains(dryRunOutput, "leave these paths on disk unchanged") {
		t.Fatalf("dry-run output = %q, want keep-files plan", dryRunOutput)
	}
	if _, err := os.Stat(overlaidPath); err != nil {
		t.Fatalf("dry-run removed overlaid file: %v", err)
	}
	if status := gitPorcelain(t, repo); status != "" {
		t.Fatalf("dry-run changed git visibility: %q", status)
	}

	cancelOutput, err := runEjectCLI(t, "\n", "eject")
	if err == nil || !strings.Contains(err.Error(), "cancelled") {
		t.Fatalf("eject without confirmation error = %v, want cancellation", err)
	}
	if !strings.Contains(cancelOutput, "Proceed with eject?") {
		t.Fatalf("confirmation output = %q, want prompt", cancelOutput)
	}
	if _, err := os.Stat(overlaidPath); err != nil {
		t.Fatalf("cancelled eject removed overlaid file: %v", err)
	}

	want := []byte("user changed this after apply\n")
	if err := os.WriteFile(overlaidPath, want, 0600); err != nil {
		t.Fatalf("modify overlaid file: %v", err)
	}
	resultOutput, err := runEjectCLI(t, "yes\n", "eject")
	if err != nil {
		t.Fatalf("confirmed eject: %v", err)
	}
	if !strings.Contains(resultOutput, "kept 1 file") {
		t.Fatalf("eject output = %q, want retained-file summary", resultOutput)
	}
	got, err := os.ReadFile(overlaidPath)
	if err != nil {
		t.Fatalf("read retained file: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("retained content = %q, want %q", got, want)
	}

	exclude, err := os.ReadFile(filepath.Join(repo, ".git", "info", "exclude"))
	if err != nil {
		t.Fatalf("read exclude: %v", err)
	}
	if strings.Contains(string(exclude), "monodev managed block") {
		t.Fatalf("managed exclude block remained after eject:\n%s", exclude)
	}
	if status := gitPorcelain(t, repo); !strings.Contains(status, "?? workspace/debug.sh") {
		t.Fatalf("retained overlaid file is not untracked after eject: %q", status)
	}
	if _, err := os.Stat(filepath.Join(repo, ".monodev", "stores", "exit-store")); err != nil {
		t.Fatalf("eject removed store: %v", err)
	}
}

func TestEjectCommandRemoveFiles(t *testing.T) {
	_, _, overlaidPath := setupEjectCommandWorkspace(t)

	dryRunOutput, err := runEjectCLI(t, "", "eject", "--remove-files", "--dry-run")
	if err != nil {
		t.Fatalf("remove-files dry-run: %v", err)
	}
	if !strings.Contains(dryRunOutput, "Eject plan (remove files)") {
		t.Fatalf("remove-files dry-run output = %q", dryRunOutput)
	}
	if _, err := os.Stat(overlaidPath); err != nil {
		t.Fatalf("remove-files dry-run removed path: %v", err)
	}

	_, err = runEjectCLI(t, "", "eject", "--remove-files", "--yes")
	if err != nil {
		t.Fatalf("remove-files eject: %v", err)
	}
	if _, err := os.Lstat(overlaidPath); !os.IsNotExist(err) {
		t.Fatalf("remove-files eject left overlaid path, err=%v", err)
	}
}

func setupEjectCommandWorkspace(t *testing.T) (repo, workspace, overlaidPath string) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("MONODEV_ROOT", "")
	repo = initGitRepo(t, t.TempDir(), "https://example.com/monodev.git")
	chdir(t, repo)
	runCLI(t, "init")
	runCLI(t, "checkout", "--new", "exit-store")
	if err := os.WriteFile(filepath.Join(repo, "debug.sh"), []byte("initial overlay\n"), 0600); err != nil {
		t.Fatalf("create tracked file: %v", err)
	}
	runCLI(t, "track", "debug.sh")
	runCLI(t, "commit", "--all")
	if err := os.Remove(filepath.Join(repo, "debug.sh")); err != nil {
		t.Fatalf("remove source file before applying elsewhere: %v", err)
	}

	workspace = filepath.Join(repo, "workspace")
	if err := os.Mkdir(workspace, 0755); err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	chdir(t, workspace)
	runCLI(t, "checkout", "exit-store")
	runCLI(t, "apply")
	return repo, workspace, filepath.Join(workspace, "debug.sh")
}

func runEjectCLI(t *testing.T, input string, args ...string) (string, error) {
	t.Helper()
	resetCommandFlags(rootCmd)
	rootCmd.SetArgs(append([]string{}, args...))
	var output bytes.Buffer
	rootCmd.SetIn(strings.NewReader(input))
	rootCmd.SetOut(&output)
	rootCmd.SetErr(&output)
	t.Cleanup(func() {
		rootCmd.SetIn(nil)
		rootCmd.SetOut(nil)
		rootCmd.SetErr(nil)
	})
	err := rootCmd.Execute()
	return output.String(), err
}
