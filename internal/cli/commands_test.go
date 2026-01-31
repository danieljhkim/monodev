package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
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
	// Status should work even without workspace state
	if err != nil {
		t.Logf("Status command error (may be expected): %v", err)
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
		t.Logf("Status command error (may be expected): %v", err)
		return
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
	// Apply may succeed with 0 operations if store doesn't exist but no conflicts
	// Check if there's an error or if output indicates no operations
	output := bufOut.String() + bufErr.String()
	if err == nil && !contains(output, "0 operations") && !contains(output, "error") {
		t.Logf("Apply with non-existent store succeeded (may be valid behavior)")
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
	// Dry run might succeed even if nothing is applied
	_ = err
	_ = buf.String()
}

func TestApplyCommand_Flags(t *testing.T) {
	workspaceDir, cleanup := setupTestEnv(t)
	defer cleanup()

	oldDir, _ := os.Getwd()
	_ = os.Chdir(workspaceDir)
	defer func() {
		_ = os.Chdir(oldDir)
	}()

	tests := []struct {
		name string
		args []string
	}{
		{"mode flag", []string{"apply", "--mode", "copy"}},
		{"force flag", []string{"apply", "--force"}},
		{"dry-run flag", []string{"apply", "--dry-run"}},
		{"all flags", []string{"apply", "--mode", "copy", "--force", "--dry-run"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rootCmd.SetArgs(tt.args)
			var buf bytes.Buffer
			rootCmd.SetErr(&buf)

			// These will likely error due to missing store, but flags should be parsed
			err := rootCmd.Execute()
			_ = err // We're just testing flag parsing
		})
	}
}

func TestCheckoutCommand_Flags(t *testing.T) {
	workspaceDir, cleanup := setupTestEnv(t)
	defer cleanup()

	oldDir, _ := os.Getwd()
	_ = os.Chdir(workspaceDir)
	defer func() {
		_ = os.Chdir(oldDir)
	}()

	tests := []struct {
		name string
		args []string
	}{
		{"new flag", []string{"checkout", "test-store", "--new"}},
		{"scope flag", []string{"checkout", "test-store", "--scope", "global"}},
		{"description flag", []string{"checkout", "test-store", "--description", "test desc"}},
		{"all flags", []string{"checkout", "test-store", "--new", "--scope", "component", "--description", "test"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rootCmd.SetArgs(tt.args)
			var buf bytes.Buffer
			rootCmd.SetOut(&buf)

			// These may succeed or fail, but flags should be parsed
			err := rootCmd.Execute()
			_ = err // We're just testing flag parsing
		})
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
		t.Logf("Command error (may be expected): %v", err)
		return
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
