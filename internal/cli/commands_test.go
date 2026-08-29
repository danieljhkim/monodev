package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

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
