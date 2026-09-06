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

func TestSaveCommand_UserIgnoredNestedDescendantIsNotPersisted(t *testing.T) {
	repo := setupSaveTestRepo(t)
	if err := os.WriteFile(filepath.Join(repo, ".gitignore"), []byte("*.log\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(repo, "tools", "nested"), 0755); err != nil {
		t.Fatal(err)
	}
	ordinary := filepath.Join("tools", "nested", "new.sh")
	ignored := filepath.Join("tools", "nested", "ignored.log")
	if err := os.WriteFile(filepath.Join(repo, ordinary), []byte("echo new\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, ignored), []byte("ignored\n"), 0644); err != nil {
		t.Fatal(err)
	}

	dryRun := runCLI(t, "save", "--dry-run")
	if !strings.Contains(dryRun, ordinary) || strings.Contains(dryRun, ignored) {
		t.Fatalf("dry-run output = %q, want only ordinary new descendant", dryRun)
	}
	overlayIgnored := filepath.Join(repo, ".monodev", "stores", "dev-store", "overlay", ignored)
	if _, err := os.Stat(overlayIgnored); !os.IsNotExist(err) {
		t.Fatalf("dry-run persisted ignored descendant, stat error = %v", err)
	}

	out := runCLI(t, "save")
	if !strings.Contains(out, ordinary) || strings.Contains(out, ignored) {
		t.Fatalf("save output = %q, want only ordinary new descendant", out)
	}
	paths := readTrackedPaths(t, filepath.Join(repo, ".monodev", "stores", "dev-store", "track.json"))
	if !containsString(paths, ordinary) || containsString(paths, ignored) {
		t.Fatalf("tracked paths = %v, want %s but not %s", paths, ordinary, ignored)
	}
	if _, err := os.Stat(overlayIgnored); !os.IsNotExist(err) {
		t.Fatalf("save persisted ignored descendant, stat error = %v", err)
	}
	if _, err := os.Stat(filepath.Join(repo, ".monodev", "stores", "dev-store", "overlay", ordinary)); err != nil {
		t.Fatalf("save did not persist ordinary descendant: %v", err)
	}
}

func TestSaveCommand_ManagedExcludeDoesNotHideNewDescendant(t *testing.T) {
	repo := setupSaveTestRepo(t)
	runCLI(t, "apply", "--force")
	newFile := filepath.Join("tools", "new.sh")
	if err := os.WriteFile(filepath.Join(repo, newFile), []byte("echo new\n"), 0644); err != nil {
		t.Fatal(err)
	}

	out := runCLI(t, "save", "--dry-run")
	if !strings.Contains(out, newFile) {
		t.Fatalf("managed exclude hid new descendant; save output = %q", out)
	}
}

func TestSaveCommand_PersistsPreviouslyTrackedContentAfterItBecomesIgnored(t *testing.T) {
	repo := setupSaveTestRepo(t)
	if err := os.WriteFile(filepath.Join(repo, ".gitignore"), []byte("*.sh\n"), 0644); err != nil {
		t.Fatal(err)
	}
	tracked := filepath.Join("tools", "existing.sh")
	if err := os.WriteFile(filepath.Join(repo, tracked), []byte("echo changed\n"), 0644); err != nil {
		t.Fatal(err)
	}

	runCLI(t, "save")
	overlay := filepath.Join(repo, ".monodev", "stores", "dev-store", "overlay", tracked)
	content, err := os.ReadFile(overlay)
	if err != nil {
		t.Fatalf("read persisted tracked content: %v", err)
	}
	if string(content) != "echo changed\n" {
		t.Fatalf("persisted tracked content = %q, want changed content", content)
	}
}
