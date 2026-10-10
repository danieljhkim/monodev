//go:build integration

package integration

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCLICommitRejectsSymlinkedSourceAncestor(t *testing.T) {
	binary := buildMonodevBinary(t)
	base := t.TempDir()
	client := newCLIClient(t, binary, base, "workspace", filepath.Join(base, "remote.git"))
	client.mustRun("init")
	client.mustRun("checkout", "--new", "source-guard")
	source := filepath.Join(client.repoRoot, "nested")
	if err := os.Mkdir(source, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "private.txt"), []byte("original snapshot"), 0600); err != nil {
		t.Fatal(err)
	}
	client.mustRun("track", "nested/private.txt")
	client.mustRun("commit", "--all")
	outside := filepath.Join(base, "outside")
	if err := os.Mkdir(outside, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outside, "private.txt"), []byte("outside-secret-sentinel"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(source, source+"-original"); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, source); err != nil {
		t.Fatal(err)
	}
	out, err := client.run("commit", "--all")
	if err == nil || !strings.Contains(out, "symlink") {
		t.Fatalf("CLI commit accepted source ancestor: %v\n%s", err, out)
	}
	// Inspect every saved file rather than assuming the CLI's store layout.
	found := false
	for _, stateRoot := range []string{client.monodevRoot, filepath.Join(client.repoRoot, ".monodev")} {
		if err := filepath.WalkDir(stateRoot, func(path string, entry os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.IsDir() {
				return nil
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			if strings.Contains(string(data), "outside-secret-sentinel") {
				t.Errorf("outside content persisted at %s", path)
			}
			if entry.Name() == "private.txt" && string(data) == "original snapshot" {
				found = true
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	}
	if !found {
		t.Fatal("original snapshot was lost")
	}
}
