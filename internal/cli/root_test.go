package cli

import (
	"bytes"
	"testing"
)

func TestRootCommand_Help(t *testing.T) {
	rootCmd.SetArgs([]string{"--help"})
	var buf bytes.Buffer
	rootCmd.SetOut(&buf)

	err := rootCmd.Execute()
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	output := buf.String()
	if output == "" {
		t.Error("expected help output, got empty string")
	}
	if !contains(output, "monodev") {
		t.Error("expected help to contain 'monodev'")
	}
}

func TestRootCommand_Version(t *testing.T) {
	SetVersion("1.2.3")
	// Cobra uses --version flag, not a version subcommand
	rootCmd.SetArgs([]string{"--version"})
	var buf bytes.Buffer
	rootCmd.SetOut(&buf)

	err := rootCmd.Execute()
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	output := buf.String()
	// Version output should contain the version number
	if !contains(output, "1.2.3") && !contains(output, "dev") {
		t.Errorf("expected version output to contain version, got %q", output)
	}
}

func TestRootCommand_InvalidCommand(t *testing.T) {
	rootCmd.SetArgs([]string{"invalid-command"})
	var buf bytes.Buffer
	rootCmd.SetErr(&buf)

	err := rootCmd.Execute()
	if err == nil {
		t.Error("expected error for invalid command")
	}
}

func TestSetVersion(t *testing.T) {
	tests := []struct {
		name    string
		version string
		want    string
	}{
		{"normal version", "1.2.3", "1.2.3"},
		{"empty version", "", "dev"}, // Should not change if empty
		{"dev version", "dev", "dev"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			SetVersion(tt.version)
			if tt.version != "" && rootCmd.Version != tt.version {
				t.Errorf("SetVersion(%q) = %q, want %q", tt.version, rootCmd.Version, tt.version)
			}
		})
	}
}

func TestRootCommand_Subcommands(t *testing.T) {
	subcommands := []string{
		"apply", "unapply", "status", "checkout", "track", "untrack",
		"commit", "prune", "list", "describe", "stack",
	}

	for _, cmd := range subcommands {
		t.Run(cmd, func(t *testing.T) {
			subCmd, _, err := rootCmd.Find([]string{cmd})
			if err != nil {
				t.Errorf("Find(%q) error = %v", cmd, err)
			}
			if subCmd == nil {
				t.Errorf("Find(%q) returned nil command", cmd)
			}
		})
	}
}

// Helper function to check if a string contains a substring
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 ||
		(len(s) > len(substr) && (s[:len(substr)] == substr ||
			s[len(s)-len(substr):] == substr ||
			containsMiddle(s, substr))))
}

func containsMiddle(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
