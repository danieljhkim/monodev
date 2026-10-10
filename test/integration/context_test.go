//go:build integration
// +build integration

package integration

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

type cliContextResult struct {
	WorkspaceID   string   `json:"workspaceId"`
	WorkspacePath string   `json:"workspacePath"`
	Source        string   `json:"source"`
	ActiveStore   string   `json:"activeStore"`
	Stores        []string `json:"stores"`
	PulledStores  []string `json:"pulledStores"`
	Notes         []string `json:"notes"`
	Sessions      []string `json:"sessions"`
	Scripts       []string `json:"scripts"`
	Warnings      []string `json:"warnings"`
	Hint          string   `json:"hint"`
}

func decodeCLIContext(t *testing.T, out string) cliContextResult {
	t.Helper()
	var result cliContextResult
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("context JSON: %v\n%s", err, out)
	}
	return result
}

func persistBranchHead(t *testing.T, bareRemote string) string {
	t.Helper()
	out, err := exec.Command("git", "-C", bareRemote, "rev-parse", "refs/heads/monodev/persist").CombinedOutput()
	if err != nil {
		t.Fatalf("rev-parse persistence branch: %v\n%s", err, out)
	}
	return strings.TrimSpace(string(out))
}

// agentStatusLines returns git status entries for agent files. The
// persistence work tree under .monodev is a nested repository, which git
// lists on its own; it is not agent context.
func agentStatusLines(t *testing.T, repoRoot string) []string {
	t.Helper()
	out, err := exec.Command("git", "-C", repoRoot, "status", "--porcelain", "--untracked-files=all").CombinedOutput()
	if err != nil {
		t.Fatalf("git status: %v\n%s", err, out)
	}
	var lines []string
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line != "" && !strings.Contains(line, ".monodev/") {
			lines = append(lines, line)
		}
	}
	return lines
}

// TestCLI_ContextRestoresRemoteOnlyWorkspaceReference drives the agent
// session-start path: the workspace reference exists only on the remote
// persistence branch, and `monodev context` pulls and applies its stores,
// then lists the agent notes and scripts, without pushing anything.
func TestCLI_ContextRestoresRemoteOnlyWorkspaceReference(t *testing.T) {
	binary := buildMonodevBinary(t)
	baseDir := t.TempDir()
	bareRemote := filepath.Join(baseDir, "remote.git")
	if err := os.MkdirAll(bareRemote, 0755); err != nil {
		t.Fatal(err)
	}
	runGit(t, bareRemote, "init", "--bare")

	producer := newCLIClient(t, binary, baseDir, "producer", bareRemote)
	producer.mustRun("init")
	producer.mustRun("remote", "use", "origin")
	files := map[string]string{
		".agents/notes/lessons.md":                     "- run make lint before pushing\n",
		".agents/notes/sessions/2026-10-09-codex-a.md": "handoff: finish the parser\n",
		".agents/scripts/check.sh":                     "#!/bin/sh\nmake test\n",
	}
	for rel, body := range files {
		path := filepath.Join(producer.repoRoot, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0644); err != nil {
			t.Fatal(err)
		}
	}
	producer.mustRun("checkout", "-n", "agent-notes")
	producer.mustRun("track", ".agents/notes", ".agents/scripts")
	producer.mustRun("save")
	producer.mustRun("push", "agent-notes", "--with-workspace")
	pushedHead := persistBranchHead(t, bareRemote)

	consumer := newCLIClient(t, binary, baseDir, "consumer", bareRemote)
	consumer.mustRun("init")

	// No remote configured yet: an empty result, exit 0, and a hint.
	empty := decodeCLIContext(t, consumer.mustRun("context", "--json"))
	if empty.Source != "none" || len(empty.Stores) != 0 || len(empty.Notes) != 0 {
		t.Fatalf("context without a remote = %#v, want an empty result", empty)
	}
	if !strings.Contains(empty.Hint, "monodev remote use origin") {
		t.Fatalf("context hint = %q, want a remote hint", empty.Hint)
	}

	consumer.mustRun("remote", "use", "origin")
	restored := decodeCLIContext(t, consumer.mustRun("context", "--json"))
	if restored.Source != "remote-reference" || restored.ActiveStore != "agent-notes" {
		t.Fatalf("context = %#v, want agent-notes restored from the remote", restored)
	}
	if !reflect.DeepEqual(restored.PulledStores, []string{"agent-notes"}) || !reflect.DeepEqual(restored.Stores, []string{"agent-notes"}) {
		t.Fatalf("context stores = %#v", restored)
	}
	if !reflect.DeepEqual(restored.Notes, []string{".agents/notes/lessons.md"}) {
		t.Errorf("notes = %v", restored.Notes)
	}
	if !reflect.DeepEqual(restored.Sessions, []string{".agents/notes/sessions/2026-10-09-codex-a.md"}) {
		t.Errorf("sessions = %v", restored.Sessions)
	}
	if !reflect.DeepEqual(restored.Scripts, []string{".agents/scripts/check.sh"}) {
		t.Errorf("scripts = %v", restored.Scripts)
	}
	for rel, body := range files {
		got, err := os.ReadFile(filepath.Join(consumer.repoRoot, filepath.FromSlash(rel)))
		if err != nil || string(got) != body {
			t.Errorf("restored %s = %q, %v; want %q", rel, got, err, body)
		}
	}
	if lines := agentStatusLines(t, consumer.repoRoot); len(lines) != 0 {
		t.Errorf("applied agent files are visible to git: %v", lines)
	}

	// A second call finds the now-active store and stays local.
	again := decodeCLIContext(t, consumer.mustRun("context", "--json"))
	if again.Source != "active-store" || again.ActiveStore != "agent-notes" || len(again.PulledStores) != 0 {
		t.Fatalf("second context = %#v, want the active store reported", again)
	}

	if head := persistBranchHead(t, bareRemote); head != pushedHead {
		t.Fatalf("context changed the remote persistence branch: %s -> %s", pushedHead, head)
	}
}

