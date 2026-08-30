package cli

import (
	"bytes"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/spf13/cobra"
)

// setupTestEnv creates a temporary directory structure for testing
func setupTestEnv(t *testing.T) (string, func()) {
	tmpDir, err := os.MkdirTemp("", "monodev-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}

	// Create a mock git repo structure
	gitDir := filepath.Join(tmpDir, "repo")
	if err := os.MkdirAll(gitDir, 0755); err != nil {
		t.Fatalf("Failed to create repo dir: %v", err)
	}

	// Create .git directory to simulate git repo
	gitDotDir := filepath.Join(gitDir, ".git")
	if err := os.MkdirAll(gitDotDir, 0755); err != nil {
		t.Fatalf("Failed to create .git dir: %v", err)
	}

	// Create workspace directory
	workspaceDir := filepath.Join(gitDir, "workspace")
	if err := os.MkdirAll(workspaceDir, 0755); err != nil {
		t.Fatalf("Failed to create workspace dir: %v", err)
	}

	// Set HOME to tmpDir so config uses test directory
	oldHome := os.Getenv("HOME")
	_ = os.Setenv("HOME", tmpDir)

	cleanup := func() {
		_ = os.Setenv("HOME", oldHome)
		_ = os.RemoveAll(tmpDir)
	}

	return workspaceDir, cleanup
}

func TestStoreLsCommand_NoStores(t *testing.T) {
	workspaceDir, cleanup := setupTestEnv(t)
	defer cleanup()

	// Change to workspace directory
	oldDir, _ := os.Getwd()
	_ = os.Chdir(workspaceDir)
	defer func() {
		_ = os.Chdir(oldDir)
	}()

	rootCmd.SetArgs([]string{"store", "ls"})
	var bufOut, bufErr bytes.Buffer
	rootCmd.SetOut(&bufOut)
	rootCmd.SetErr(&bufErr)

	err := rootCmd.Execute()
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	output := bufOut.String() + bufErr.String()
	if output != "" {
		// Should be valid JSON
		var v interface{}
		if err := json.Unmarshal([]byte(output), &v); err != nil {
			t.Errorf("expected valid JSON output, got error: %v, output: %q", err, output)
		}
	}
}

func TestStoreLsCommand_JSONOutput(t *testing.T) {
	workspaceDir, cleanup := setupTestEnv(t)
	defer cleanup()

	oldDir, _ := os.Getwd()
	_ = os.Chdir(workspaceDir)
	defer func() {
		_ = os.Chdir(oldDir)
	}()

	rootCmd.SetArgs([]string{"store", "ls", "--json"})
	var buf bytes.Buffer
	rootCmd.SetOut(&buf)

	err := rootCmd.Execute()
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	output := buf.String()
	// Trim whitespace and newlines
	output = string(bytes.TrimSpace([]byte(output)))
	if output == "" {
		t.Skip("No output to validate (empty stores list)")
		return
	}
	// Should be valid JSON
	var v interface{}
	if err := json.Unmarshal([]byte(output), &v); err != nil {
		t.Errorf("expected valid JSON output, got error: %v, output: %q", err, output)
	}
}

func TestStatusCommand_NoWorkspaceState(t *testing.T) {
	workspaceDir, cleanup := setupTestEnv(t)
	defer cleanup()

	oldDir, _ := os.Getwd()
	_ = os.Chdir(workspaceDir)
	defer func() {
		_ = os.Chdir(oldDir)
	}()

	rootCmd.SetArgs([]string{"status"})
	var buf bytes.Buffer
	rootCmd.SetOut(&buf)

	err := rootCmd.Execute()
	if err != nil {
		t.Fatalf("Status command error = %v", err)
	}
}

func TestStatusCommand_JSONOutput(t *testing.T) {
	workspaceDir, cleanup := setupTestEnv(t)
	defer cleanup()

	oldDir, _ := os.Getwd()
	_ = os.Chdir(workspaceDir)
	defer func() {
		_ = os.Chdir(oldDir)
	}()

	rootCmd.SetArgs([]string{"status", "--json"})
	var buf bytes.Buffer
	rootCmd.SetOut(&buf)

	err := rootCmd.Execute()
	if err != nil {
		t.Fatalf("Status command error = %v", err)
	}

	output := buf.String()
	if output != "" {
		// If there's output, it should be valid JSON
		var v interface{}
		if err := json.Unmarshal([]byte(output), &v); err != nil {
			t.Errorf("expected valid JSON output, got error: %v", err)
		}
	}
}

func TestCheckoutCommand_InvalidArgs(t *testing.T) {
	workspaceDir, cleanup := setupTestEnv(t)
	defer cleanup()

	oldDir, _ := os.Getwd()
	_ = os.Chdir(workspaceDir)
	defer func() {
		_ = os.Chdir(oldDir)
	}()

	// Test with no args (should fail)
	rootCmd.SetArgs([]string{"checkout"})
	var buf bytes.Buffer
	rootCmd.SetErr(&buf)

	err := rootCmd.Execute()
	if err == nil {
		t.Error("expected error for checkout command with no args")
	}
}

func TestTrackCommand_InvalidArgs(t *testing.T) {
	workspaceDir, cleanup := setupTestEnv(t)
	defer cleanup()

	oldDir, _ := os.Getwd()
	_ = os.Chdir(workspaceDir)
	defer func() {
		_ = os.Chdir(oldDir)
	}()

	// Test with no args (should fail)
	rootCmd.SetArgs([]string{"track"})
	var buf bytes.Buffer
	rootCmd.SetErr(&buf)

	err := rootCmd.Execute()
	if err == nil {
		t.Error("expected error for track command with no args")
	}
}

func TestResolveAgentPresetPaths_UsesProvidedFilesystem(t *testing.T) {
	workspace := fstest.MapFS{
		".claude":         &fstest.MapFile{Mode: fs.ModeDir},
		"CLAUDE.md":       &fstest.MapFile{Data: []byte("instructions")},
		".aider.conf.yml": &fstest.MapFile{Data: []byte("model: test")},
		"unrelated.txt":   &fstest.MapFile{Data: []byte("not a preset")},
	}

	found, missing, err := resolveAgentPresetPaths(workspace)
	if err != nil {
		t.Fatalf("resolveAgentPresetPaths() error = %v", err)
	}

	if !containsString(found, ".claude") || !containsString(found, "CLAUDE.md") || !containsString(found, ".aider.conf.yml") {
		t.Fatalf("found = %v, want .claude, CLAUDE.md, and wildcard .aider match", found)
	}
	if containsString(found, "unrelated.txt") {
		t.Fatalf("found unrelated path: %v", found)
	}
	if !containsString(missing, ".cursor/") {
		t.Fatalf("missing = %v, want .cursor/", missing)
	}
}

func TestTrackAgentsCommand_TracksExistingAndReportsAbsent(t *testing.T) {
	repo := setupTrackAgentsTestRepo(t)
	if err := os.Mkdir(filepath.Join(repo, ".claude"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "CLAUDE.md"), []byte("instructions\n"), 0644); err != nil {
		t.Fatal(err)
	}

	output := runCLI(t, "track", "--agents")
	if !strings.Contains(output, "Agent paths found: .claude, CLAUDE.md") {
		t.Fatalf("output = %q, want found agent paths", output)
	}
	if !strings.Contains(output, "Agent path skipped-absent: .cursor/") {
		t.Fatalf("output = %q, want skipped .cursor/", output)
	}

	paths := readTrackedPaths(t, filepath.Join(repo, ".monodev", "stores", "agents", "track.json"))
	if len(paths) != 2 || !containsString(paths, ".claude") || !containsString(paths, "CLAUDE.md") {
		t.Fatalf("tracked paths = %v, want exactly .claude and CLAUDE.md", paths)
	}
}

func TestTrackAgentsCommand_UnionsExplicitPaths(t *testing.T) {
	repo := setupTrackAgentsTestRepo(t)
	if err := os.Mkdir(filepath.Join(repo, ".claude"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "extra_file.py"), []byte("print('hi')\n"), 0644); err != nil {
		t.Fatal(err)
	}

	runCLI(t, "track", "--agents", "extra_file.py")
	paths := readTrackedPaths(t, filepath.Join(repo, ".monodev", "stores", "agents", "track.json"))
	if len(paths) != 2 || !containsString(paths, ".claude") || !containsString(paths, "extra_file.py") {
		t.Fatalf("tracked paths = %v, want .claude and extra_file.py", paths)
	}
}

func TestTrackAgentsCommand_SurvivesCommitAndApplyInSecondWorkspace(t *testing.T) {
	repo := setupTrackAgentsTestRepo(t)
	if err := os.Mkdir(filepath.Join(repo, ".claude"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "CLAUDE.md"), []byte("instructions\n"), 0644); err != nil {
		t.Fatal(err)
	}

	runCLI(t, "track", "--agents")
	runCLI(t, "commit", "--all")

	secondWorkspace := filepath.Join(repo, "second-workspace")
	if err := os.Mkdir(secondWorkspace, 0755); err != nil {
		t.Fatal(err)
	}
	chdir(t, secondWorkspace)
	runCLI(t, "checkout", "agents")
	runCLI(t, "apply")

	if _, err := os.Stat(filepath.Join(secondWorkspace, ".claude")); err != nil {
		t.Fatalf("applied .claude directory: %v", err)
	}
	if _, err := os.Stat(filepath.Join(secondWorkspace, "CLAUDE.md")); err != nil {
		t.Fatalf("applied CLAUDE.md: %v", err)
	}
}

func TestTrackAgentsCommand_NoMatchesSucceedsWithoutTracking(t *testing.T) {
	repo := setupTrackAgentsTestRepo(t)
	output := runCLI(t, "track", "--agents")
	if !strings.Contains(output, "No agent context paths found") {
		t.Fatalf("output = %q, want clear no-match message", output)
	}
	if !strings.Contains(output, "No paths tracked") {
		t.Fatalf("output = %q, want no-tracking message", output)
	}

	paths := readTrackedPaths(t, filepath.Join(repo, ".monodev", "stores", "agents", "track.json"))
	if len(paths) != 0 {
		t.Fatalf("tracked paths = %v, want none", paths)
	}
}

func setupTrackAgentsTestRepo(t *testing.T) string {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("MONODEV_ROOT", "")
	repo := initGitRepo(t, t.TempDir(), "https://example.com/monodev.git")
	chdir(t, repo)
	runCLI(t, "init")
	runCLI(t, "checkout", "--new", "agents")
	return repo
}

func readTrackedPaths(t *testing.T, trackPath string) []string {
	t.Helper()
	data, err := os.ReadFile(trackPath)
	if err != nil {
		t.Fatalf("read track file: %v", err)
	}
	var track struct {
		Tracked []struct {
			Path string `json:"path"`
		} `json:"tracked"`
	}
	if err := json.Unmarshal(data, &track); err != nil {
		t.Fatalf("unmarshal track file: %v", err)
	}
	paths := make([]string, 0, len(track.Tracked))
	for _, tracked := range track.Tracked {
		paths = append(paths, tracked.Path)
	}
	return paths
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func TestApplyCommand_InvalidStore(t *testing.T) {
	workspaceDir, cleanup := setupTestEnv(t)
	defer cleanup()

	oldDir, _ := os.Getwd()
	_ = os.Chdir(workspaceDir)
	defer func() {
		_ = os.Chdir(oldDir)
	}()

	// Try to apply a non-existent store
	rootCmd.SetArgs([]string{"apply", "nonexistent-store"})
	var bufOut, bufErr bytes.Buffer
	rootCmd.SetOut(&bufOut)
	rootCmd.SetErr(&bufErr)

	err := rootCmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "store 'nonexistent-store' not found") {
		t.Fatalf("Apply error = %v, want missing-store error", err)
	}
}

func TestUnapplyCommand_NoState(t *testing.T) {
	workspaceDir, cleanup := setupTestEnv(t)
	defer cleanup()

	oldDir, _ := os.Getwd()
	_ = os.Chdir(workspaceDir)
	defer func() {
		_ = os.Chdir(oldDir)
	}()

	// Try to unapply when nothing is applied
	rootCmd.SetArgs([]string{"unapply"})
	var buf bytes.Buffer
	rootCmd.SetErr(&buf)

	err := rootCmd.Execute()
	// Should error because nothing is applied
	if err == nil {
		t.Error("expected error for unapply when nothing is applied")
	}
}

func TestUnapplyCommand_DryRun(t *testing.T) {
	workspaceDir, cleanup := setupTestEnv(t)
	defer cleanup()

	oldDir, _ := os.Getwd()
	_ = os.Chdir(workspaceDir)
	defer func() {
		_ = os.Chdir(oldDir)
	}()

	// Try dry-run unapply
	rootCmd.SetArgs([]string{"unapply", "--dry-run"})
	var buf bytes.Buffer
	rootCmd.SetOut(&buf)

	err := rootCmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "workspace has no managed paths") {
		t.Fatalf("Unapply --dry-run error = %v, want missing-workspace error", err)
	}
}

