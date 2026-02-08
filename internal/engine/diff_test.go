package engine

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/danieljhkim/monodev/internal/fsops"
	"github.com/danieljhkim/monodev/internal/hash"
)

func TestGenerateUnifiedDiff_ModifiedFile(t *testing.T) {
	diff, additions, deletions := generateUnifiedDiff(
		"test.txt",
		[]byte("line1\nline2\nline3\n"),
		[]byte("line1\nline-two\nline3\n"),
		"modified",
	)

	if additions != 1 {
		t.Fatalf("additions = %d, want 1", additions)
	}
	if deletions != 1 {
		t.Fatalf("deletions = %d, want 1", deletions)
	}

	checks := []string{
		"diff --git a/test.txt b/test.txt",
		"--- a/test.txt",
		"+++ b/test.txt",
		"@@",
		"-line2",
		"+line-two",
	}
	for _, want := range checks {
		if !strings.Contains(diff, want) {
			t.Fatalf("diff missing %q:\n%s", want, diff)
		}
	}
}

func TestGenerateUnifiedDiff_AddedFile(t *testing.T) {
	diff, additions, deletions := generateUnifiedDiff(
		"new.txt",
		nil,
		[]byte("first\nsecond\n"),
		"added",
	)

	if additions != 2 {
		t.Fatalf("additions = %d, want 2", additions)
	}
	if deletions != 0 {
		t.Fatalf("deletions = %d, want 0", deletions)
	}

	checks := []string{
		"--- /dev/null",
		"+++ b/new.txt",
		"+first",
		"+second",
	}
	for _, want := range checks {
		if !strings.Contains(diff, want) {
			t.Fatalf("diff missing %q:\n%s", want, diff)
		}
	}
}

func TestComparePath_ShowContentPopulatesUnifiedDiff(t *testing.T) {
	tmpDir := t.TempDir()
	storePath := filepath.Join(tmpDir, "store.txt")
	workspacePath := filepath.Join(tmpDir, "workspace.txt")

	if err := os.WriteFile(storePath, []byte("alpha\nbeta\n"), 0644); err != nil {
		t.Fatalf("failed to write store file: %v", err)
	}
	if err := os.WriteFile(workspacePath, []byte("alpha\ngamma\n"), 0644); err != nil {
		t.Fatalf("failed to write workspace file: %v", err)
	}

	eng := &Engine{
		fs:     fsops.NewRealFS(),
		hasher: hash.NewSHA256Hasher(),
	}

	info := eng.comparePath(workspacePath, storePath, "example.txt", "file", true)

	if info.Status != "modified" {
		t.Fatalf("status = %q, want modified", info.Status)
	}
	if info.UnifiedDiff == "" {
		t.Fatal("expected UnifiedDiff to be populated")
	}
	if info.Additions != 1 || info.Deletions != 1 {
		t.Fatalf("line stats = +%d/-%d, want +1/-1", info.Additions, info.Deletions)
	}
	if !strings.Contains(info.UnifiedDiff, "-beta") || !strings.Contains(info.UnifiedDiff, "+gamma") {
		t.Fatalf("unexpected UnifiedDiff:\n%s", info.UnifiedDiff)
	}
}
