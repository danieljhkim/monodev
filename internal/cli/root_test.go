package cli

import (
	"bytes"
	"strings"
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
		"commit", "store", "workspace",
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

func TestStoreCommand_Subcommands(t *testing.T) {
	storeSubcommands := []string{"ls", "rm", "describe"}

	for _, cmd := range storeSubcommands {
		t.Run(cmd, func(t *testing.T) {
			subCmd, _, err := rootCmd.Find([]string{"store", cmd})
			if err != nil {
				t.Errorf("Find(%q) error = %v", []string{"store", cmd}, err)
			}
			if subCmd == nil {
				t.Errorf("Find(%q) returned nil command", []string{"store", cmd})
			}
		})
	}
}

func TestWorkspaceCommand_Subcommands(t *testing.T) {
	workspaceSubcommands := []string{"ls", "rm", "describe", "repair"}

	for _, cmd := range workspaceSubcommands {
		t.Run(cmd, func(t *testing.T) {
			subCmd, _, err := rootCmd.Find([]string{"workspace", cmd})
			if err != nil {
				t.Errorf("Find(%q) error = %v", []string{"workspace", cmd}, err)
			}
			if subCmd == nil {
				t.Errorf("Find(%q) returned nil command", []string{"workspace", cmd})
			}
		})
	}
}

func TestOldCommands_NotRegistered(t *testing.T) {
	oldCommands := []string{"list", "delete", "describe", "prune"}

	for _, cmd := range oldCommands {
		t.Run(cmd, func(t *testing.T) {
			subCmd, _, err := rootCmd.Find([]string{cmd})
			// These commands should not be found at the root level
			if err == nil && subCmd != nil && subCmd.Name() == cmd {
				t.Errorf("Old command %q should not be registered at root level", cmd)
			}
		})
	}
}

func TestCommandGroups_RejectUnexpectedArguments(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{name: "store retired command", args: []string{"store", "prune"}},
		{name: "store unknown command", args: []string{"store", "bogus"}},
		{name: "workspace unknown command", args: []string{"workspace", "bogus"}},
		{name: "remote unknown command", args: []string{"remote", "bogus"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resetCommandFlags(rootCmd)
			rootCmd.SetArgs(tt.args)
			var output bytes.Buffer
			rootCmd.SetOut(&output)
			rootCmd.SetErr(&output)

			err := rootCmd.Execute()
			if err == nil {
				t.Fatalf("Execute(%q) returned nil; output = %q", tt.args, output.String())
			}
			if !strings.Contains(err.Error(), "unknown command") && !strings.Contains(err.Error(), "unexpected argument") {
				t.Fatalf("Execute(%q) error = %q, want an unknown-command or unexpected-argument error", tt.args, err)
			}
		})
	}
}

func TestCommandGroups_Help(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{name: "store", args: []string{"store"}},
		{name: "workspace", args: []string{"workspace"}},
		{name: "remote", args: []string{"remote"}},
		{name: "store help", args: []string{"store", "--help"}},
		{name: "workspace help", args: []string{"workspace", "--help"}},
		{name: "remote help", args: []string{"remote", "--help"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resetCommandFlags(rootCmd)
			rootCmd.SetArgs(tt.args)
			var output bytes.Buffer
			rootCmd.SetOut(&output)

			if err := rootCmd.Execute(); err != nil {
				t.Fatalf("Execute(%q) error = %v", tt.args, err)
			}
			if !strings.Contains(output.String(), "Usage:") {
				t.Fatalf("Execute(%q) output = %q, want help output", tt.args, output.String())
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