func TestApplyCommand_Flags(t *testing.T) {
	tests := []struct {
		name      string
		args      []string
		wantError string
	}{
		{"unsupported mode flag", []string{"apply", "--mode", "copy"}, "unknown flag: --mode"},
		{"force flag", []string{"apply", "--force"}, "no active store set"},
		{"dry-run flag", []string{"apply", "--dry-run"}, "no active store set"},
		{"all supported flags", []string{"apply", "--force", "--dry-run"}, "no active store set"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			workspaceDir, cleanup := setupTestEnv(t)
			defer cleanup()
			oldDir, _ := os.Getwd()
			if err := os.Chdir(workspaceDir); err != nil {
				t.Fatalf("Chdir() error = %v", err)
			}
			defer func() { _ = os.Chdir(oldDir) }()

			rootCmd.SetArgs(tt.args)
			err := rootCmd.Execute()
			if err == nil || !strings.Contains(err.Error(), tt.wantError) {
				t.Fatalf("Execute() error = %v, want %q", err, tt.wantError)
			}
		})
	}
}

func TestCheckoutCommand_Flags(t *testing.T) {
	tests := []struct {
		name      string
		args      []string
		wantError string
	}{
		{"new flag", []string{"checkout", "test-store", "--new"}, ""},
		{"scope flag", []string{"checkout", "test-store", "--new", "--scope", "global"}, ""},
		{"description flag", []string{"checkout", "test-store", "--new", "--description", "test desc"}, ""},
		{"owner flag", []string{"checkout", "test-store", "--new", "--owner", "test-owner"}, ""},
		{"task-id flag", []string{"checkout", "test-store", "--new", "--task-id", "DANI-1"}, ""},
		{"all supported flags", []string{"checkout", "test-store", "--new", "--scope", "global", "--description", "test", "--owner", "owner", "--task-id", "DANI-1"}, ""},
		{"retired type flag", []string{"checkout", "test-store", "--new", "--type", "issue"}, "unknown flag: --type"},
		{"retired priority flag", []string{"checkout", "test-store", "--new", "--priority", "high"}, "unknown flag: --priority"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			workspaceDir, cleanup := setupTestEnv(t)
			defer cleanup()
			oldDir, _ := os.Getwd()
			if err := os.Chdir(workspaceDir); err != nil {
				t.Fatalf("Chdir() error = %v", err)
			}
			defer func() { _ = os.Chdir(oldDir) }()

			rootCmd.SetArgs(tt.args)
			err := rootCmd.Execute()
			if tt.wantError == "" {
				if err != nil {
					t.Fatalf("Execute() error = %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantError) {
				t.Fatalf("Execute() error = %v, want %q", err, tt.wantError)
			}
		})
	}
}

func TestStoreUpdateCommand_Flags(t *testing.T) {
	tests := []struct {
		name      string
		args      []string
		wantError string
	}{
		{"scope flag", []string{"store", "update", "test-store", "--scope", "global"}, ""},
		{"description flag", []string{"store", "update", "test-store", "--description", "test desc"}, ""},
		{"owner flag", []string{"store", "update", "test-store", "--owner", "test-owner"}, ""},
		{"task-id flag", []string{"store", "update", "test-store", "--task-id", "DANI-1"}, ""},
		{"retired status flag", []string{"store", "update", "test-store", "--status", "done"}, "unknown flag: --status"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			workspaceDir, cleanup := setupTestEnv(t)
			defer cleanup()
			oldDir, _ := os.Getwd()
			if err := os.Chdir(workspaceDir); err != nil {
				t.Fatalf("Chdir() error = %v", err)
			}
			defer func() { _ = os.Chdir(oldDir) }()

			rootCmd.SetArgs([]string{"checkout", "test-store", "--new", "--scope", "global"})
			if err := rootCmd.Execute(); err != nil {
				t.Fatalf("setup checkout error = %v", err)
			}

			rootCmd.SetArgs(tt.args)
			err := rootCmd.Execute()
			if tt.wantError == "" {
				if err != nil {
					t.Fatalf("Execute() error = %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantError) {
				t.Fatalf("Execute() error = %v, want %q", err, tt.wantError)
			}
		})
	}
}

func TestREADMECommandExamplesUseRegisteredSurface(t *testing.T) {
	readme, err := os.ReadFile(filepath.Join("..", "..", "README.md"))
	if err != nil {
		t.Fatalf("ReadFile(README.md) error = %v", err)
	}

	flagPattern := regexp.MustCompile(`(?:^|\s)(--?[[:alpha:]][[:alnum:]-]*)`)
	for lineNumber, line := range strings.Split(string(readme), "\n") {
		line = strings.TrimSpace(strings.SplitN(line, " #", 2)[0])
		if !strings.HasPrefix(line, "monodev ") {
			continue
		}

		fields := strings.Fields(line)
		var commandPath []string
		current := rootCmd
		for _, field := range fields[1:] {
			if strings.HasPrefix(field, "-") || strings.HasPrefix(field, "<") || strings.HasPrefix(field, "[") {
				break
			}
			var child *cobra.Command
			for _, candidate := range current.Commands() {
				if candidate.Name() == field {
					child = candidate
					break
				}
			}
			if child == nil {
				break
			}
			commandPath = append(commandPath, field)
			current = child
		}
		if len(commandPath) == 0 {
			t.Errorf("README:%d: command path missing in %q", lineNumber+1, line)
			continue
		}

		cmd, _, findErr := rootCmd.Find(commandPath)
		if findErr != nil || cmd == nil || cmd.Name() != commandPath[len(commandPath)-1] {
			t.Errorf("README:%d: command %q is not registered (error: %v)", lineNumber+1, strings.Join(commandPath, " "), findErr)
			continue
		}

		for _, match := range flagPattern.FindAllStringSubmatch(line, -1) {
			flagName := match[1]
			normalizedFlagName := strings.TrimLeft(flagName, "-")
			registered := cmd.Flags().Lookup(normalizedFlagName)
			if registered == nil && strings.HasPrefix(flagName, "-") && !strings.HasPrefix(flagName, "--") {
				registered = cmd.Flags().ShorthandLookup(normalizedFlagName)
			}
			if registered == nil {
				t.Errorf("README:%d: flag %s is not registered for %q", lineNumber+1, flagName, strings.Join(commandPath, " "))
			}
		}
	}
}

func TestGlobalJSONFlag(t *testing.T) {
	workspaceDir, cleanup := setupTestEnv(t)
	defer cleanup()

	oldDir, _ := os.Getwd()
	_ = os.Chdir(workspaceDir)
	defer func() {
		_ = os.Chdir(oldDir)
	}()

	// Test that --json flag works globally
	rootCmd.SetArgs([]string{"store", "ls", "--json"})
	var buf bytes.Buffer
	rootCmd.SetOut(&buf)

	err := rootCmd.Execute()
	if err != nil {
		t.Fatalf("Command error = %v", err)
	}

	output := buf.String()
	if output != "" {
		// Should be valid JSON
		var v interface{}
		if err := json.Unmarshal([]byte(output), &v); err != nil {
			t.Errorf("expected valid JSON with --json flag, got error: %v", err)
		}
	}
}

func TestCommandHelp(t *testing.T) {
	commands := []string{"apply", "unapply", "status", "checkout", "track", "store", "workspace"}

	for _, cmd := range commands {
		t.Run(cmd, func(t *testing.T) {
			rootCmd.SetArgs([]string{cmd, "--help"})
			var buf bytes.Buffer
			rootCmd.SetOut(&buf)

			err := rootCmd.Execute()
			if err != nil {
				t.Errorf("Execute() for %s --help error = %v", cmd, err)
			}

			output := buf.String()
			if output == "" {
				t.Errorf("expected help output for %s, got empty", cmd)
			}
		})
	}
}
