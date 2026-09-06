//go:build integration
// +build integration

package integration

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/danieljhkim/monodev/internal/state"
)

// TestCLI_CommitDirectoryPreservesOwnershipManifest exercises the built binary
// because the regression is a ledger transition between commit and unapply,
// rather than only the manifest helper in isolation.
func TestCLI_CommitDirectoryPreservesOwnershipManifest(t *testing.T) {
	binary := buildMonodevBinary(t)
	baseDir := t.TempDir()

	setup := func(t *testing.T, name string) *cliClient {
		t.Helper()
		client := newCLIClient(t, binary, baseDir, name, "https://example.test/commit-unapply.git")
		client.mustRun("init")
		for path, contents := range map[string]string{
			"config/app.yml":        "version: one\n",
			"config/nested/job.yml": "enabled: true\n",
		} {
			fullPath := filepath.Join(client.repoRoot, path)
			if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(fullPath, []byte(contents), 0644); err != nil {
				t.Fatal(err)
			}
		}
		client.mustRun("checkout", "--new", name+"-store")
		client.mustRun("track", "config")
		client.mustRun("commit", "--all")
		return client
	}

	t.Run("ordinary unapply accepts the committed directory", func(t *testing.T) {
		client := setup(t, "clean")
		workspaceID := cliStatusWorkspaceID(t, client.mustRun("status", "--json"))
		contents, err := os.ReadFile(client.stateFilePath(workspaceID))
		if err != nil {
			t.Fatalf("read workspace ledger: %v", err)
		}
		var ledger state.WorkspaceState
		if err := json.Unmarshal(contents, &ledger); err != nil {
			t.Fatalf("decode workspace ledger: %v", err)
		}
		ownership, ok := ledger.Paths["config"]
		if !ok || ownership.Contents == nil {
			t.Fatalf("config ownership = %#v, want a directory manifest", ownership)
		}
		if len(ownership.Contents.Files) != 2 {
			t.Fatalf("manifest files = %#v, want both directory leaves", ownership.Contents.Files)
		}

		client.mustRun("unapply")
		if _, err := os.Stat(filepath.Join(client.repoRoot, "config")); !os.IsNotExist(err) {
			t.Fatalf("config remains after unapply, err=%v", err)
		}
	})

	t.Run("recommit refreshes the applied directory manifest", func(t *testing.T) {
		client := setup(t, "recommit")
		client.mustRun("apply", "--force")
		if err := os.WriteFile(filepath.Join(client.repoRoot, "config", "app.yml"), []byte("version: two\n"), 0644); err != nil {
			t.Fatal(err)
		}
		client.mustRun("commit", "--all")
		client.mustRun("unapply")
		if _, err := os.Stat(filepath.Join(client.repoRoot, "config")); !os.IsNotExist(err) {
			t.Fatalf("recommitted config remains after unapply, err=%v", err)
		}
	})

	t.Run("post-commit directory changes remain protected", func(t *testing.T) {
		client := setup(t, "drift")
		if err := os.WriteFile(filepath.Join(client.repoRoot, "config", "added.yml"), []byte("local: true\n"), 0644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(client.repoRoot, "config", "app.yml"), []byte("version: changed\n"), 0644); err != nil {
			t.Fatal(err)
		}
		if err := os.Remove(filepath.Join(client.repoRoot, "config", "nested", "job.yml")); err != nil {
			t.Fatal(err)
		}

		out, err := client.run("unapply")
		if err == nil {
			t.Fatalf("unapply succeeded despite post-commit changes:\n%s", out)
		}
		for _, path := range []string{"config/added.yml", "config/app.yml", "config/nested/job.yml"} {
			if !strings.Contains(out, path) {
				t.Fatalf("unapply output = %q, want exact changed path %q", out, path)
			}
		}
		if _, err := os.Stat(filepath.Join(client.repoRoot, "config", "app.yml")); err != nil {
			t.Fatalf("drifted config was removed, err=%v", err)
		}
	})
}
