package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func setupSaveTestRepo(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("MONODEV_ROOT", "")

	repo := initGitRepo(t, t.TempDir(), "https://example.com/monodev.git")
	chdir(t, repo)
	runCLI(t, "init")

	if err := os.MkdirAll(filepath.Join(repo, "tools"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "tools", "existing.sh"), []byte("echo hi\n"), 0644); err != nil {
		t.Fatal(err)
	}

	runCLI(t, "checkout", "--new", "dev-store")
	runCLI(t, "track", "tools")
	runCLI(t, "commit", "--all")

	return repo
}

func TestSaveCommand_TracksNewFileAndCommits(t *testing.T) {
	repo := setupSaveTestRepo(t)

	// Simulate a working session: a new file appears under the tracked directory.
	newFile := filepath.Join("tools", "new.sh")
	if err := os.WriteFile(filepath.Join(repo, newFile), []byte("echo new\n"), 0644); err != nil {
		t.Fatal(err)
	}

	out := runCLI(t, "save")
	if !strings.Contains(out, "Newly tracked") {
		t.Fatalf("output = %q, want newly tracked section", out)
	}
	if !strings.Contains(out, newFile) {
		t.Fatalf("output = %q, want new file path reported", out)
	}

	paths := readTrackedPaths(t, filepath.Join(repo, ".monodev", "stores", "dev-store", "track.json"))
	if !containsString(paths, newFile) {
		t.Fatalf("tracked paths = %v, want %s tracked", paths, newFile)
	}

	overlayPath := filepath.Join(repo, ".monodev", "stores", "dev-store", "overlay", "tools", "new.sh")
	if _, err := os.Stat(overlayPath); err != nil {
		t.Fatalf("expected new file content persisted to overlay: %v", err)
	}

	diffOut := runCLI(t, "diff")
	if !strings.Contains(diffOut, "No changes detected") {
		t.Fatalf("diff output = %q, want clean after save", diffOut)
	}
}

func TestSaveCommand_DryRunDoesNotMutate(t *testing.T) {
	repo := setupSaveTestRepo(t)

	newFile := filepath.Join("tools", "new.sh")
	if err := os.WriteFile(filepath.Join(repo, newFile), []byte("echo new\n"), 0644); err != nil {
		t.Fatal(err)
	}

	out := runCLI(t, "save", "--dry-run")
	if !strings.Contains(out, "Would newly track") {
		t.Fatalf("output = %q, want dry-run newly-track wording", out)
	}
	if !strings.Contains(out, newFile) {
		t.Fatalf("output = %q, want new file path reported", out)
	}

	paths := readTrackedPaths(t, filepath.Join(repo, ".monodev", "stores", "dev-store", "track.json"))
	if containsString(paths, newFile) {
		t.Fatalf("tracked paths = %v, dry-run must not mutate track.json", paths)
	}

	overlayPath := filepath.Join(repo, ".monodev", "stores", "dev-store", "overlay", "tools", "new.sh")
	if _, err := os.Stat(overlayPath); !os.IsNotExist(err) {
		t.Fatalf("dry-run must not persist content, but %s exists (err=%v)", overlayPath, err)
	}
}