// TestCLI_ContextRefusesToOverwriteDivergedLocalStore checks that context
// goes through pull's refusal instead of forcing: a local copy of the store
// that differs from the remote is left alone and the command fails.
func TestCLI_ContextRefusesToOverwriteDivergedLocalStore(t *testing.T) {
	binary := buildMonodevBinary(t)
	baseDir := t.TempDir()
	bareRemote := filepath.Join(baseDir, "remote.git")
	if err := os.MkdirAll(bareRemote, 0755); err != nil {
		t.Fatal(err)
	}
	runGit(t, bareRemote, "init", "--bare")

	producer := newCLIClient(t, binary, baseDir, "producer", bareRemote)
	producer.mustRun("init")
	producer.mustRun("remote", "use", "origin")
	if err := os.WriteFile(filepath.Join(producer.repoRoot, "notes.md"), []byte("remote\n"), 0644); err != nil {
		t.Fatal(err)
	}
	producer.mustRun("checkout", "-n", "agent-notes")
	producer.mustRun("track", "notes.md")
	producer.mustRun("save")
	producer.mustRun("push", "agent-notes", "--with-workspace")

	// The consumer already has its own agent-notes store with other content,
	// created in a different directory so this workspace has no active store.
	consumer := newCLIClient(t, binary, baseDir, "consumer", bareRemote)
	consumer.mustRun("init")
	consumer.mustRun("remote", "use", "origin")
	elsewhere := filepath.Join(consumer.repoRoot, "elsewhere")
	if err := os.MkdirAll(elsewhere, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(elsewhere, "notes.md"), []byte("local\n"), 0644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"checkout", "-n", "agent-notes"}, {"track", "notes.md"}, {"save"}} {
		cmd := exec.Command(binary, args...)
		cmd.Dir = elsewhere
		cmd.Env = consumer.env
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("monodev %v: %v\n%s", args, err, out)
		}
	}

	out, err := consumer.run("context", "--json")
	if err == nil {
		t.Fatalf("context overwrote a diverged local store:\n%s", out)
	}
	if !strings.Contains(out, "differs from local copy") {
		t.Fatalf("context error = %q, want the pull refusal", out)
	}
	if _, statErr := os.Stat(filepath.Join(consumer.repoRoot, "notes.md")); !os.IsNotExist(statErr) {
		t.Fatalf("refused context wrote notes.md: %v", statErr)
	}
}
