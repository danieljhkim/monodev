package cli

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/fatih/color"

	"github.com/danieljhkim/monodev/internal/engine"
)

func TestFormatDefaultDiff_PrintsPatchAndSummary(t *testing.T) {
	result := &engine.DiffResult{
		Files: []engine.DiffFileInfo{
			{
				Path:        "b.txt",
				Status:      "modified",
				UnifiedDiff: "diff --git a/b.txt b/b.txt\n--- a/b.txt\n+++ b/b.txt\n@@ -1 +1 @@\n-old\n+new\n",
				Additions:   1,
				Deletions:   1,
			},
			{
				Path:        "a.txt",
				Status:      "added",
				UnifiedDiff: "diff --git a/a.txt b/a.txt\n--- /dev/null\n+++ b/a.txt\n@@ -0,0 +1 @@\n+line\n",
				Additions:   1,
				Deletions:   0,
			},
			{
				Path:   "z.txt",
				Status: "unchanged",
			},
		},
	}

	output := captureStdout(t, func() {
		if err := formatDefaultDiff(result); err != nil {
			t.Fatalf("formatDefaultDiff failed: %v", err)
		}
	})

	if strings.Index(output, "a.txt") > strings.Index(output, "b.txt") {
		t.Fatalf("expected sorted output by path, got:\n%s", output)
	}
	if !strings.Contains(output, "2 files changed, 2 insertions(+), 1 deletion(-)") {
		t.Fatalf("expected git-like summary line, got:\n%s", output)
	}
}

func TestFormatDefaultDiff_NoChanges(t *testing.T) {
	result := &engine.DiffResult{
		Files: []engine.DiffFileInfo{{Path: "a.txt", Status: "unchanged"}},
	}

	output := captureStdout(t, func() {
		if err := formatDefaultDiff(result); err != nil {
			t.Fatalf("formatDefaultDiff failed: %v", err)
		}
	})

	if !strings.Contains(output, "No changes detected") {
		t.Fatalf("expected empty-state message, got:\n%s", output)
	}
}

func captureStdout(t *testing.T, fn func()) string {
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
