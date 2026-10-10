package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func decodeContextResult(t *testing.T, out string) contextResult {
	t.Helper()
	var result contextResult
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("context JSON: %v\n%s", err, out)
	}
	return result
}

func TestContext_NoRemoteIsEmptyResult(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("MONODEV_ROOT", "")
	configureTestGitIdentity(t)
	repo := initGitRepo(t, t.TempDir(), "https://example.com/monodev.git")
	runGit(t, repo, "commit", "--allow-empty", "-m", "initial")
	chdir(t, repo)

	out := runCLI(t, "context", "--json")
	// Every documented field is present, and lists are empty rather than null.
	var raw map[string]json.RawMessage
	if err := json.Unmarshal([]byte(out), &raw); err != nil {
		t.Fatalf("context JSON: %v\n%s", err, out)
	}
	for _, field := range []string{"workspaceId", "workspacePath", "source", "activeStore", "stores", "pulledStores", "notes", "sessions", "scripts", "warnings", "hint"} {
		value, ok := raw[field]
		if !ok {
			t.Errorf("context JSON missing %q:\n%s", field, out)
		}
		if string(value) == "null" {
			t.Errorf("context JSON field %q is null:\n%s", field, out)
		}
	}

	result := decodeContextResult(t, out)
	if result.Source != contextSourceNone || result.ActiveStore != "" || len(result.Stores) != 0 || len(result.PulledStores) != 0 {
		t.Fatalf("context result = %#v, want empty", result)
	}
	if !strings.Contains(result.Hint, "monodev remote use origin") {
		t.Fatalf("context hint = %q, want a remote hint", result.Hint)
	}
}

func TestContext_ReportsActiveStoreAndAgentPaths(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("MONODEV_ROOT", "")
	configureTestGitIdentity(t)
	repo := initGitRepo(t, t.TempDir(), "https://example.com/monodev.git")
	runGit(t, repo, "commit", "--allow-empty", "-m", "initial")
	for path, body := range map[string]string{
		".agents/notes/lessons.md":                      "lesson\n",
		".agents/notes/sessions/2026-10-09-claude-1.md": "session\n",
		".agents/scripts/tools/check.sh":                "echo ok\n",
	} {
		abs := filepath.Join(repo, filepath.FromSlash(path))
		if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(abs, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	chdir(t, repo)
	runCLI(t, "checkout", "-n", "agent-context")
	runCLI(t, "track", ".agents/notes", ".agents/scripts")
	runCLI(t, "save")

	result := decodeContextResult(t, runCLI(t, "context", "--json"))
	if result.Source != contextSourceActiveStore || result.ActiveStore != "agent-context" || !reflect.DeepEqual(result.Stores, []string{"agent-context"}) {
		t.Fatalf("context result = %#v, want the active store", result)
	}
	if !reflect.DeepEqual(result.Notes, []string{".agents/notes/lessons.md"}) {
		t.Errorf("notes = %v", result.Notes)
	}
	if !reflect.DeepEqual(result.Sessions, []string{".agents/notes/sessions/2026-10-09-claude-1.md"}) {
		t.Errorf("sessions = %v", result.Sessions)
	}
	if !reflect.DeepEqual(result.Scripts, []string{".agents/scripts/tools/check.sh"}) {
		t.Errorf("scripts = %v", result.Scripts)
	}
	if len(result.PulledStores) != 0 {
		t.Errorf("pulledStores = %v, want none for an active store", result.PulledStores)
	}
}

func TestContext_UnreachableRemoteIsEmptyResult(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("MONODEV_ROOT", "")
	configureTestGitIdentity(t)
	repo := initGitRepo(t, t.TempDir(), filepath.Join(t.TempDir(), "missing.git"))
	runGit(t, repo, "commit", "--allow-empty", "-m", "initial")
	chdir(t, repo)
	runCLI(t, "remote", "use", "origin")

	result := decodeContextResult(t, runCLI(t, "context", "--json"))
	if result.Source != contextSourceNone || len(result.Stores) != 0 {
		t.Fatalf("context result = %#v, want empty", result)
	}
	if len(result.Warnings) == 0 || !strings.Contains(result.Hint, "Could not fetch") {
		t.Fatalf("context result = %#v, want a fetch warning and hint", result)
	}
}
